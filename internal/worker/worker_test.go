package worker

import (
	"testing"
	"time"

	"arcticexpress/internal/domain"
	"arcticexpress/internal/service"
	"arcticexpress/internal/store"
)

type wfix struct {
	st      *store.Store
	clk     *domain.FakeClock
	booking *service.BookingService
	route   *service.RouteService
	wh      *service.WarehouseService
	wk      *Worker
}

func newWfix() *wfix {
	st := store.New()
	clk := domain.NewFakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	booking := service.NewBookingService(st, clk)
	route := service.NewRouteService(st, clk)
	wh := service.NewWarehouseService(st, clk)
	wk := New(st, clk, booking, wh)
	return &wfix{st: st, clk: clk, booking: booking, route: route, wh: wh, wk: wk}
}

func (w *wfix) seedVoyage(id string, cap int, gate, cutoff, etd time.Time) {
	w.st.Lock()
	defer w.st.Unlock()
	w.st.Vessels["VSEL"] = &domain.Vessel{ID: "VSEL", Name: "Arctic Sun", TempCapacityTEU: cap}
	w.st.Ports["PGBG"] = &domain.Port{ID: "PGBG", Code: "GBG", Name: "Gdynia"}
	w.st.Voyages[domain.VoyageID(id)] = &domain.Voyage{
		ID:           domain.VoyageID(id),
		VesselID:     "VSEL",
		VoyageNumber: id,
		TempCapacity: cap,
		Status:       domain.VoyageScheduled,
		PortCalls: []domain.PortCall{{
			PortID: "PGBG", Sequence: 1, GateOpen: gate, Cutoff: cutoff, ETD: etd,
		}},
	}
}

func (w *wfix) seedForwarder(id string, bat bool) {
	f := domain.NewForwarder(domain.ForwarderID(id), id)
	if bat {
		f.Grant(domain.QualPowerBattery)
	}
	w.st.Lock()
	defer w.st.Unlock()
	w.st.Forwarders[f.ID] = f
}

func (w *wfix) confirmedBooking(voyage, fwd string, submitted time.Time) *domain.Booking {
	b, _ := w.booking.CreateBooking(domain.VoyageID(voyage), domain.ForwarderID(fwd), domain.CargoPowerBattery, 1, true, submitted)
	_ = w.booking.PayDeposit(b.ID)
	cb, _ := w.booking.ConfirmBooking(b.ID)
	return cb
}

func TestWorkerDepositRelease72hWindow(t *testing.T) {
	w := newWfix()
	gate := w.clk.Now().Add(100 * time.Hour)
	w.seedVoyage("V1", 8, gate, gate.Add(100*time.Hour), gate.Add(110*time.Hour))
	w.seedForwarder("F1", true)
	b, _ := w.booking.CreateBooking("V1", "F1", domain.CargoPowerBattery, 1, true, w.clk.Now())
	if b.Status != domain.BookingPending {
		t.Fatalf("expected pending, got %s", b.Status)
	}

	// 73h before gate: still outside the release window (gate - 72h is the threshold).
	w.clk.Set(gate.Add(-73 * time.Hour))
	if r := w.wk.TickDepositRelease(); r != 0 {
		t.Fatalf("expected 0 releases 73h before gate, got %d", r)
	}
	if b.Status != domain.BookingPending {
		t.Fatalf("booking should still be pending, got %s", b.Status)
	}

	// 71h before gate: inside the window, unpaid booking is auto-released.
	w.clk.Set(gate.Add(-71 * time.Hour))
	if r := w.wk.TickDepositRelease(); r != 1 {
		t.Fatalf("expected 1 release inside window, got %d", r)
	}
	if b.Status != domain.BookingReleased {
		t.Fatalf("booking should be released, got %s", b.Status)
	}
}

func TestWorkerTempMonitorIssuesStandbyTransfer(t *testing.T) {
	w := newWfix()
	gate := w.clk.Now().Add(500 * time.Hour)
	w.seedVoyage("V1", 8, gate, gate.Add(100*time.Hour), gate.Add(110*time.Hour))
	w.seedForwarder("F1", true)
	b := w.confirmedBooking("V1", "F1", w.clk.Now())

	c, err := w.wh.BindContainer(b.ID, "CNT1", 2, 8)
	if err != nil {
		t.Fatalf("bind: %v", err)
	}

	// Out-of-bounds reading 11 minutes ago: a 10-minute breach is now in effect.
	breachAt := w.clk.Now().Add(-11 * time.Minute)
	if _, err := w.wh.RecordReading("CNT1", 15, breachAt); err != nil {
		t.Fatalf("reading: %v", err)
	}

	// A normal container is bound too: it must not be transferred.
	b2 := w.confirmedBooking("V1", "F1", w.clk.Now().Add(time.Second))
	w.wh.BindContainer(b2.ID, "CNT2", 2, 8)
	w.wh.RecordReading("CNT2", 5, w.clk.Now().Add(-11*time.Minute))

	if r := w.wk.TickTempMonitor(); r != 1 {
		t.Fatalf("expected 1 standby transfer, got %d", r)
	}
	c, _ = w.wh.Container("CNT1")
	if !c.Frozen || c.Status != domain.ContainerFrozen {
		t.Fatalf("breached container should be frozen, status=%s frozen=%v", c.Status, c.Frozen)
	}
	if c.TransferOrderID == "" {
		t.Fatal("standby transfer order id should be recorded on the container")
	}
	c2, _ := w.wh.Container("CNT2")
	if c2.Frozen {
		t.Fatal("normal container should not be frozen")
	}

	// Idempotent: re-running the monitor does not double-issue transfers.
	if r := w.wk.TickTempMonitor(); r != 0 {
		t.Fatalf("expected 0 transfers on second sweep, got %d", r)
	}
}
