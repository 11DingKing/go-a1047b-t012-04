package domain

import (
	"testing"
	"time"
)

var testBase = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

func TestForwarderBatteryQualification(t *testing.T) {
	f := NewForwarder("F1", "Acme")
	if f.HasBatteryQualification() {
		t.Fatal("forwarder should not hold battery qualification by default")
	}
	f.Grant(QualPowerBattery)
	if !f.HasBatteryQualification() {
		t.Fatal("grant did not confer battery qualification")
	}
	f.Revoke(QualPowerBattery)
	if f.HasBatteryQualification() {
		t.Fatal("revoke did not remove battery qualification")
	}
}

func TestVoyageOversellLimit(t *testing.T) {
	cases := []struct {
		cap, want int
	}{
		{2, 3}, // 2 + ceil(0.06) = 3
		{100, 103},
		{1, 2},   // 1 + ceil(0.03) = 2
		{33, 34}, // 33 + ceil(0.99) = 34
	}
	for _, c := range cases {
		v := Voyage{TempCapacity: c.cap}
		if got := v.OversellLimit(); got != c.want {
			t.Errorf("oversell limit for cap %d = %d, want %d", c.cap, got, c.want)
		}
	}
}

func TestVoyageCallsSortedRotation(t *testing.T) {
	v := Voyage{PortCalls: []PortCall{
		{PortID: "Hamburg", Sequence: 3},
		{PortID: "Felixstowe", Sequence: 1},
		{PortID: "Gdynia", Sequence: 4},
		{PortID: "Rotterdam", Sequence: 2},
	}}
	got := v.CallsSorted()
	want := []string{"Felixstowe", "Rotterdam", "Hamburg", "Gdynia"}
	for i, c := range got {
		if string(c.PortID) != want[i] {
			t.Fatalf("call %d = %s, want %s", i, c.PortID, want[i])
		}
	}
	first, ok := v.FirstCall()
	if !ok || first.PortID != "Felixstowe" {
		t.Fatalf("first call = %s ok=%v", first.PortID, ok)
	}
}

func TestBookingStatusTransitions(t *testing.T) {
	now := testBase
	b := &Booking{Status: BookingPending}
	if !b.HoldsSlot() {
		t.Fatal("pending booking should hold a slot")
	}
	b.PayDeposit()
	if b.Status != BookingDepositPaid {
		t.Fatalf("expected deposit_paid, got %s", b.Status)
	}
	b.Confirm(now)
	if b.Status != BookingConfirmed {
		t.Fatalf("expected confirmed, got %s", b.Status)
	}
	if b.ConfirmedAt != now {
		t.Fatal("confirmed timestamp not recorded")
	}
	b.Cancel()
	if b.HoldsSlot() {
		t.Fatal("cancelled booking should not hold a slot")
	}
}

func TestBookingPromotePreservesDeposit(t *testing.T) {
	b := &Booking{Status: BookingPending}
	b.PayDeposit()
	b.MarkWaitlist(testBase)
	if b.HoldsSlot() {
		t.Fatal("waitlisted booking should not hold a slot")
	}
	b.Promote(testBase.Add(time.Second))
	if b.Status != BookingDepositPaid {
		t.Fatalf("promoted deposit-paid booking should be deposit_paid, got %s", b.Status)
	}
	if b.DepositPaid != true {
		t.Fatal("deposit flag lost on promotion")
	}
}

func TestTempContainerOutOfBoundsDuration(t *testing.T) {
	c := &TempContainer{SetpointLow: 2, SetpointHigh: 8, Status: ContainerBound}
	start := testBase
	c.AddReading(TempReading{At: start, Value: 15}) // out of bounds
	if got := c.OutOfBoundsDuration(start.Add(9 * time.Minute)); got != 9*time.Minute {
		t.Fatalf("duration = %v, want 9m", got)
	}
	c.AddReading(TempReading{At: start.Add(10 * time.Minute), Value: 5}) // back in bounds
	if got := c.OutOfBoundsDuration(start.Add(11 * time.Minute)); got != 0 {
		t.Fatalf("duration after recovery = %v, want 0", got)
	}
}

func TestManifestRestoreConfirmed(t *testing.T) {
	m := NewManifest("V1")
	m.Entries = ManifestEntries("B1")
	m.Cutoff = testBase.Add(100 * time.Hour)
	m.MarkConfirmed()
	origVersion := m.Version
	// Simulate a parallel edit that bumps the version.
	m.BumpVersion()
	m.Entries = ManifestEntries("B1", "B2")
	if m.Version == origVersion {
		t.Fatal("version should have bumped")
	}
	// Conflict: roll back to the last confirmed snapshot.
	m.RestoreConfirmed()
	if m.Status != ManifestFrozen {
		t.Fatalf("expected frozen, got %s", m.Status)
	}
	if m.Version != origVersion {
		t.Fatalf("version not restored: %d", m.Version)
	}
	if len(m.Entries) != 1 {
		t.Fatalf("entries not restored: %v", m.Entries)
	}
}

// ManifestEntries is a tiny test helper to build an entry slice.
func ManifestEntries(ids ...BookingID) []ManifestEntry {
	out := make([]ManifestEntry, 0, len(ids))
	for _, id := range ids {
		out = append(out, ManifestEntry{BookingID: id})
	}
	return out
}
