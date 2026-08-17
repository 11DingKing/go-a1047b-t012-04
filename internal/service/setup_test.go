package service

import (
	"time"

	"arcticexpress/internal/domain"
	"arcticexpress/internal/store"
)

// testBase is the anchor time used across service tests.
var testBase = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

// fixture wires a store, a controllable clock, and all services for a test.
type fixture struct {
	store     *store.Store
	clock     *domain.FakeClock
	booking   *BookingService
	route     *RouteService
	warehouse *WarehouseService
	dispatch  *DispatchService
	customs   *CustomsService
	lock      *LockService
}

func newFixture() *fixture {
	st := store.New()
	clk := domain.NewFakeClock(testBase)
	return &fixture{
		store:     st,
		clock:     clk,
		booking:   NewBookingService(st, clk),
		route:     NewRouteService(st, clk),
		warehouse: NewWarehouseService(st, clk),
		dispatch:  NewDispatchService(st, clk),
		customs:   NewCustomsService(st, clk),
		lock:      NewLockService(st, clk),
	}
}

func (f *fixture) seedForwarder(id string, withBattery bool) *domain.Forwarder {
	fwd := domain.NewForwarder(domain.ForwarderID(id), id)
	if withBattery {
		fwd.Grant(domain.QualPowerBattery)
	}
	f.store.Lock()
	defer f.store.Unlock()
	f.store.Forwarders[fwd.ID] = fwd
	return fwd
}

// seedVoyage creates a vessel, port and a single-call voyage so FirstCall works.
func (f *fixture) seedVoyage(id string, cap int, gateOpen, cutoff, etd time.Time) *domain.Voyage {
	f.store.Lock()
	defer f.store.Unlock()
	f.store.Vessels["VSEL"] = &domain.Vessel{ID: "VSEL", Name: "Arctic Sun", TempCapacityTEU: cap}
	f.store.Ports["PGBG"] = &domain.Port{ID: "PGBG", Code: "GBG", Name: "Gdynia"}
	v := &domain.Voyage{
		ID:           domain.VoyageID(id),
		VesselID:     "VSEL",
		VoyageNumber: id,
		TempCapacity: cap,
		Status:       domain.VoyageScheduled,
		PortCalls: []domain.PortCall{{
			PortID: "PGBG", Sequence: 1,
			GateOpen: gateOpen, Cutoff: cutoff, ETD: etd,
		}},
	}
	f.store.Voyages[v.ID] = v
	return v
}

// createAndConfirmBooking is a happy-path helper: create, deposit, confirm.
func (f *fixture) createAndConfirmBooking(voyage, fwd string, submittedAt time.Time) *domain.Booking {
	b, err := f.booking.CreateBooking(domain.VoyageID(voyage), domain.ForwarderID(fwd), domain.CargoPowerBattery, 1, true, submittedAt)
	if err != nil {
		panic(err)
	}
	if err := f.booking.PayDeposit(b.ID); err != nil {
		panic(err)
	}
	cb, err := f.booking.ConfirmBooking(b.ID)
	if err != nil {
		panic(err)
	}
	return cb
}

// bookingLockKey mirrors lockKeyBooking for cross-role lock tests.
func bookingLockKey(id domain.BookingID) string { return "booking:" + string(id) }
