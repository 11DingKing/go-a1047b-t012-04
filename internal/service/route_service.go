package service

import (
	"fmt"
	"time"

	"arcticexpress/internal/domain"
	"arcticexpress/internal/store"
)

// RouteService is the ship-owner / route-operations workflow: maintaining the
// Felixstowe → Rotterdam → Hamburg → Gdynia rotation, remaining capacity, and
// the Suez→Arctic port-change approvals (rule 4).
type RouteService struct {
	base
}

func NewRouteService(s *store.Store, c domain.Clock) *RouteService {
	return &RouteService{base: base{Store: s, Clock: c}}
}

// CreatePort registers a port.
func (rs *RouteService) CreatePort(id domain.PortID, code, name string) *domain.Port {
	rs.Store.Lock()
	defer rs.Store.Unlock()
	p := &domain.Port{ID: id, Code: code, Name: name}
	rs.Store.Ports[id] = p
	return p
}

// CreateVessel registers a vessel with its temperature-controlled capacity.
func (rs *RouteService) CreateVessel(id domain.VesselID, name string, capTEU int) *domain.Vessel {
	rs.Store.Lock()
	defer rs.Store.Unlock()
	v := &domain.Vessel{ID: id, Name: name, TempCapacityTEU: capTEU}
	rs.Store.Vessels[id] = v
	return v
}

// CreateBerth registers a quay slot at a port.
func (rs *RouteService) CreateBerth(id domain.BerthID, portID domain.PortID) *domain.Berth {
	rs.Store.Lock()
	defer rs.Store.Unlock()
	b := &domain.Berth{ID: id, PortID: portID}
	rs.Store.Berths[id] = b
	return b
}

// CreateVoyage registers an Arctic rotation. Port calls must already be
// sequenced in the maintained Felixstowe → Rotterdam → Hamburg → Gdynia order.
func (rs *RouteService) CreateVoyage(id domain.VoyageID, vesselID domain.VesselID, number string, calls []domain.PortCall, cap int) (*domain.Voyage, error) {
	rs.Store.Lock()
	defer rs.Store.Unlock()
	if _, ok := rs.Store.Vessels[vesselID]; !ok {
		return nil, fmt.Errorf("%w: vessel %s", domain.ErrNotFound, vesselID)
	}
	if cap <= 0 {
		return nil, fmt.Errorf("%w: capacity must be positive", domain.ErrInvalidState)
	}
	for i := 1; i < len(calls); i++ {
		if calls[i-1].Sequence >= calls[i].Sequence {
			return nil, fmt.Errorf("%w: port calls must be sequenced ascending", domain.ErrInvalidState)
		}
	}
	v := &domain.Voyage{
		ID:           id,
		VesselID:     vesselID,
		VoyageNumber: number,
		PortCalls:    append([]domain.PortCall(nil), calls...),
		TempCapacity: cap,
		Status:       domain.VoyageScheduled,
	}
	rs.Store.Voyages[id] = v
	return v, nil
}

// Voyage fetches a voyage by id.
func (rs *RouteService) Voyage(id domain.VoyageID) (*domain.Voyage, error) {
	rs.Store.Lock()
	defer rs.Store.Unlock()
	v, ok := rs.Store.Voyages[id]
	if !ok {
		return nil, fmt.Errorf("%w: voyage %s", domain.ErrNotFound, id)
	}
	return v, nil
}

// RemainingCapacity reports held and oversell-remaining slots for a voyage.
func (rs *RouteService) RemainingCapacity(id domain.VoyageID) (held, limit int, err error) {
	rs.Store.Lock()
	defer rs.Store.Unlock()
	v, ok := rs.Store.Voyages[id]
	if !ok {
		return 0, 0, fmt.Errorf("%w: voyage %s", domain.ErrNotFound, id)
	}
	n := 0
	for _, b := range rs.Store.Bookings {
		if b.VoyageID == id && b.HoldsSlot() {
			n++
		}
	}
	return n, v.OversellLimit(), nil
}

// RequestPortChange starts a Suez-congestion diversion. It acquires the booking
// edit lock (rule 5) so it is mutually exclusive with berth-lock and refund.
func (rs *RouteService) RequestPortChange(bookingID domain.BookingID, source, target domain.VoyageID, reason string) (*domain.PortChange, error) {
	if err := rs.acquireLock(lockKeyBooking(bookingID), domain.RoleRouteOps); err != nil {
		return nil, err
	}
	defer rs.releaseLock(lockKeyBooking(bookingID))

	rs.Store.Lock()
	defer rs.Store.Unlock()

	b, ok := rs.Store.Bookings[bookingID]
	if !ok {
		return nil, fmt.Errorf("%w: booking %s", domain.ErrNotFound, bookingID)
	}
	if b.Status == domain.BookingCancelled || b.Status == domain.BookingReleased {
		return nil, fmt.Errorf("%w: booking is terminal", domain.ErrInvalidState)
	}
	if _, ok := rs.Store.Voyages[source]; !ok {
		return nil, fmt.Errorf("%w: source voyage", domain.ErrNotFound)
	}
	if _, ok := rs.Store.Voyages[target]; !ok {
		return nil, fmt.Errorf("%w: target voyage", domain.ErrNotFound)
	}
	if v := rs.Store.Voyages[target]; v.ManifestFrozen {
		return nil, fmt.Errorf("%w: target voyage frozen", domain.ErrFrozen)
	}
	pc := &domain.PortChange{
		ID:           rs.Store.NextID("PC"),
		BookingID:    bookingID,
		SourceVoyage: source,
		TargetVoyage: target,
		Reason:       reason,
		Status:       domain.PCRequested,
		RequestedAt:  rs.now(),
	}
	rs.Store.PortChanges[pc.ID] = pc
	return pc, nil
}

// ApprovePortChange performs the route-ops secondary approval (rule 4) and
// enforces the 48h-before-cutoff deadline. A late request is rejected.
func (rs *RouteService) ApprovePortChange(pcID string) (*domain.PortChange, error) {
	rs.Store.Lock()
	defer rs.Store.Unlock()

	pc, ok := rs.Store.PortChanges[pcID]
	if !ok {
		return nil, fmt.Errorf("%w: port change %s", domain.ErrNotFound, pcID)
	}
	if pc.Status != domain.PCRequested {
		return nil, fmt.Errorf("%w: port change not in requested state", domain.ErrInvalidState)
	}
	v, ok := rs.Store.Voyages[pc.SourceVoyage]
	if !ok {
		return nil, fmt.Errorf("%w: source voyage", domain.ErrNotFound)
	}
	cutoff, ok := v.LoadingCutoff()
	if !ok {
		return nil, fmt.Errorf("%w: voyage has no cutoff", domain.ErrInvalidState)
	}
	now := rs.now()
	deadline := cutoff.Add(-48 * time.Hour)
	if now.After(deadline) {
		pc.Status = domain.PCRejected
		return pc, fmt.Errorf("%w: port change past 48h cutoff window (deadline %s)", domain.ErrDeadlineExceeded, deadline.Format(time.RFC3339))
	}
	pc.Status = domain.PCRouteOpsApproved
	pc.ApprovedAt = now
	return pc, nil
}

// ApplyPortChange moves the booking onto the target voyage and bumps both
// manifests' versions. The version bump is what lets a parallel customs
// declaration detect a manifest conflict and roll back (failure recovery).
func (rs *RouteService) ApplyPortChange(pcID string) (*domain.PortChange, error) {
	pc, err := rs.ApprovePortChange(pcID) // idempotent: re-checks approval + deadline
	if err != nil {
		return nil, err
	}
	rs.Store.Lock()
	defer rs.Store.Unlock()

	if pc.Status != domain.PCRouteOpsApproved {
		return nil, fmt.Errorf("%w: port change not approved", domain.ErrInvalidState)
	}
	b, ok := rs.Store.Bookings[pc.BookingID]
	if !ok {
		return nil, fmt.Errorf("%w: booking", domain.ErrNotFound)
	}
	if !b.HoldsSlot() {
		return nil, fmt.Errorf("%w: booking no longer holds a slot", domain.ErrInvalidState)
	}
	if target := rs.Store.Voyages[pc.TargetVoyage]; target == nil {
		return nil, fmt.Errorf("%w: target voyage", domain.ErrNotFound)
	} else if target.ManifestFrozen {
		return nil, fmt.Errorf("%w: target voyage frozen", domain.ErrFrozen)
	}

	wasLast := b.IsLastSlot
	b.VoyageID = pc.TargetVoyage
	b.IsLastSlot = false
	if wasLast {
		// The freed final slot on the source voyage may promote a waitlisted booking.
		rs.promoteWaitlistLocked(pc.SourceVoyage)
	}

	// Bump manifest versions so an in-flight customs declaration notices the change.
	if m := rebuildManifest(rs.Store, pc.SourceVoyage); m != nil {
		m.BumpVersion()
	}
	if m := rebuildManifest(rs.Store, pc.TargetVoyage); m != nil {
		m.BumpVersion()
	}

	pc.Status = domain.PCApplied
	pc.AppliedAt = rs.now()
	return pc, nil
}

// PortChange fetches a port change by id.
func (rs *RouteService) PortChange(pcID string) (*domain.PortChange, error) {
	rs.Store.Lock()
	defer rs.Store.Unlock()
	pc, ok := rs.Store.PortChanges[pcID]
	if !ok {
		return nil, fmt.Errorf("%w: port change %s", domain.ErrNotFound, pcID)
	}
	return pc, nil
}

// promoteWaitlistLocked is a store-lock-held variant reusing the booking service
// promotion logic. It is intentionally duplicated-free via a helper on the
// shared store; here it delegates by reconstructing the candidate selection.
func (rs *RouteService) promoteWaitlistLocked(voyageID domain.VoyageID) {
	v, ok := rs.Store.Voyages[voyageID]
	if !ok {
		return
	}
	held := 0
	for _, b := range rs.Store.Bookings {
		if b.VoyageID == voyageID && b.HoldsSlot() {
			held++
		}
	}
	if held >= v.OversellLimit() {
		return
	}
	var candidate *domain.Booking
	for _, b := range rs.Store.Bookings {
		if b.VoyageID == voyageID && b.Status == domain.BookingWaitlist {
			if candidate == nil || b.SubmittedAt.Before(candidate.SubmittedAt) {
				candidate = b
			}
		}
	}
	if candidate == nil {
		return
	}
	candidate.Promote(rs.now())
	held++
	if held == v.OversellLimit() {
		candidate.IsLastSlot = true
	}
	rs.notify(domain.RoleBookingSpecialist, "promoted",
		fmt.Sprintf("booking %s promoted from waitlist on %s after port-change", candidate.ID, voyageID))
}
