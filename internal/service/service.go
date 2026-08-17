package service

import (
	"fmt"
	"time"

	"arcticexpress/internal/domain"
	"arcticexpress/internal/store"
)

// base is shared by every service: it owns the persistence handle and the clock.
type base struct {
	Store *store.Store
	Clock domain.Clock
}

func (b base) now() time.Time { return b.Clock.Now() }

// notify records a real-time notification for a role. Caller must hold the lock.
func (b base) notify(target domain.Role, subject, body string) *domain.Notification {
	n := &domain.Notification{
		ID:        b.Store.NextID("NTF"),
		Target:    target,
		Subject:   subject,
		Body:      body,
		CreatedAt: b.now(),
	}
	b.Store.Notifications[n.ID] = n
	return n
}

// lastSlotContentionWindow bounds how long a confirmed last-slot holder remains
// "stealable" by an earlier-submitted booking. It models the window during
// which near-simultaneous submissions are reconciled by timestamp.
const lastSlotContentionWindow = 5 * time.Minute

// berthContentionWindow bounds berth-claim reconciliation similarly.
const berthContentionWindow = 5 * time.Minute

// lockKey helpers namespace the rule-5 edit lock by resource.
func lockKeyBooking(id domain.BookingID) string { return "booking:" + string(id) }
func lockKeyVoyage(id domain.VoyageID) string   { return "voyage:" + string(id) }

// acquireLock implements rule 5: only one role may hold a resource's edit lock
// at a time. Re-acquisition by the same holder (idempotent refresh) is allowed.
func (b base) acquireLock(resource string, role domain.Role) error {
	b.Store.Lock()
	defer b.Store.Unlock()
	if existing, ok := b.Store.Locks[resource]; ok && existing.Holder != role {
		return fmt.Errorf("%w: %s held by %s", domain.ErrLockHeld, resource, existing.Holder)
	}
	b.Store.Locks[resource] = &domain.EditLock{
		Resource:   resource,
		Holder:     role,
		AcquiredAt: b.now(),
	}
	return nil
}

func (b base) releaseLock(resource string) {
	b.Store.Lock()
	defer b.Store.Unlock()
	delete(b.Store.Locks, resource)
}

// rebuildManifest reconstructs manifest entries from the voyage's confirmed
// bookings. The manifest is created on first use. Caller must hold the lock.
func rebuildManifest(s *store.Store, voyageID domain.VoyageID) *domain.Manifest {
	m, ok := s.Manifests[voyageID]
	if !ok {
		m = domain.NewManifest(voyageID)
		s.Manifests[voyageID] = m
	}
	entries := make([]domain.ManifestEntry, 0)
	for _, b := range s.Bookings {
		if b.VoyageID == voyageID && b.Status == domain.BookingConfirmed {
			entries = append(entries, domain.ManifestEntry{
				BookingID:     b.ID,
				ForwarderID:   b.ForwarderID,
				CargoCategory: b.CargoCategory,
				TEU:           b.TEU,
			})
		}
	}
	m.Entries = entries
	return m
}
