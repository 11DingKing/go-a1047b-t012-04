package service

import (
	"errors"
	"testing"
	"time"

	"arcticexpress/internal/domain"
)

func TestRule1TempControlledRequiresBatteryQualification(t *testing.T) {
	f := newFixture()
	f.seedVoyage("V1", 10, testBase.Add(100*time.Hour), testBase.Add(200*time.Hour), testBase.Add(210*time.Hour))
	f.seedForwarder("F1", false)

	// Without the power-battery qualification: temp-controlled booking is rejected.
	_, err := f.booking.CreateBooking("V1", "F1", domain.CargoPowerBattery, 1, true, testBase)
	if !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("expected ErrUnauthorized, got %v", err)
	}

	// Non-temp-controlled booking is allowed without the qualification.
	b, err := f.booking.CreateBooking("V1", "F1", domain.CargoPVComponent, 1, false, testBase)
	if err != nil {
		t.Fatalf("non-temp booking should succeed, got %v", err)
	}
	if b.TempControlled {
		t.Fatal("booking should not be temp controlled")
	}

	// Grant the qualification: temp-controlled booking now succeeds.
	f.seedForwarder("F2", true)
	b2, err := f.booking.CreateBooking("V1", "F2", domain.CargoPowerBattery, 1, true, testBase)
	if err != nil {
		t.Fatalf("temp booking with qualification should succeed, got %v", err)
	}
	if !b2.TempControlled {
		t.Fatal("booking should be temp controlled")
	}
}

func TestOversellAndLastBerthEarliestSubmissionWins(t *testing.T) {
	f := newFixture()
	// cap 2 -> oversell limit 3.
	f.seedVoyage("V1", 2, testBase.Add(1000*time.Hour), testBase.Add(2000*time.Hour), testBase.Add(2100*time.Hour))
	f.seedForwarder("F1", true)

	b1, _ := f.booking.CreateBooking("V1", "F1", domain.CargoPowerBattery, 1, true, testBase)                    // slot 1
	b2, _ := f.booking.CreateBooking("V1", "F1", domain.CargoPowerBattery, 1, true, testBase.Add(1*time.Minute)) // slot 2
	b3, _ := f.booking.CreateBooking("V1", "F1", domain.CargoPowerBattery, 1, true, testBase.Add(5*time.Minute)) // last slot (held==limit-1)

	// B4 was submitted earlier than B3 but arrives later: it should steal the last slot.
	b4, _ := f.booking.CreateBooking("V1", "F1", domain.CargoPowerBattery, 1, true, testBase.Add(2*time.Minute))

	if !b4.HoldsSlot() || !b4.IsLastSlot {
		t.Fatalf("earliest-submitted B4 should hold the last slot, got status=%s lastSlot=%v", b4.Status, b4.IsLastSlot)
	}
	if b3.HoldsSlot() {
		t.Fatalf("later-submitted B3 should have lost the last slot, status=%s", b3.Status)
	}
	if b3.Status != domain.BookingWaitlist {
		t.Fatalf("B3 should be waitlisted, got %s", b3.Status)
	}
	// B1/B2 unaffected.
	if !b1.HoldsSlot() || !b2.HoldsSlot() {
		t.Fatal("B1/B2 should still hold slots")
	}
	// A real-time waitlist notification must exist.
	notes := f.lock.Notifications(domain.RoleBookingSpecialist)
	found := false
	for _, n := range notes {
		if n.Subject == "waitlist" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected a waitlist notification for the demoted booking")
	}
}

func TestLastBerthLaterSubmissionGoesWaitlist(t *testing.T) {
	f := newFixture()
	f.seedVoyage("V1", 2, testBase.Add(1000*time.Hour), testBase.Add(2000*time.Hour), testBase.Add(2100*time.Hour))
	f.seedForwarder("F1", true)

	// Fill two regular slots, then the earliest-submitted booking takes the last slot.
	_, _ = f.booking.CreateBooking("V1", "F1", domain.CargoPowerBattery, 1, true, testBase.Add(9*time.Minute))
	_, _ = f.booking.CreateBooking("V1", "F1", domain.CargoPowerBattery, 1, true, testBase.Add(8*time.Minute))
	b3, _ := f.booking.CreateBooking("V1", "F1", domain.CargoPowerBattery, 1, true, testBase.Add(2*time.Minute)) // last slot (earliest)
	if !b3.IsLastSlot {
		t.Fatal("earliest-submitted B3 should hold the last slot")
	}
	// A later-submitted booking cannot steal and must waitlist.
	b4, _ := f.booking.CreateBooking("V1", "F1", domain.CargoPowerBattery, 1, true, testBase.Add(5*time.Minute))
	if b4.HoldsSlot() {
		t.Fatalf("later-submitted B4 should be waitlisted, got %s", b4.Status)
	}
	if b4.Status != domain.BookingWaitlist {
		t.Fatalf("B4 status = %s, want waitlist", b4.Status)
	}
	if !b3.IsLastSlot {
		t.Fatal("B3 should still hold the last slot (not demoted by a later submission)")
	}
}

func TestReleaseUnpaidFreesSlotAndPromotes(t *testing.T) {
	f := newFixture()
	f.seedVoyage("V1", 2, testBase.Add(1000*time.Hour), testBase.Add(2000*time.Hour), testBase.Add(2100*time.Hour))
	f.seedForwarder("F1", true)

	b1, _ := f.booking.CreateBooking("V1", "F1", domain.CargoPowerBattery, 1, true, testBase.Add(1*time.Minute))
	b2, _ := f.booking.CreateBooking("V1", "F1", domain.CargoPowerBattery, 1, true, testBase.Add(2*time.Minute))
	b3, _ := f.booking.CreateBooking("V1", "F1", domain.CargoPowerBattery, 1, true, testBase.Add(3*time.Minute)) // last slot
	b4, _ := f.booking.CreateBooking("V1", "F1", domain.CargoPowerBattery, 1, true, testBase.Add(4*time.Minute)) // waitlist
	if err := f.booking.PayDeposit(b3.ID); err != nil {
		t.Fatal(err)
	}

	// Release unpaid (pending) bookings: B3 (deposit_paid) is retained, B1/B2 released,
	// and the freed capacity promotes B4 from the waitlist.
	n := f.booking.ReleaseUnpaidBeforeGate("V1")
	if n != 2 {
		t.Fatalf("expected 2 unpaid releases, got %d", n)
	}
	if b1.Status != domain.BookingReleased || b2.Status != domain.BookingReleased {
		t.Fatalf("B1=%s B2=%s, both should be released", b1.Status, b2.Status)
	}
	if b3.Status != domain.BookingDepositPaid {
		t.Fatalf("paid B3 should be retained, got %s", b3.Status)
	}
	if !b4.HoldsSlot() {
		t.Fatalf("B4 should be promoted from waitlist, got %s", b4.Status)
	}
}

func TestWaitlistPromotionOnCancel(t *testing.T) {
	f := newFixture()
	f.seedVoyage("V1", 2, testBase.Add(1000*time.Hour), testBase.Add(2000*time.Hour), testBase.Add(2100*time.Hour))
	f.seedForwarder("F1", true)

	b1, _ := f.booking.CreateBooking("V1", "F1", domain.CargoPowerBattery, 1, true, testBase.Add(1*time.Minute))
	b2, _ := f.booking.CreateBooking("V1", "F1", domain.CargoPowerBattery, 1, true, testBase.Add(2*time.Minute))
	b3, _ := f.booking.CreateBooking("V1", "F1", domain.CargoPowerBattery, 1, true, testBase.Add(3*time.Minute)) // last slot
	b4, _ := f.booking.CreateBooking("V1", "F1", domain.CargoPowerBattery, 1, true, testBase.Add(4*time.Minute)) // waitlist
	if b4.Status != domain.BookingWaitlist {
		t.Fatalf("B4 should start waitlisted, got %s", b4.Status)
	}

	// Cancel the last-slot holder: B4 should be promoted into it.
	if err := f.booking.CancelBooking(b3.ID); err != nil {
		t.Fatal(err)
	}
	if b3.Status != domain.BookingCancelled {
		t.Fatalf("B3 should be cancelled, got %s", b3.Status)
	}
	if !b4.HoldsSlot() {
		t.Fatalf("B4 should be promoted to hold a slot, got %s", b4.Status)
	}
	if !b4.IsLastSlot {
		t.Fatal("promoted B4 should occupy the last slot")
	}
	if b1.HoldsSlot() == false || b2.HoldsSlot() == false {
		t.Fatal("B1/B2 should still hold slots")
	}
}

func TestRule5EditLockMutualExclusion(t *testing.T) {
	f := newFixture()
	f.seedVoyage("V1", 10, testBase.Add(1000*time.Hour), testBase.Add(2000*time.Hour), testBase.Add(2100*time.Hour))
	f.seedForwarder("F1", true)
	b, _ := f.booking.CreateBooking("V1", "F1", domain.CargoPowerBattery, 1, true, testBase)
	if err := f.booking.PayDeposit(b.ID); err != nil {
		t.Fatal(err)
	}

	key := bookingLockKey(b.ID)

	// Route ops holds the booking edit lock: the specialist cannot confirm.
	if err := f.lock.Acquire(key, domain.RoleRouteOps); err != nil {
		t.Fatal(err)
	}
	_, err := f.booking.ConfirmBooking(b.ID)
	if !errors.Is(err, domain.ErrLockHeld) {
		t.Fatalf("expected ErrLockHeld while route ops holds lock, got %v", err)
	}

	// Release: confirmation now proceeds.
	f.lock.Release(key)
	cb, err := f.booking.ConfirmBooking(b.ID)
	if err != nil {
		t.Fatalf("confirm should succeed after lock release, got %v", err)
	}
	if cb.Status != domain.BookingConfirmed {
		t.Fatalf("expected confirmed, got %s", cb.Status)
	}
}
