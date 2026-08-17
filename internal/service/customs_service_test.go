package service

import (
	"errors"
	"testing"
	"time"

	"arcticexpress/internal/domain"
)

// TestManifestConflictRollsBackFreezesAndRecovers exercises the failure-recovery
// flow: a parallel port-change bumps the manifest version while customs
// declares against a stale version. The manifest rolls back to the last
// confirmed snapshot, loading freezes, and dual confirmation unfreezes and
// recomputes the cutoff.
func TestManifestConflictRollsBackFreezesAndRecovers(t *testing.T) {
	f := newFixture()
	f.seedVoyage("V1", 4, testBase.Add(500*time.Hour), testBase.Add(2000*time.Hour), testBase.Add(2100*time.Hour))
	f.seedVoyage("V2", 4, testBase.Add(500*time.Hour), testBase.Add(2000*time.Hour), testBase.Add(2100*time.Hour))
	f.seedForwarder("F1", true)
	b := f.createAndConfirmBooking("V1", "F1", testBase)

	// 1. Customs prepares and declares against version 1 -> confirmed + snapshot.
	m, _ := f.customs.PrepareManifest("V1")
	declared, err := f.customs.DeclareCustoms("V1", m.Version)
	if err != nil {
		t.Fatalf("initial declare: %v", err)
	}
	if declared.Status != domain.ManifestConfirmed {
		t.Fatalf("status = %s, want confirmed", declared.Status)
	}
	confirmedVersion := declared.Version

	// 2. Route ops applies a Suez->Arctic port change, bumping the manifest version.
	pc, _ := f.route.RequestPortChange(b.ID, "V1", "V2", "suez_congestion")
	if _, err := f.route.ApplyPortChange(pc.ID); err != nil {
		t.Fatalf("apply port change: %v", err)
	}

	// 3. Customs re-declares against the stale version -> conflict.
	conflicted, err := f.customs.DeclareCustoms("V1", confirmedVersion)
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("expected ErrConflict, got %v", err)
	}
	if conflicted.Status != domain.ManifestFrozen {
		t.Fatalf("status = %s, want frozen", conflicted.Status)
	}
	if conflicted.Version != confirmedVersion {
		t.Fatalf("version should roll back to %d, got %d", confirmedVersion, conflicted.Version)
	}
	voyage, _ := f.route.Voyage("V1")
	if !voyage.ManifestFrozen {
		t.Fatal("source voyage should be frozen after manifest conflict")
	}
	// Booking operations are blocked while frozen.
	if _, err := f.booking.ConfirmBooking(domain.BookingID("nope")); err == nil {
		t.Fatal("expected error on frozen voyage op")
	}

	// 4. Route ops alone does not unfreeze.
	if _, err := f.customs.ConfirmManifest("V1", domain.RoleRouteOps); err != nil {
		t.Fatalf("route ops confirm: %v", err)
	}
	voyage, _ = f.route.Voyage("V1")
	if !voyage.ManifestFrozen {
		t.Fatal("voyage should still be frozen after only route ops confirmed")
	}

	// 5. Customs confirms too -> unfreeze + recompute cutoff.
	final, err := f.customs.ConfirmManifest("V1", domain.RoleCustoms)
	if err != nil {
		t.Fatalf("customs confirm: %v", err)
	}
	if final.Status != domain.ManifestReconfirmed {
		t.Fatalf("status = %s, want reconfirmed", final.Status)
	}
	voyage, _ = f.route.Voyage("V1")
	if voyage.ManifestFrozen {
		t.Fatal("voyage should be unfrozen after dual confirmation")
	}
	// Recomputed cutoff = now + 48h (well before the far-future ETD).
	want := f.clock.Now().Add(48 * time.Hour)
	if !final.Cutoff.Equal(want) {
		t.Fatalf("recomputed cutoff = %s, want %s", final.Cutoff.Format(time.RFC3339), want.Format(time.RFC3339))
	}
}

func TestManifestDeclareLocksCutoff(t *testing.T) {
	f := newFixture()
	cutoff := testBase.Add(200 * time.Hour)
	f.seedVoyage("V1", 4, cutoff.Add(-80*time.Hour), cutoff, cutoff.Add(10*time.Hour))
	f.seedForwarder("F1", true)
	f.createAndConfirmBooking("V1", "F1", testBase)

	m, _ := f.customs.PrepareManifest("V1")
	declared, err := f.customs.DeclareCustoms("V1", m.Version)
	if err != nil {
		t.Fatalf("declare: %v", err)
	}
	if !declared.Cutoff.Equal(cutoff) {
		t.Fatalf("cutoff = %s, want %s", declared.Cutoff, cutoff)
	}
	if !declared.CustomsConfirmed {
		t.Fatal("customs confirmation flag should be set")
	}
}
