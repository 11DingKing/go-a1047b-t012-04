package domain

import "time"

// Role identifies the actor holding an edit lock or receiving a notification.
type Role string

const (
	RoleBookingSpecialist Role = "booking_specialist"
	RoleDispatcher        Role = "dispatcher"
	RoleWarehouse         Role = "warehouse"
	RoleRouteOps          Role = "route_ops"
	RoleCustoms           Role = "customs"
)

// PortChangeStatus models the Suez→Arctic diversion lifecycle.
type PortChangeStatus string

const (
	PCRequested        PortChangeStatus = "requested"
	PCRouteOpsApproved PortChangeStatus = "route_ops_approved"
	PCApplied          PortChangeStatus = "applied"
	PCRejected         PortChangeStatus = "rejected"
)

// PortChange moves a booking from a congested (e.g. Suez) voyage onto the
// Arctic Express. Rule 4 requires route-ops secondary approval and submission
// no later than 48h before the loading cutoff.
type PortChange struct {
	ID           string
	BookingID    BookingID
	SourceVoyage VoyageID
	TargetVoyage VoyageID
	Reason       string
	Status       PortChangeStatus
	RequestedAt  time.Time
	ApprovedAt   time.Time
	AppliedAt    time.Time
}

// EditLock enforces rule 5: berth-lock / port-change / refund are mutually
// exclusive per resource at any instant.
type EditLock struct {
	Resource   string
	Holder     Role
	AcquiredAt time.Time
}

// Notification is the real-time message pushed to a role when it loses a
// contention (last berth or berth) or is promoted from the waitlist.
type Notification struct {
	ID        string
	Target    Role
	Subject   string
	Body      string
	CreatedAt time.Time
	Read      bool
}
