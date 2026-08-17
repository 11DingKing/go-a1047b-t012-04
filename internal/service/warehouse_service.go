package service

import (
	"fmt"
	"time"

	"arcticexpress/internal/domain"
	"arcticexpress/internal/store"
)

// WarehouseService is the cold-chain warehouse workflow: binding
// temperature-controlled containers to bookings, recording the temperature
// curve, and dispatching standby containers on rule-3 breaches.
type WarehouseService struct {
	base
}

func NewWarehouseService(s *store.Store, c domain.Clock) *WarehouseService {
	return &WarehouseService{base: base{Store: s, Clock: c}}
}

// BindContainer binds a temperature-controlled container to a booking and
// records the setpoint band that defines an out-of-bounds breach.
func (ws *WarehouseService) BindContainer(bookingID domain.BookingID, containerID domain.ContainerID, setpointLow, setpointHigh float64) (*domain.TempContainer, error) {
	ws.Store.Lock()
	defer ws.Store.Unlock()

	b, ok := ws.Store.Bookings[bookingID]
	if !ok {
		return nil, fmt.Errorf("%w: booking %s", domain.ErrNotFound, bookingID)
	}
	if !b.HoldsSlot() {
		return nil, fmt.Errorf("%w: booking must hold a slot to bind a container", domain.ErrInvalidState)
	}
	if _, exists := ws.Store.Containers[containerID]; exists {
		return nil, fmt.Errorf("%w: container %s already bound", domain.ErrConflict, containerID)
	}
	if setpointLow >= setpointHigh {
		return nil, fmt.Errorf("%w: invalid setpoint band", domain.ErrInvalidState)
	}
	c := &domain.TempContainer{
		ID:           containerID,
		BookingID:    bookingID,
		SetpointLow:  setpointLow,
		SetpointHigh: setpointHigh,
		Status:       domain.ContainerBound,
	}
	ws.Store.Containers[containerID] = c
	return c, nil
}

// RecordReading appends a temperature sample. It returns whether the reading
// is currently outside the setpoint band.
func (ws *WarehouseService) RecordReading(containerID domain.ContainerID, value float64, at time.Time) (bool, error) {
	ws.Store.Lock()
	defer ws.Store.Unlock()

	c, ok := ws.Store.Containers[containerID]
	if !ok {
		return false, fmt.Errorf("%w: container %s", domain.ErrNotFound, containerID)
	}
	if c.Frozen {
		return false, fmt.Errorf("%w: container frozen pending standby transfer", domain.ErrFrozen)
	}
	if at.IsZero() {
		at = ws.now()
	}
	return c.AddReading(domain.TempReading{At: at, Value: value}), nil
}

// IssueStandbyTransfer implements rule 3: a standby container is reallocated and
// the original container is frozen so it cannot be loaded. This is invoked by
// the background monitor after a 10-minute continuous breach, or manually.
func (ws *WarehouseService) IssueStandbyTransfer(containerID domain.ContainerID, reason string) (*domain.TransferOrder, error) {
	ws.Store.Lock()
	defer ws.Store.Unlock()

	c, ok := ws.Store.Containers[containerID]
	if !ok {
		return nil, fmt.Errorf("%w: container %s", domain.ErrNotFound, containerID)
	}
	if c.Frozen {
		return nil, fmt.Errorf("%w: container already frozen", domain.ErrInvalidState)
	}
	to := &domain.TransferOrder{
		ID:          ws.Store.NextID("TRF"),
		ContainerID: containerID,
		BookingID:   c.BookingID,
		Reason:      reason,
		IssuedAt:    ws.now(),
	}
	ws.Store.Transfers[to.ID] = to
	c.Freeze()
	c.TransferOrderID = to.ID
	ws.notify(domain.RoleWarehouse, "standby_transfer",
		fmt.Sprintf("standby transfer %s issued for container %s; original frozen", to.ID, containerID))
	return to, nil
}

// Container fetches a container by id.
func (ws *WarehouseService) Container(containerID domain.ContainerID) (*domain.TempContainer, error) {
	ws.Store.Lock()
	defer ws.Store.Unlock()
	c, ok := ws.Store.Containers[containerID]
	if !ok {
		return nil, fmt.Errorf("%w: container %s", domain.ErrNotFound, containerID)
	}
	return c, nil
}
