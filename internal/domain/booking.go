package domain

import "time"

// BookingStatus is the lifecycle state of a slot reservation.
type BookingStatus string

const (
	// BookingPending holds a slot tentatively before the deposit is paid.
	BookingPending BookingStatus = "pending"
	// BookingDepositPaid means the deposit cleared; the slot is confirmed-funding.
	BookingDepositPaid BookingStatus = "deposit_paid"
	// BookingConfirmed means the berth is locked (rule 5 edit lock released after).
	BookingConfirmed BookingStatus = "confirmed"
	// BookingLoaded means cargo is aboard.
	BookingLoaded BookingStatus = "loaded"
	// BookingReleased means an unpaid booking was auto-released at the 72h gate window.
	BookingReleased BookingStatus = "released"
	// BookingCancelled means the forwarder refunded.
	BookingCancelled BookingStatus = "cancelled"
	// BookingWaitlist means the booking lost the last-berth contention and waits.
	BookingWaitlist BookingStatus = "waitlist"
)

// Booking is a temperature-controlled slot reservation made by a booking
// specialist. SubmittedAt is the immutable submission timestamp used to break
// last-berth contentions (earliest wins).
type Booking struct {
	ID             BookingID
	VoyageID       VoyageID
	ForwarderID    ForwarderID
	CargoCategory  CargoCategory
	TEU            int
	TempControlled bool
	Status         BookingStatus
	DepositPaid    bool
	SubmittedAt    time.Time
	CreatedAt      time.Time
	ConfirmedAt    time.Time
	WaitlistSince  time.Time
	ReleasedReason string
	// IsLastSlot marks the booking occupying the oversell-limit-th (final) slot,
	// making it the stealable contention target.
	IsLastSlot bool
}

// HoldsSlot reports whether the booking currently consumes voyage capacity.
func (b Booking) HoldsSlot() bool {
	switch b.Status {
	case BookingPending, BookingDepositPaid, BookingConfirmed:
		return true
	}
	return false
}

func (b *Booking) PayDeposit() {
	b.DepositPaid = true
	if b.Status == BookingPending {
		b.Status = BookingDepositPaid
	}
}

func (b *Booking) Confirm(now time.Time) {
	b.Status = BookingConfirmed
	b.ConfirmedAt = now
}

// MarkWaitlist demotes the booking to the waitlist, releasing its slot.
func (b *Booking) MarkWaitlist(now time.Time) {
	b.Status = BookingWaitlist
	b.WaitlistSince = now
	b.IsLastSlot = false
}

// Promote restores a waitlisted booking to a slot-holding status, preserving
// any previously paid deposit.
func (b *Booking) Promote(now time.Time) {
	if b.DepositPaid {
		b.Status = BookingDepositPaid
	} else {
		b.Status = BookingPending
	}
	b.WaitlistSince = time.Time{}
}

func (b *Booking) Release(reason string) {
	b.Status = BookingReleased
	b.ReleasedReason = reason
	b.IsLastSlot = false
}

func (b *Booking) Cancel() {
	b.Status = BookingCancelled
	b.IsLastSlot = false
}

func (b *Booking) Load() {
	b.Status = BookingLoaded
}
