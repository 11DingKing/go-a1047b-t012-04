package service

import (
	"fmt"
	"time"

	"arcticexpress/internal/domain"
	"arcticexpress/internal/store"
)

// BookingService covers the booking specialist workflow: reserving
// temperature-controlled berths, paying deposits, locking berths, refunding,
// and the waitlist/promotion mechanics.
type BookingService struct {
	base
}

func NewBookingService(s *store.Store, c domain.Clock) *BookingService {
	return &BookingService{base: base{Store: s, Clock: c}}
}

// CreateBooking reserves a slot. Rule 1 gates temp-controlled berths behind the
// power-battery qualification. Capacity (with the 3% oversell cap) is consumed
// here; the last slot is reconciled by earliest submission timestamp.
func (bs *BookingService) CreateBooking(
	voyageID domain.VoyageID,
	fwdID domain.ForwarderID,
	cargo domain.CargoCategory,
	teu int,
	tempControlled bool,
	submittedAt time.Time,
) (*domain.Booking, error) {
	bs.Store.Lock()
	defer bs.Store.Unlock()

	v, ok := bs.Store.Voyages[voyageID]
	if !ok {
		return nil, fmt.Errorf("%w: voyage %s", domain.ErrNotFound, voyageID)
	}
	if v.Status == domain.VoyageClosed {
		return nil, fmt.Errorf("%w: voyage closed", domain.ErrInvalidState)
	}
	if v.ManifestFrozen {
		return nil, fmt.Errorf("%w: voyage manifest frozen", domain.ErrFrozen)
	}
	fwd, ok := bs.Store.Forwarders[fwdID]
	if !ok {
		return nil, fmt.Errorf("%w: forwarder %s", domain.ErrNotFound, fwdID)
	}
	if tempControlled && !fwd.HasBatteryQualification() {
		return nil, fmt.Errorf("%w: temp-controlled berth requires power-battery qualification", domain.ErrUnauthorized)
	}
	if teu <= 0 {
		return nil, fmt.Errorf("%w: teu must be positive", domain.ErrInvalidState)
	}
	if submittedAt.IsZero() {
		submittedAt = bs.now()
	}

	limit := v.OversellLimit()
	// Count held slots BEFORE inserting so the new booking is not counted.
	held := bs.countHeld(voyageID)
	now := bs.now()

	b := &domain.Booking{
		ID:             domain.BookingID(bs.Store.NextID("BKG")),
		VoyageID:       voyageID,
		ForwarderID:    fwdID,
		CargoCategory:  cargo,
		TEU:            teu,
		TempControlled: tempControlled,
		Status:         domain.BookingPending,
		SubmittedAt:    submittedAt,
		CreatedAt:      now,
	}
	bs.Store.Bookings[b.ID] = b

	switch {
	case held < limit-1:
		// Plenty of room: the booking holds a regular slot.
	case held >= limit:
		// Full: reconcile the last slot by earliest submission timestamp.
		if !bs.tryStealLastSlot(b, now) {
			b.MarkWaitlist(now)
			bs.notify(domain.RoleBookingSpecialist, "waitlist",
				fmt.Sprintf("booking %s waitlisted on %s (capacity full)", b.ID, voyageID))
		}
	default:
		// held == limit-1: this booking occupies the final slot.
		b.IsLastSlot = true
	}
	return b, nil
}

// PayDeposit moves a pending booking to deposit_paid.
func (bs *BookingService) PayDeposit(bookingID domain.BookingID) error {
	bs.Store.Lock()
	defer bs.Store.Unlock()
	b, ok := bs.Store.Bookings[bookingID]
	if !ok {
		return fmt.Errorf("%w: booking %s", domain.ErrNotFound, bookingID)
	}
	if b.Status != domain.BookingPending {
		return fmt.Errorf("%w: cannot pay deposit from %s", domain.ErrInvalidState, b.Status)
	}
	b.PayDeposit()
	return nil
}

// ConfirmBooking locks the berth (rule 5 edit lock). The deposit must be paid.
func (bs *BookingService) ConfirmBooking(bookingID domain.BookingID) (*domain.Booking, error) {
	if err := bs.acquireLock(lockKeyBooking(bookingID), domain.RoleBookingSpecialist); err != nil {
		return nil, err
	}
	defer bs.releaseLock(lockKeyBooking(bookingID))

	bs.Store.Lock()
	defer bs.Store.Unlock()

	b, ok := bs.Store.Bookings[bookingID]
	if !ok {
		return nil, fmt.Errorf("%w: booking %s", domain.ErrNotFound, bookingID)
	}
	if b.Status != domain.BookingDepositPaid {
		return nil, fmt.Errorf("%w: confirm requires deposit_paid, got %s", domain.ErrInvalidState, b.Status)
	}
	v, ok := bs.Store.Voyages[b.VoyageID]
	if !ok {
		return nil, fmt.Errorf("%w: voyage", domain.ErrNotFound)
	}
	if v.ManifestFrozen {
		return nil, fmt.Errorf("%w: voyage frozen", domain.ErrFrozen)
	}
	b.Confirm(bs.now())
	return b, nil
}

// CancelBooking refunds a booking (rule 5 edit lock). Releasing a held slot
// promotes the earliest waitlisted booking on the voyage.
func (bs *BookingService) CancelBooking(bookingID domain.BookingID) error {
	if err := bs.acquireLock(lockKeyBooking(bookingID), domain.RoleBookingSpecialist); err != nil {
		return err
	}
	defer bs.releaseLock(lockKeyBooking(bookingID))

	bs.Store.Lock()
	defer bs.Store.Unlock()

	b, ok := bs.Store.Bookings[bookingID]
	if !ok {
		return fmt.Errorf("%w: booking %s", domain.ErrNotFound, bookingID)
	}
	if b.Status == domain.BookingCancelled || b.Status == domain.BookingReleased {
		return fmt.Errorf("%w: booking already terminal", domain.ErrInvalidState)
	}
	held := b.HoldsSlot()
	b.Cancel()
	if held {
		bs.promoteWaitlist(b.VoyageID)
	}
	return nil
}

// ReleaseUnpaidBeforeGate implements rule 2's automatic release: bookings still
// unpaid (pending) are released once the 72h gate window has elapsed. Releasing
// a held slot promotes a waitlisted booking.
func (bs *BookingService) ReleaseUnpaidBeforeGate(voyageID domain.VoyageID) int {
	bs.Store.Lock()
	defer bs.Store.Unlock()
	released := 0
	for _, b := range bs.Store.Bookings {
		if b.VoyageID == voyageID && b.Status == domain.BookingPending {
			b.Release("unpaid deposit past 72h gate window")
			released++
		}
	}
	if released > 0 {
		bs.promoteWaitlist(voyageID)
	}
	return released
}

// Booking returns a booking by id.
func (bs *BookingService) Booking(bookingID domain.BookingID) (*domain.Booking, error) {
	bs.Store.Lock()
	defer bs.Store.Unlock()
	b, ok := bs.Store.Bookings[bookingID]
	if !ok {
		return nil, fmt.Errorf("%w: booking %s", domain.ErrNotFound, bookingID)
	}
	return b, nil
}

// countHeld counts slot-holding bookings on a voyage. Caller must hold the lock.
func (bs *BookingService) countHeld(voyageID domain.VoyageID) int {
	n := 0
	for _, b := range bs.Store.Bookings {
		if b.VoyageID == voyageID && b.HoldsSlot() {
			n++
		}
	}
	return n
}

// tryStealLastSlot reconciles the final slot when the voyage is full: if the
// incoming booking was submitted earlier than the current last-slot holder
// (within the contention window), the holder is demoted to the waitlist and the
// earlier booking takes the final slot. Returns false if no steal occurred.
func (bs *BookingService) tryStealLastSlot(b *domain.Booking, now time.Time) bool {
	for _, other := range bs.Store.Bookings {
		if other.ID == b.ID || other.VoyageID != b.VoyageID || !other.IsLastSlot {
			continue
		}
		if !other.HoldsSlot() {
			continue
		}
		if now.Sub(other.ConfirmedAt) > lastSlotContentionWindow && !other.ConfirmedAt.IsZero() {
			continue
		}
		if b.SubmittedAt.Before(other.SubmittedAt) {
			other.MarkWaitlist(now)
			bs.notify(domain.RoleBookingSpecialist, "waitlist",
				fmt.Sprintf("booking %s demoted to waitlist; earlier submission %s took the last berth on %s", other.ID, b.ID, b.VoyageID))
			b.IsLastSlot = true
			return true
		}
	}
	return false
}

// promoteWaitlist assigns the freed slot to the earliest-submitted waitlisted
// booking. Caller must hold the lock.
func (bs *BookingService) promoteWaitlist(voyageID domain.VoyageID) {
	v, ok := bs.Store.Voyages[voyageID]
	if !ok {
		return
	}
	if bs.countHeld(voyageID) >= v.OversellLimit() {
		return
	}
	var candidate *domain.Booking
	for _, b := range bs.Store.Bookings {
		if b.VoyageID == voyageID && b.Status == domain.BookingWaitlist {
			if candidate == nil || b.SubmittedAt.Before(candidate.SubmittedAt) {
				candidate = b
			}
		}
	}
	if candidate == nil {
		return
	}
	now := bs.now()
	candidate.Promote(now)
	if bs.countHeld(voyageID) == v.OversellLimit() {
		candidate.IsLastSlot = true
	}
	bs.notify(domain.RoleBookingSpecialist, "promoted",
		fmt.Sprintf("booking %s promoted from waitlist on %s", candidate.ID, voyageID))
}
