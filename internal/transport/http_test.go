package transport

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"arcticexpress/internal/domain"
	"arcticexpress/internal/service"
	"arcticexpress/internal/store"
)

func newTestServer(t *testing.T) (*httptest.Server, *store.Store) {
	t.Helper()
	st := store.New()
	clk := domain.NewFakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	srv := NewServer(st,
		service.NewBookingService(st, clk),
		service.NewRouteService(st, clk),
		service.NewWarehouseService(st, clk),
		service.NewDispatchService(st, clk),
		service.NewCustomsService(st, clk),
		service.NewLockService(st, clk),
	)
	ts := httptest.NewServer(srv.Handler())
	return ts, st
}

func do(t *testing.T, ts *httptest.Server, method, path string, body any) (int, map[string]any, []byte) {
	t.Helper()
	var rdr *bytes.Buffer
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		rdr = bytes.NewBuffer(buf)
	} else {
		rdr = &bytes.Buffer{}
	}
	req, err := http.NewRequest(method, ts.URL+path, rdr)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	return resp.StatusCode, out, raw
}

func arcticCalls() []map[string]any {
	return []map[string]any{{
		"port_id": "PGBG", "sequence": 1,
		"eta": "2026-06-01T00:00:00Z", "etd": "2026-06-11T00:00:00Z",
		"gate_open": "2026-06-01T00:00:00Z", "cutoff": "2026-06-10T00:00:00Z",
	}}
}

func TestHTTPHealth(t *testing.T) {
	ts, _ := newTestServer(t)
	defer ts.Close()
	status, body, _ := do(t, ts, "GET", "/api/health", nil)
	if status != http.StatusOK {
		t.Fatalf("health status = %d", status)
	}
	if body["status"] != "ok" {
		t.Fatalf("health body = %v", body)
	}
}

func TestHTTPBookingHappyPathAndRule1(t *testing.T) {
	ts, _ := newTestServer(t)
	defer ts.Close()

	base := "2026-01-01T00:00:00Z"

	if s, _, _ := do(t, ts, "POST", "/api/forwarders", map[string]string{"id": "F1", "name": "Acme"}); s != http.StatusCreated {
		t.Fatalf("create forwarder status = %d", s)
	}
	if s, _, _ := do(t, ts, "POST", "/api/forwarders/F1/qualifications", map[string]string{"qualification": "power_battery"}); s != http.StatusOK {
		t.Fatalf("grant qual status = %d", s)
	}
	if s, _, _ := do(t, ts, "POST", "/api/vessels", map[string]any{"id": "VSEL", "name": "Arctic Sun", "capacity": 4}); s != http.StatusCreated {
		t.Fatalf("create vessel status = %d", s)
	}
	if s, _, _ := do(t, ts, "POST", "/api/ports", map[string]string{"id": "PGBG", "code": "GBG", "name": "Gdynia"}); s != http.StatusCreated {
		t.Fatalf("create port status = %d", s)
	}
	if s, _, _ := do(t, ts, "POST", "/api/berths", map[string]string{"id": "BTH1", "port_id": "PGBG"}); s != http.StatusCreated {
		t.Fatalf("create berth status = %d", s)
	}
	voyageBody := map[string]any{
		"id": "V1", "vessel_id": "VSEL", "number": "ARCTIC-01", "capacity": 4,
		"calls": arcticCalls(),
	}
	if s, _, _ := do(t, ts, "POST", "/api/voyages", voyageBody); s != http.StatusCreated {
		t.Fatalf("create voyage status = %d", s)
	}

	// Rule 1 error path: a forwarder without the qualification cannot book a temp berth.
	do(t, ts, "POST", "/api/forwarders", map[string]string{"id": "F2", "name": "NoQual"})
	s, e, _ := do(t, ts, "POST", "/api/bookings", map[string]any{
		"voyage_id": "V1", "forwarder_id": "F2", "cargo": "power_battery",
		"teu": 1, "temp_controlled": true, "submitted_at": base,
	})
	if s != http.StatusForbidden {
		t.Fatalf("unqualified temp booking status = %d body=%v", s, e)
	}

	// Happy path: qualified forwarder books, pays deposit, confirms.
	s, b, _ := do(t, ts, "POST", "/api/bookings", map[string]any{
		"voyage_id": "V1", "forwarder_id": "F1", "cargo": "power_battery",
		"teu": 1, "temp_controlled": true, "submitted_at": base,
	})
	if s != http.StatusCreated {
		t.Fatalf("create booking status = %d body=%v", s, b)
	}
	bookingID, ok := b["ID"].(string)
	if !ok || bookingID == "" {
		t.Fatalf("missing booking id in response: %v", b)
	}
	if s, _, _ := do(t, ts, "POST", "/api/bookings/"+bookingID+"/deposit", nil); s != http.StatusOK {
		t.Fatalf("deposit status = %d", s)
	}
	if s, cb, _ := do(t, ts, "POST", "/api/bookings/"+bookingID+"/confirm", nil); s != http.StatusOK {
		t.Fatalf("confirm status = %d body=%v", s, cb)
	}
	s, v, _ := do(t, ts, "GET", "/api/voyages/V1", nil)
	if s != http.StatusOK {
		t.Fatalf("get voyage status = %d", s)
	}
	held, _ := v["held_slots"].(float64)
	if held != 1 {
		t.Fatalf("held_slots = %v, want 1", v["held_slots"])
	}
}

func TestHTTPBerthAllocation(t *testing.T) {
	ts, _ := newTestServer(t)
	defer ts.Close()

	do(t, ts, "POST", "/api/vessels", map[string]any{"id": "VSEL", "name": "Arctic Sun", "capacity": 4})
	do(t, ts, "POST", "/api/ports", map[string]string{"id": "PGBG", "code": "GBG", "name": "Gdynia"})
	do(t, ts, "POST", "/api/berths", map[string]string{"id": "BTH9", "port_id": "PGBG"})
	for _, id := range []string{"VA", "VB"} {
		s, _, _ := do(t, ts, "POST", "/api/voyages", map[string]any{
			"id": id, "vessel_id": "VSEL", "number": id, "capacity": 4,
			"calls": arcticCalls(),
		})
		if s != http.StatusCreated {
			t.Fatalf("create voyage %s status = %d", id, s)
		}
	}

	// VB claims first with a later timestamp.
	s, b, _ := do(t, ts, "POST", "/api/berths/BTH9/allocate", map[string]any{
		"voyage_id": "VB", "ship": "Ice Bear", "requested_at": "2026-01-01T00:02:00Z",
	})
	if s != http.StatusOK || b["allocated"] != true {
		t.Fatalf("first allocate: status=%d body=%v", s, b)
	}
	// VA (earlier timestamp) steals the berth.
	s, b, _ = do(t, ts, "POST", "/api/berths/BTH9/allocate", map[string]any{
		"voyage_id": "VA", "ship": "Polar Star", "requested_at": "2026-01-01T00:01:00Z",
	})
	if s != http.StatusOK || b["allocated"] != true {
		t.Fatalf("steal allocate: status=%d body=%v", s, b)
	}
	if loser, _ := b["loser_voyage"].(string); loser != "VB" {
		t.Fatalf("loser_voyage = %v, want VB", b["loser_voyage"])
	}
	// A dispatcher waitlist notification exists.
	_, _, raw := do(t, ts, "GET", "/api/notifications?role=dispatcher", nil)
	if !strings.Contains(string(raw), "berth_waitlist") {
		t.Fatalf("expected a berth_waitlist notification, got %s", string(raw))
	}
}
