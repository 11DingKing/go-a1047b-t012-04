package worker

import (
	"context"
	"time"

	"arcticexpress/internal/domain"
	"arcticexpress/internal/service"
	"arcticexpress/internal/store"
)

// DepositReleaseWindow is the rule-2 lead time: unpaid bookings are released
// once this much time remains before the loading port's gate opens.
const DepositReleaseWindow = 72 * time.Hour

// TempBreachWindow is the rule-3 threshold: a continuous out-of-bounds reading
// lasting this long triggers a standby transfer and freezes the original unit.
const TempBreachWindow = 10 * time.Minute

// Worker runs the periodic background tasks for the platform: the 72h deposit
// auto-release and the 10-minute temperature-breach standby transfer.
type Worker struct {
	Store     *store.Store
	Clock     domain.Clock
	Booking   *service.BookingService
	Warehouse *service.WarehouseService
}

func New(s *store.Store, c domain.Clock, b *service.BookingService, w *service.WarehouseService) *Worker {
	return &Worker{Store: s, Clock: c, Booking: b, Warehouse: w}
}

// TickDepositRelease releases unpaid (pending) bookings whose voyage has entered
// the 72h pre-gate window. It returns the number of bookings released.
func (wk *Worker) TickDepositRelease() int {
	now := wk.Clock.Now()
	type pending struct {
		id domain.VoyageID
	}
	var queue []pending
	wk.Store.Lock()
	for _, v := range wk.Store.Voyages {
		if v.Status == domain.VoyageClosed {
			continue
		}
		gate, ok := v.GateOpen()
		if !ok {
			continue
		}
		if !now.Before(gate.Add(-DepositReleaseWindow)) {
			queue = append(queue, pending{v.ID})
		}
	}
	wk.Store.Unlock()

	released := 0
	for _, q := range queue {
		released += wk.Booking.ReleaseUnpaidBeforeGate(q.id)
	}
	return released
}

// TickTempMonitor issues standby transfers for containers that have been
// continuously out of bounds for at least 10 minutes. It returns the number of
// transfers issued.
func (wk *Worker) TickTempMonitor() int {
	now := wk.Clock.Now()
	type breach struct {
		id     domain.ContainerID
		reason string
	}
	var queue []breach
	wk.Store.Lock()
	for _, c := range wk.Store.Containers {
		if c.Frozen {
			continue
		}
		if c.OutOfBoundsDuration(now) >= TempBreachWindow {
			queue = append(queue, breach{
				id:     c.ID,
				reason: "temperature out of bounds >= 10 minutes",
			})
		}
	}
	wk.Store.Unlock()

	issued := 0
	for _, b := range queue {
		if _, err := wk.Warehouse.IssueStandbyTransfer(b.id, b.reason); err == nil {
			issued++
		}
	}
	return issued
}

// Tick runs both periodic sweeps once.
func (wk *Worker) Tick() (released, transfers int) {
	return wk.TickDepositRelease(), wk.TickTempMonitor()
}

// Start runs the worker on a fixed interval until the context is cancelled.
func (wk *Worker) Start(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			wk.Tick()
		}
	}
}
