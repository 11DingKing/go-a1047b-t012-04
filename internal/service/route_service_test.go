package service

import (
	"errors"
	"testing"
	"time"

	"arcticexpress/internal/domain"
)

func TestPortChangeApprovedWithin48hDeadline(t *testing.T) {
	f := newFixture()
	cutoff := testBase.Add(100 * time.Hour)
	f.seedVoyage("V1", 4, cutoff.Add(-80*time.Hour), cutoff, cutoff.Add(10*time.Hour))
	f.seedVoyage("V2", 4, testBase.Add(1000*time.Hour), testBase.Add(2000*time.Hour), testBase.Add(2100*time.Hour))
	f.seedForwarder("F1", true)
	b := f.createAndConfirmBooking("V1", "F1", testBase)

	// 40h in: deadline is cutoff-48h = +52h, so this is within the window.
	f.clock.Set(testBase.Add(40 * time.Hour))
	pc, err := f.route.RequestPortChange(b.ID, "V1", "V2", "suez_congestion")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	appr, err := f.route.ApprovePortChange(pc.ID)
	if err != nil {
		t.Fatalf("approve should succeed within deadline: %v", err)
	}
	if appr.Status != domain.PCRouteOpsApproved {
		t.Fatalf("status = %s, want route_ops_approved", appr.Status)
	}
}

func TestPortChangeRejectedPast48hDeadline(t *testing.T) {
	f := newFixture()
	cutoff := testBase.Add(100 * time.Hour)
	f.seedVoyage("V1", 4, cutoff.Add(-80*time.Hour), cutoff, cutoff.Add(10*time.Hour))
	f.seedVoyage("V2", 4, testBase.Add(1000*time.Hour), testBase.Add(2000*time.Hour), testBase.Add(2100*time.Hour))
	f.seedForwarder("F1", true)
	b := f.createAndConfirmBooking("V1", "F1", testBase)

	// 60h in: past the cutoff-48h deadline (+52h).
	f.clock.Set(testBase.Add(60 * time.Hour))
	pc, err := f.route.RequestPortChange(b.ID, "V1", "V2", "suez_congestion")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	appr, err := f.route.ApprovePortChange(pc.ID)
	if !errors.Is(err, domain.ErrDeadlineExceeded) {
		t.Fatalf("expected ErrDeadlineExceeded past deadline, got %v", err)
	}
	if appr.Status != domain.PCRejected {
		t.Fatalf("status = %s, want rejected", appr.Status)
	}
}

func TestPortChangeApplyMovesBookingAndBumpsManifest(t *testing.T) {
	f := newFixture()
	cutoff := testBase.Add(200 * time.Hour)
	f.seedVoyage("V1", 4, cutoff.Add(-80*time.Hour), cutoff, cutoff.Add(10*time.Hour))
	f.seedVoyage("V2", 4, testBase.Add(1000*time.Hour), testBase.Add(2000*time.Hour), testBase.Add(2100*time.Hour))
	f.seedForwarder("F1", true)
	b := f.createAndConfirmBooking("V1", "F1", testBase)

	// Establish a confirmed manifest on the source voyage first.
	m, _ := f.customs.PrepareManifest("V1")
	if _, err := f.customs.DeclareCustoms("V1", m.Version); err != nil {
		t.Fatalf("declare: %v", err)
	}
	prevVersion := m.Version // captured by value before the port change mutates the manifest

	f.clock.Set(testBase.Add(10 * time.Hour))
	pc, _ := f.route.RequestPortChange(b.ID, "V1", "V2", "suez_congestion")
	applied, err := f.route.ApplyPortChange(pc.ID)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if applied.Status != domain.PCApplied {
		t.Fatalf("status = %s, want applied", applied.Status)
	}
	got, _ := f.booking.Booking(b.ID)
	if got.VoyageID != "V2" {
		t.Fatalf("booking should move to V2, got %s", got.VoyageID)
	}
	// Source manifest version must have advanced (the basis for conflict detection).
	src, _ := f.customs.Manifest("V1")
	if src.Version == prevVersion {
		t.Fatalf("source manifest version should have bumped after port change: %d == %d", src.Version, prevVersion)
	}
}
