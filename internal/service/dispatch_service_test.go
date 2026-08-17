package service

import (
	"testing"
	"time"

	"arcticexpress/internal/domain"
)

func TestBerthAllocationEarliestTimestampWins(t *testing.T) {
	f := newFixture()
	f.seedVoyage("V1", 4, testBase.Add(1000*time.Hour), testBase.Add(2000*time.Hour), testBase.Add(2100*time.Hour))
	f.seedVoyage("V2", 4, testBase.Add(1000*time.Hour), testBase.Add(2000*time.Hour), testBase.Add(2100*time.Hour))
	f.route.CreateBerth("BTH1", "PGBG")

	// V2 claims first with a later submission timestamp.
	allocated, loser, err := f.dispatch.AllocateBerth("BTH1", "V2", "Ice Bear", testBase.Add(2*time.Minute))
	if err != nil || !allocated || loser != "" {
		t.Fatalf("first claim should win the free berth: allocated=%v loser=%s err=%v", allocated, loser, err)
	}

	// V1 submits later in wall-clock but earlier in timestamp: it steals the berth.
	allocated, loser, err = f.dispatch.AllocateBerth("BTH1", "V1", "Polar Star", testBase.Add(1*time.Minute))
	if err != nil || !allocated || loser != "V2" {
		t.Fatalf("earlier-submitted V1 should steal: allocated=%v loser=%s err=%v", allocated, loser, err)
	}

	berth, _ := f.dispatch.Berth("BTH1")
	if berth.AllocatedVoyage != "V1" {
		t.Fatalf("berth should be held by V1, got %s", berth.AllocatedVoyage)
	}
	// The displaced voyage must be notified.
	notes := f.lock.Notifications(domain.RoleDispatcher)
	found := false
	for _, n := range notes {
		if n.Subject == "berth_waitlist" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected a berth waitlist notification for the displaced voyage")
	}
}

func TestBerthAllocationLaterSubmissionWaitlists(t *testing.T) {
	f := newFixture()
	f.seedVoyage("V1", 4, testBase.Add(1000*time.Hour), testBase.Add(2000*time.Hour), testBase.Add(2100*time.Hour))
	f.seedVoyage("V2", 4, testBase.Add(1000*time.Hour), testBase.Add(2000*time.Hour), testBase.Add(2100*time.Hour))
	f.route.CreateBerth("BTH2", "PGBG")

	if allocated, _, err := f.dispatch.AllocateBerth("BTH2", "V1", "Polar Star", testBase.Add(1*time.Minute)); err != nil || !allocated {
		t.Fatalf("first claim should win: %v %v", allocated, err)
	}
	// V2 submits later: cannot steal, enters the berth waitlist.
	allocated, _, err := f.dispatch.AllocateBerth("BTH2", "V2", "Ice Bear", testBase.Add(5*time.Minute))
	if allocated {
		t.Fatal("later-submitted V2 should not win the berth")
	}
	if err != nil {
		t.Fatalf("later submission should not error, got %v", err)
	}
	berth, _ := f.dispatch.Berth("BTH2")
	if berth.AllocatedVoyage != "V1" {
		t.Fatalf("berth should remain with V1, got %s", berth.AllocatedVoyage)
	}
}
