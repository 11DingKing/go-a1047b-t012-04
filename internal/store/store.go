package store

import (
	"fmt"
	"sync"

	"arcticexpress/internal/domain"
)

// Store is the process-local persistence layer. All maps are accessed under the
// store mutex; service callers obtain it via Lock/Unlock so multi-step domain
// operations are atomic transactions.
type Store struct {
	mu sync.Mutex

	Forwarders    map[domain.ForwarderID]*domain.Forwarder
	Ports         map[domain.PortID]*domain.Port
	Berths        map[domain.BerthID]*domain.Berth
	Vessels       map[domain.VesselID]*domain.Vessel
	Voyages       map[domain.VoyageID]*domain.Voyage
	Bookings      map[domain.BookingID]*domain.Booking
	Containers    map[domain.ContainerID]*domain.TempContainer
	Transfers     map[string]*domain.TransferOrder
	Manifests     map[domain.VoyageID]*domain.Manifest
	PortChanges   map[string]*domain.PortChange
	Locks         map[string]*domain.EditLock
	Notifications map[string]*domain.Notification

	seq int
}

func New() *Store {
	return &Store{
		Forwarders:    map[domain.ForwarderID]*domain.Forwarder{},
		Ports:         map[domain.PortID]*domain.Port{},
		Berths:        map[domain.BerthID]*domain.Berth{},
		Vessels:       map[domain.VesselID]*domain.Vessel{},
		Voyages:       map[domain.VoyageID]*domain.Voyage{},
		Bookings:      map[domain.BookingID]*domain.Booking{},
		Containers:    map[domain.ContainerID]*domain.TempContainer{},
		Transfers:     map[string]*domain.TransferOrder{},
		Manifests:     map[domain.VoyageID]*domain.Manifest{},
		PortChanges:   map[string]*domain.PortChange{},
		Locks:         map[string]*domain.EditLock{},
		Notifications: map[string]*domain.Notification{},
	}
}

// Lock acquires the global store mutex. Callers must defer Unlock.
func (s *Store) Lock()   { s.mu.Lock() }
func (s *Store) Unlock() { s.mu.Unlock() }

// NextID returns a process-unique identifier. The caller must hold the lock.
func (s *Store) NextID(prefix string) string {
	s.seq++
	return fmt.Sprintf("%s-%d", prefix, s.seq)
}

// Snapshot is a point-in-time copy used for diagnostics and the HTTP layer.
type Snapshot struct {
	Voyages       []*domain.Voyage
	Bookings      []*domain.Booking
	Berths        []*domain.Berth
	Containers    []*domain.TempContainer
	Manifests     []*domain.Manifest
	PortChanges   []*domain.PortChange
	Notifications []*domain.Notification
}

func (s *Store) Snapshot() Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	snap := Snapshot{}
	for _, v := range s.Voyages {
		snap.Voyages = append(snap.Voyages, v)
	}
	for _, b := range s.Bookings {
		snap.Bookings = append(snap.Bookings, b)
	}
	for _, b := range s.Berths {
		snap.Berths = append(snap.Berths, b)
	}
	for _, c := range s.Containers {
		snap.Containers = append(snap.Containers, c)
	}
	for _, m := range s.Manifests {
		snap.Manifests = append(snap.Manifests, m)
	}
	for _, p := range s.PortChanges {
		snap.PortChanges = append(snap.PortChanges, p)
	}
	for _, n := range s.Notifications {
		snap.Notifications = append(snap.Notifications, n)
	}
	return snap
}
