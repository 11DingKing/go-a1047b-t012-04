package service

import (
	"fmt"
	"time"

	"arcticexpress/internal/domain"
	"arcticexpress/internal/store"
)

// DispatchService is the port-dispatcher workflow: coordinating berth allocation
// and fleet container pickup. Berth claims by two vessels for the same berth
// are resolved in favour of the earliest submission timestamp, with the loser
// entering a waitlist and being notified in real time.
type DispatchService struct {
	base
}

func NewDispatchService(s *store.Store, c domain.Clock) *DispatchService {
	return &DispatchService{base: base{Store: s, Clock: c}}
}

// AllocateBerth claims a berth for a voyage. Returns allocated=true when the
// voyage now holds the berth. When an earlier-submitted claim displaces a
// recent holder, loserVoyage is set to the displaced voyage. A later submission
// that cannot win returns allocated=false with loserVoyage equal to the current
// holder (the requester is notified and recorded as a berth waitlister).
func (ds *DispatchService) AllocateBerth(berthID domain.BerthID, voyageID domain.VoyageID, shipName string, requestedAt time.Time) (allocated bool, loserVoyage domain.VoyageID, err error) {
	ds.Store.Lock()
	defer ds.Store.Unlock()

	berth, ok := ds.Store.Berths[berthID]
	if !ok {
		return false, "", fmt.Errorf("%w: berth %s", domain.ErrNotFound, berthID)
	}
	if _, ok := ds.Store.Voyages[voyageID]; !ok {
		return false, "", fmt.Errorf("%w: voyage %s", domain.ErrNotFound, voyageID)
	}
	if requestedAt.IsZero() {
		requestedAt = ds.now()
	}
	now := ds.now()

	if !berth.IsAllocated() {
		berth.AllocatedVoyage = voyageID
		berth.AllocatedShip = shipName
		berth.AllocatedAt = now
		berth.ClaimRequestedAt = requestedAt
		return true, "", nil
	}

	// Contested berth: reconcile by earliest submission timestamp within the
	// contention window. An earlier submission may displace a recent holder.
	canSteal := berth.AllocatedAt.IsZero() || now.Sub(berth.AllocatedAt) <= berthContentionWindow
	if canSteal && requestedAt.Before(berth.ClaimRequestedAt) {
		loser := berth.AllocatedVoyage
		berth.AllocatedVoyage = voyageID
		berth.AllocatedShip = shipName
		berth.AllocatedAt = now
		berth.ClaimRequestedAt = requestedAt
		ds.notify(domain.RoleDispatcher, "berth_waitlist",
			fmt.Sprintf("voyage %s lost berth %s to an earlier submission from %s", loser, berthID, voyageID))
		return true, loser, nil
	}

	// Later submission: the requester enters the berth waitlist.
	ds.notify(domain.RoleDispatcher, "berth_waitlist",
		fmt.Sprintf("voyage %s entered berth waitlist for %s (held by %s)", voyageID, berthID, berth.AllocatedVoyage))
	return false, berth.AllocatedVoyage, nil
}

// ReleaseBerth frees a berth, e.g. after the vessel departs.
func (ds *DispatchService) ReleaseBerth(berthID domain.BerthID) error {
	ds.Store.Lock()
	defer ds.Store.Unlock()
	berth, ok := ds.Store.Berths[berthID]
	if !ok {
		return fmt.Errorf("%w: berth %s", domain.ErrNotFound, berthID)
	}
	berth.AllocatedVoyage = ""
	berth.AllocatedShip = ""
	berth.AllocatedAt = time.Time{}
	berth.ClaimRequestedAt = time.Time{}
	return nil
}

// Berth fetches a berth by id.
func (ds *DispatchService) Berth(berthID domain.BerthID) (*domain.Berth, error) {
	ds.Store.Lock()
	defer ds.Store.Unlock()
	b, ok := ds.Store.Berths[berthID]
	if !ok {
		return nil, fmt.Errorf("%w: berth %s", domain.ErrNotFound, berthID)
	}
	return b, nil
}
