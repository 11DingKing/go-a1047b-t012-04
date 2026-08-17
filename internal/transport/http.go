package transport

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"arcticexpress/internal/domain"
	"arcticexpress/internal/service"
	"arcticexpress/internal/store"
)

const ListenAddr = ":58021"

// Server wires the HTTP API to the application services.
type Server struct {
	Store     *store.Store
	Booking   *service.BookingService
	Route     *service.RouteService
	Warehouse *service.WarehouseService
	Dispatch  *service.DispatchService
	Customs   *service.CustomsService
	Lock      *service.LockService
	mux       *http.ServeMux
}

func NewServer(
	st *store.Store,
	booking *service.BookingService,
	route *service.RouteService,
	warehouse *service.WarehouseService,
	dispatch *service.DispatchService,
	customs *service.CustomsService,
	lock *service.LockService,
) *Server {
	s := &Server{
		Store: st, Booking: booking, Route: route,
		Warehouse: warehouse, Dispatch: dispatch, Customs: customs, Lock: lock,
		mux: http.NewServeMux(),
	}
	s.routes()
	return s
}

func (s *Server) Handler() http.Handler { return s.mux }

func (s *Server) routes() {
	s.mux.HandleFunc("GET /api/health", s.health)

	s.mux.HandleFunc("POST /api/forwarders", s.createForwarder)
	s.mux.HandleFunc("POST /api/forwarders/{id}/qualifications", s.grantQualification)

	s.mux.HandleFunc("POST /api/vessels", s.createVessel)
	s.mux.HandleFunc("POST /api/ports", s.createPort)
	s.mux.HandleFunc("POST /api/berths", s.createBerth)
	s.mux.HandleFunc("POST /api/berths/{id}/allocate", s.allocateBerth)

	s.mux.HandleFunc("POST /api/voyages", s.createVoyage)
	s.mux.HandleFunc("GET /api/voyages/{id}", s.getVoyage)

	s.mux.HandleFunc("POST /api/bookings", s.createBooking)
	s.mux.HandleFunc("POST /api/bookings/{id}/deposit", s.payDeposit)
	s.mux.HandleFunc("POST /api/bookings/{id}/confirm", s.confirmBooking)
	s.mux.HandleFunc("POST /api/bookings/{id}/cancel", s.cancelBooking)

	s.mux.HandleFunc("POST /api/containers", s.bindContainer)
	s.mux.HandleFunc("POST /api/containers/{id}/readings", s.recordReading)
	s.mux.HandleFunc("POST /api/containers/{id}/transfer", s.transferContainer)

	s.mux.HandleFunc("POST /api/portchanges", s.requestPortChange)
	s.mux.HandleFunc("POST /api/portchanges/{id}/approve", s.approvePortChange)
	s.mux.HandleFunc("POST /api/portchanges/{id}/apply", s.applyPortChange)

	s.mux.HandleFunc("POST /api/manifests/{voyageId}/prepare", s.prepareManifest)
	s.mux.HandleFunc("POST /api/manifests/{voyageId}/declare", s.declareManifest)
	s.mux.HandleFunc("POST /api/manifests/{voyageId}/confirm", s.confirmManifest)

	s.mux.HandleFunc("POST /api/locks", s.acquireLock)
	s.mux.HandleFunc("GET /api/notifications", s.listNotifications)
}

// ---- helpers ----

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func decodeJSON(r *http.Request, v any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

func statusFor(err error) int {
	switch {
	case errors.Is(err, domain.ErrNotFound):
		return http.StatusNotFound
	case errors.Is(err, domain.ErrUnauthorized):
		return http.StatusForbidden
	case errors.Is(err, domain.ErrConflict):
		return http.StatusConflict
	case errors.Is(err, domain.ErrLockHeld):
		return http.StatusLocked
	case errors.Is(err, domain.ErrCapacity):
		return http.StatusTooManyRequests
	case errors.Is(err, domain.ErrDeadlineExceeded), errors.Is(err, domain.ErrFrozen):
		return http.StatusUnprocessableEntity
	case errors.Is(err, domain.ErrInvalidState):
		return http.StatusBadRequest
	}
	return http.StatusInternalServerError
}

func writeError(w http.ResponseWriter, err error) {
	writeJSON(w, statusFor(err), map[string]string{"error": err.Error()})
}

func parseTime(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	return time.Parse(time.RFC3339, s)
}

// ---- handlers ----

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "service": "arctic-express"})
}

type createForwarderReq struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func (s *Server) createForwarder(w http.ResponseWriter, r *http.Request) {
	var req createForwarderReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, err)
		return
	}
	s.Store.Lock()
	defer s.Store.Unlock()
	f := domain.NewForwarder(domain.ForwarderID(req.ID), req.Name)
	s.Store.Forwarders[f.ID] = f
	writeJSON(w, http.StatusCreated, f)
}

type grantQualReq struct {
	Qualification string `json:"qualification"`
}

func (s *Server) grantQualification(w http.ResponseWriter, r *http.Request) {
	var req grantQualReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, err)
		return
	}
	id := domain.ForwarderID(r.PathValue("id"))
	s.Store.Lock()
	defer s.Store.Unlock()
	f, ok := s.Store.Forwarders[id]
	if !ok {
		writeError(w, domain.ErrNotFound)
		return
	}
	f.Grant(domain.Qualification(req.Qualification))
	writeJSON(w, http.StatusOK, f)
}

type createVesselReq struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Capacity int    `json:"capacity"`
}

func (s *Server) createVessel(w http.ResponseWriter, r *http.Request) {
	var req createVesselReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, err)
		return
	}
	v := s.Route.CreateVessel(domain.VesselID(req.ID), req.Name, req.Capacity)
	writeJSON(w, http.StatusCreated, v)
}

type createPortReq struct {
	ID   string `json:"id"`
	Code string `json:"code"`
	Name string `json:"name"`
}

func (s *Server) createPort(w http.ResponseWriter, r *http.Request) {
	var req createPortReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, err)
		return
	}
	p := s.Route.CreatePort(domain.PortID(req.ID), req.Code, req.Name)
	writeJSON(w, http.StatusCreated, p)
}

type createBerthReq struct {
	ID     string `json:"id"`
	PortID string `json:"port_id"`
}

func (s *Server) createBerth(w http.ResponseWriter, r *http.Request) {
	var req createBerthReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, err)
		return
	}
	b := s.Route.CreateBerth(domain.BerthID(req.ID), domain.PortID(req.PortID))
	writeJSON(w, http.StatusCreated, b)
}

type allocateBerthReq struct {
	VoyageID    string `json:"voyage_id"`
	Ship        string `json:"ship"`
	RequestedAt string `json:"requested_at"`
}

func (s *Server) allocateBerth(w http.ResponseWriter, r *http.Request) {
	var req allocateBerthReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, err)
		return
	}
	at, err := parseTime(req.RequestedAt)
	if err != nil {
		writeError(w, domain.ErrInvalidState)
		return
	}
	allocated, loser, err := s.Dispatch.AllocateBerth(domain.BerthID(r.PathValue("id")), domain.VoyageID(req.VoyageID), req.Ship, at)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"allocated":    allocated,
		"loser_voyage": string(loser),
		"berth_id":     r.PathValue("id"),
		"voyage_id":    req.VoyageID,
	})
}

type portCallReq struct {
	PortID   string `json:"port_id"`
	Sequence int    `json:"sequence"`
	ETA      string `json:"eta"`
	ETD      string `json:"etd"`
	GateOpen string `json:"gate_open"`
	Cutoff   string `json:"cutoff"`
}

type createVoyageReq struct {
	ID       string        `json:"id"`
	VesselID string        `json:"vessel_id"`
	Number   string        `json:"number"`
	Calls    []portCallReq `json:"calls"`
	Capacity int           `json:"capacity"`
}

func (s *Server) createVoyage(w http.ResponseWriter, r *http.Request) {
	var req createVoyageReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, err)
		return
	}
	calls := make([]domain.PortCall, 0, len(req.Calls))
	for _, c := range req.Calls {
		eta, err := parseTime(c.ETA)
		if err != nil {
			writeError(w, domain.ErrInvalidState)
			return
		}
		etd, err := parseTime(c.ETD)
		if err != nil {
			writeError(w, domain.ErrInvalidState)
			return
		}
		gate, err := parseTime(c.GateOpen)
		if err != nil {
			writeError(w, domain.ErrInvalidState)
			return
		}
		cut, err := parseTime(c.Cutoff)
		if err != nil {
			writeError(w, domain.ErrInvalidState)
			return
		}
		calls = append(calls, domain.PortCall{
			PortID: domain.PortID(c.PortID), Sequence: c.Sequence,
			ETA: eta, ETD: etd, GateOpen: gate, Cutoff: cut,
		})
	}
	v, err := s.Route.CreateVoyage(domain.VoyageID(req.ID), domain.VesselID(req.VesselID), req.Number, calls, req.Capacity)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, v)
}

func (s *Server) getVoyage(w http.ResponseWriter, r *http.Request) {
	held, limit, err := s.Route.RemainingCapacity(domain.VoyageID(r.PathValue("id")))
	if err != nil {
		writeError(w, err)
		return
	}
	v, err := s.Route.Voyage(domain.VoyageID(r.PathValue("id")))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"voyage":         v,
		"held_slots":     held,
		"oversell_limit": limit,
		"remaining":      limit - held,
	})
}

type createBookingReq struct {
	VoyageID       string `json:"voyage_id"`
	ForwarderID    string `json:"forwarder_id"`
	Cargo          string `json:"cargo"`
	TEU            int    `json:"teu"`
	TempControlled bool   `json:"temp_controlled"`
	SubmittedAt    string `json:"submitted_at"`
}

func (s *Server) createBooking(w http.ResponseWriter, r *http.Request) {
	var req createBookingReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, err)
		return
	}
	at, err := parseTime(req.SubmittedAt)
	if err != nil {
		writeError(w, domain.ErrInvalidState)
		return
	}
	b, err := s.Booking.CreateBooking(domain.VoyageID(req.VoyageID), domain.ForwarderID(req.ForwarderID), domain.CargoCategory(req.Cargo), req.TEU, req.TempControlled, at)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, b)
}

func (s *Server) payDeposit(w http.ResponseWriter, r *http.Request) {
	if err := s.Booking.PayDeposit(domain.BookingID(r.PathValue("id"))); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deposit_paid"})
}

func (s *Server) confirmBooking(w http.ResponseWriter, r *http.Request) {
	b, err := s.Booking.ConfirmBooking(domain.BookingID(r.PathValue("id")))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, b)
}

func (s *Server) cancelBooking(w http.ResponseWriter, r *http.Request) {
	if err := s.Booking.CancelBooking(domain.BookingID(r.PathValue("id"))); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "cancelled"})
}

type containerReq struct {
	BookingID    string  `json:"booking_id"`
	ContainerID  string  `json:"container_id"`
	SetpointLow  float64 `json:"setpoint_low"`
	SetpointHigh float64 `json:"setpoint_high"`
}

func (s *Server) bindContainer(w http.ResponseWriter, r *http.Request) {
	var req containerReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, err)
		return
	}
	c, err := s.Warehouse.BindContainer(domain.BookingID(req.BookingID), domain.ContainerID(req.ContainerID), req.SetpointLow, req.SetpointHigh)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, c)
}

type readingReq struct {
	Value float64 `json:"value"`
	At    string  `json:"at"`
}

func (s *Server) recordReading(w http.ResponseWriter, r *http.Request) {
	var req readingReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, err)
		return
	}
	at, err := parseTime(req.At)
	if err != nil {
		writeError(w, domain.ErrInvalidState)
		return
	}
	out, err := s.Warehouse.RecordReading(domain.ContainerID(r.PathValue("id")), req.Value, at)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"out_of_bounds": out})
}

type transferReq struct {
	Reason string `json:"reason"`
}

func (s *Server) transferContainer(w http.ResponseWriter, r *http.Request) {
	var req transferReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, err)
		return
	}
	to, err := s.Warehouse.IssueStandbyTransfer(domain.ContainerID(r.PathValue("id")), req.Reason)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, to)
}

type portChangeReq struct {
	BookingID string `json:"booking_id"`
	Source    string `json:"source_voyage"`
	Target    string `json:"target_voyage"`
	Reason    string `json:"reason"`
}

func (s *Server) requestPortChange(w http.ResponseWriter, r *http.Request) {
	var req portChangeReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, err)
		return
	}
	pc, err := s.Route.RequestPortChange(domain.BookingID(req.BookingID), domain.VoyageID(req.Source), domain.VoyageID(req.Target), req.Reason)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, pc)
}

func (s *Server) approvePortChange(w http.ResponseWriter, r *http.Request) {
	pc, err := s.Route.ApprovePortChange(r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, pc)
}

func (s *Server) applyPortChange(w http.ResponseWriter, r *http.Request) {
	pc, err := s.Route.ApplyPortChange(r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, pc)
}

func (s *Server) prepareManifest(w http.ResponseWriter, r *http.Request) {
	m, err := s.Customs.PrepareManifest(domain.VoyageID(r.PathValue("voyageId")))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, m)
}

type declareReq struct {
	ExpectedVersion int `json:"expected_version"`
}

func (s *Server) declareManifest(w http.ResponseWriter, r *http.Request) {
	var req declareReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, err)
		return
	}
	m, err := s.Customs.DeclareCustoms(domain.VoyageID(r.PathValue("voyageId")), req.ExpectedVersion)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, m)
}

type confirmReq struct {
	Role string `json:"role"`
}

func (s *Server) confirmManifest(w http.ResponseWriter, r *http.Request) {
	var req confirmReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, err)
		return
	}
	m, err := s.Customs.ConfirmManifest(domain.VoyageID(r.PathValue("voyageId")), domain.Role(req.Role))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, m)
}

type lockReq struct {
	Resource string `json:"resource"`
	Role     string `json:"role"`
}

func (s *Server) acquireLock(w http.ResponseWriter, r *http.Request) {
	var req lockReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, err)
		return
	}
	if err := s.Lock.Acquire(req.Resource, domain.Role(req.Role)); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "locked", "resource": req.Resource})
}

func (s *Server) listNotifications(w http.ResponseWriter, r *http.Request) {
	role := domain.Role(r.URL.Query().Get("role"))
	writeJSON(w, http.StatusOK, s.Lock.Notifications(role))
}
