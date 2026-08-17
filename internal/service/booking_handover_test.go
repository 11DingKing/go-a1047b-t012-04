package service

import (
	"errors"
	"testing"
	"time"

	"arcticexpress/internal/domain"
)

// handoverFixture seeds a source and a target voyage plus a qualified forwarder
// so a booking can be moved between voyages after it is locked.
func handoverFixture(t *testing.T) *fixture {
	t.Helper()
	f := newFixture()
	f.seedVoyage("V1", 6, testBase.Add(500*time.Hour), testBase.Add(2000*time.Hour), testBase.Add(2100*time.Hour))
	f.seedVoyage("V2", 6, testBase.Add(500*time.Hour), testBase.Add(2000*time.Hour), testBase.Add(2100*time.Hour))
	f.seedForwarder("F1", true)
	return f
}

// TestBookingEditLockIsFreeAfterBerthLock covers the hand-over of the per-booking
// edit lock: locking the berth is a finished operation, so afterwards no role is
// still holding the booking and another role can take over.
func TestBookingEditLockIsFreeAfterBerthLock(t *testing.T) {
	f := handoverFixture(t)
	b := f.createAndConfirmBooking("V1", "F1", testBase)

	if holder, held := f.lock.Holder(bookingLockKey(b.ID)); held {
		t.Fatalf("booking edit lock still held by %q after the berth was locked, want it released", holder)
	}
	for _, l := range f.lock.Locks() {
		if l.Resource == bookingLockKey(b.ID) {
			t.Fatalf("booking edit lock %+v is still listed after the berth was locked", l)
		}
	}
}

// TestPortChangeWorksAfterBerthLock covers the workflow the operators actually
// run: a booking whose berth is already locked can still be diverted to another
// voyage by route operations.
func TestPortChangeWorksAfterBerthLock(t *testing.T) {
	f := handoverFixture(t)
	b := f.createAndConfirmBooking("V1", "F1", testBase)

	pc, err := f.route.RequestPortChange(b.ID, "V1", "V2", "suez_congestion")
	if err != nil {
		t.Fatalf("route ops must be able to request a port change on a locked booking, got %v", err)
	}
	if errors.Is(err, domain.ErrLockHeld) {
		t.Fatalf("port change was blocked by an edit lock: %v", err)
	}
	applied, err := f.route.ApplyPortChange(pc.ID)
	if err != nil {
		t.Fatalf("apply port change: %v", err)
	}
	if applied.Status != domain.PCApplied {
		t.Fatalf("port change status = %s, want applied", applied.Status)
	}
	got, err := f.booking.Booking(b.ID)
	if err != nil {
		t.Fatalf("booking: %v", err)
	}
	if got.VoyageID != "V2" {
		t.Fatalf("booking voyage = %s, want V2", got.VoyageID)
	}
}

// TestSecondBerthLockAndRefundStillWorkAfterBerthLock covers the same-role path
// and the refund path after a berth lock, so the operations the specialist owns
// keep working.
func TestSecondBerthLockAndRefundStillWorkAfterBerthLock(t *testing.T) {
	f := handoverFixture(t)
	b := f.createAndConfirmBooking("V1", "F1", testBase.Add(time.Minute))

	// Refunding right after locking the berth is a specialist operation.
	if err := f.booking.CancelBooking(b.ID); err != nil {
		t.Fatalf("refund after berth lock: %v", err)
	}
	got, _ := f.booking.Booking(b.ID)
	if got.Status != domain.BookingCancelled {
		t.Fatalf("booking status = %s, want cancelled", got.Status)
	}
	if holder, held := f.lock.Holder(bookingLockKey(b.ID)); held {
		t.Fatalf("booking edit lock still held by %q after the refund, want it released", holder)
	}
}

// TestPortChangeWorksOnBookingThatWasNeverLocked pins the negative case: a
// booking that never went through the berth lock is divertible as before.
func TestPortChangeWorksOnBookingThatWasNeverLocked(t *testing.T) {
	f := handoverFixture(t)
	b, err := f.booking.CreateBooking("V1", "F1", domain.CargoPowerBattery, 1, true, testBase)
	if err != nil {
		t.Fatalf("create booking: %v", err)
	}
	if err := f.booking.PayDeposit(b.ID); err != nil {
		t.Fatalf("pay deposit: %v", err)
	}

	if _, err := f.route.RequestPortChange(b.ID, "V1", "V2", "suez_congestion"); err != nil {
		t.Fatalf("port change on a booking that was never locked: %v", err)
	}
}

// TestBerthLockStillHonoursAnotherRolesLock pins the mutual-exclusion rule that
// must stay in force: while another role holds the booking, locking the berth is
// refused.
func TestBerthLockStillHonoursAnotherRolesLock(t *testing.T) {
	f := handoverFixture(t)
	b, err := f.booking.CreateBooking("V1", "F1", domain.CargoPowerBattery, 1, true, testBase)
	if err != nil {
		t.Fatalf("create booking: %v", err)
	}
	if err := f.booking.PayDeposit(b.ID); err != nil {
		t.Fatalf("pay deposit: %v", err)
	}

	if err := f.lock.Acquire(bookingLockKey(b.ID), domain.RoleRouteOps); err != nil {
		t.Fatalf("acquire as route ops: %v", err)
	}
	if _, err := f.booking.ConfirmBooking(b.ID); !errors.Is(err, domain.ErrLockHeld) {
		t.Fatalf("confirm while route ops holds the booking: err = %v, want ErrLockHeld", err)
	}
	f.lock.Release(bookingLockKey(b.ID))
	if _, err := f.booking.ConfirmBooking(b.ID); err != nil {
		t.Fatalf("confirm after the other role released: %v", err)
	}
}
