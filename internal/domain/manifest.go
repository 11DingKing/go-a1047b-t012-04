package domain

import "time"

// ManifestStatus models the versioned cargo manifest lifecycle. A conflict
// (rule: parallel port-change vs customs declaration) freezes the manifest and
// requires dual confirmation before loading resumes.
type ManifestStatus string

const (
	ManifestDraft       ManifestStatus = "draft"
	ManifestConfirmed   ManifestStatus = "confirmed"
	ManifestFrozen      ManifestStatus = "frozen"
	ManifestReconfirmed ManifestStatus = "reconfirmed"
)

// ManifestEntry is one confirmed booking line on the manifest.
type ManifestEntry struct {
	BookingID     BookingID
	ForwarderID   ForwarderID
	CargoCategory CargoCategory
	TEU           int
}

// Manifest is versioned per voyage. LastConfirmedSnapshot holds the most recent
// confirmed state so a conflict can roll back to it.
type Manifest struct {
	VoyageID              VoyageID
	Version               int
	Status                ManifestStatus
	Entries               []ManifestEntry
	Cutoff                time.Time
	RouteOpsConfirmed     bool
	CustomsConfirmed      bool
	LastConfirmedSnapshot *Manifest
}

func NewManifest(voyageID VoyageID) *Manifest {
	return &Manifest{VoyageID: voyageID, Version: 1, Status: ManifestDraft}
}

// Snapshot returns a deep copy of the current manifest for later rollback.
func (m *Manifest) Snapshot() *Manifest {
	entries := append([]ManifestEntry(nil), m.Entries...)
	return &Manifest{
		VoyageID: m.VoyageID,
		Version:  m.Version,
		Status:   m.Status,
		Entries:  entries,
		Cutoff:   m.Cutoff,
	}
}

func (m *Manifest) BumpVersion() { m.Version++ }

// MarkConfirmed records the confirmed state and snapshots it for rollback.
func (m *Manifest) MarkConfirmed() {
	m.Status = ManifestConfirmed
	m.LastConfirmedSnapshot = m.Snapshot()
}

// RestoreConfirmed rolls the manifest back to its last confirmed snapshot and
// enters the frozen, dual-confirmation-pending state.
func (m *Manifest) RestoreConfirmed() {
	if m.LastConfirmedSnapshot != nil {
		s := m.LastConfirmedSnapshot
		m.Version = s.Version
		m.Entries = append([]ManifestEntry(nil), s.Entries...)
		m.Cutoff = s.Cutoff
	}
	m.Status = ManifestFrozen
	m.RouteOpsConfirmed = false
	m.CustomsConfirmed = false
}

// Confirm marks confirmation from one of the two roles. When both have confirmed
// the frozen manifest is reconfirmed.
func (m *Manifest) Confirm(role Role) bool {
	switch role {
	case RoleRouteOps:
		m.RouteOpsConfirmed = true
	case RoleCustoms:
		m.CustomsConfirmed = true
	}
	if m.Status == ManifestFrozen && m.RouteOpsConfirmed && m.CustomsConfirmed {
		m.Status = ManifestReconfirmed
		return true
	}
	return false
}
