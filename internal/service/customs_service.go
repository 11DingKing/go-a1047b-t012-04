package service

import (
	"fmt"
	"time"

	"arcticexpress/internal/domain"
	"arcticexpress/internal/store"
)

// CustomsService is the customs-broker workflow: preparing the versioned cargo
// manifest, declaring customs (locking the cutoff), and recovering from a
// parallel port-change / declaration conflict by rolling back, freezing, and
// requiring dual confirmation before recomputing the cutoff.
type CustomsService struct {
	base
}

func NewCustomsService(s *store.Store, c domain.Clock) *CustomsService {
	return &CustomsService{base: base{Store: s, Clock: c}}
}

// PrepareManifest ensures a draft manifest exists and refreshes its entries
// from the voyage's confirmed bookings. It returns the manifest (with its
// current version) so the broker can declare against a stable baseline.
func (cs *CustomsService) PrepareManifest(voyageID domain.VoyageID) (*domain.Manifest, error) {
	cs.Store.Lock()
	defer cs.Store.Unlock()
	v, ok := cs.Store.Voyages[voyageID]
	if !ok {
		return nil, fmt.Errorf("%w: voyage %s", domain.ErrNotFound, voyageID)
	}
	if v.ManifestFrozen {
		return nil, fmt.Errorf("%w: voyage frozen, resolve manifest conflict first", domain.ErrFrozen)
	}
	m, ok := cs.Store.Manifests[voyageID]
	if !ok {
		m = domain.NewManifest(voyageID)
		cs.Store.Manifests[voyageID] = m
	}
	if m.Status == domain.ManifestDraft {
		rebuildManifest(cs.Store, voyageID)
	}
	return m, nil
}

// DeclareCustoms declares customs against the expected manifest version and
// locks the cutoff. If the version changed since the broker last read it (a
// parallel port-change bumped it), the manifest is rolled back to the last
// confirmed snapshot, loading is frozen, and the broker must resolve via
// ConfirmManifest from both route ops and customs.
func (cs *CustomsService) DeclareCustoms(voyageID domain.VoyageID, expectedVersion int) (*domain.Manifest, error) {
	cs.Store.Lock()
	defer cs.Store.Unlock()

	v, ok := cs.Store.Voyages[voyageID]
	if !ok {
		return nil, fmt.Errorf("%w: voyage %s", domain.ErrNotFound, voyageID)
	}
	if v.ManifestFrozen {
		return nil, fmt.Errorf("%w: voyage frozen, resolve manifest conflict first", domain.ErrFrozen)
	}
	m, ok := cs.Store.Manifests[voyageID]
	if !ok {
		return nil, fmt.Errorf("%w: manifest for %s; prepare first", domain.ErrNotFound, voyageID)
	}
	if m.Status == domain.ManifestFrozen {
		return nil, fmt.Errorf("%w: manifest frozen pending dual confirmation", domain.ErrFrozen)
	}

	if m.Version != expectedVersion {
		actual := m.Version
		m.RestoreConfirmed()
		v.FreezeLoading()
		cs.notify(domain.RoleRouteOps, "manifest_conflict",
			fmt.Sprintf("manifest %s rolled back to v%d and frozen; version moved %d→%d during declaration", voyageID, m.Version, expectedVersion, actual))
		cs.notify(domain.RoleCustoms, "manifest_conflict",
			fmt.Sprintf("customs declaration for %s conflicted (expected v%d, was v%d); manifest frozen", voyageID, expectedVersion, actual))
		return m, fmt.Errorf("%w: manifest version moved from %d to %d during declaration; rolled back and frozen", domain.ErrConflict, expectedVersion, actual)
	}

	cutoff, ok := v.LoadingCutoff()
	if !ok {
		return nil, fmt.Errorf("%w: voyage has no cutoff", domain.ErrInvalidState)
	}
	m.Cutoff = cutoff
	m.MarkConfirmed()
	m.CustomsConfirmed = true
	return m, nil
}

// ConfirmManifest records one role's confirmation of a frozen manifest. When
// both route ops and customs have confirmed, loading is unfrozen and the cutoff
// is recomputed (failure recovery completion).
func (cs *CustomsService) ConfirmManifest(voyageID domain.VoyageID, role domain.Role) (*domain.Manifest, error) {
	cs.Store.Lock()
	defer cs.Store.Unlock()

	v, ok := cs.Store.Voyages[voyageID]
	if !ok {
		return nil, fmt.Errorf("%w: voyage %s", domain.ErrNotFound, voyageID)
	}
	m, ok := cs.Store.Manifests[voyageID]
	if !ok {
		return nil, fmt.Errorf("%w: manifest for %s", domain.ErrNotFound, voyageID)
	}
	if m.Status != domain.ManifestFrozen && m.Status != domain.ManifestReconfirmed {
		return nil, fmt.Errorf("%w: manifest not in frozen/reconfirmed state", domain.ErrInvalidState)
	}
	unfroze := m.Confirm(role)
	if unfroze {
		v.UnfreezeLoading()
		cs.recomputeCutoff(v, m)
		cs.notify(domain.RoleCustoms, "manifest_unfrozen",
			fmt.Sprintf("manifest %s reconfirmed; cutoff recomputed to %s", voyageID, m.Cutoff.Format(time.RFC3339)))
	}
	return m, nil
}

// recomputeCutoff sets a fresh cutoff after conflict resolution: 48h from now,
// capped by the loading port's ETD so it never exceeds departure.
func (cs *CustomsService) recomputeCutoff(v *domain.Voyage, m *domain.Manifest) {
	nc := cs.now().Add(48 * time.Hour)
	if c, ok := v.FirstCall(); ok && !c.ETD.IsZero() && nc.After(c.ETD) {
		nc = c.ETD
	}
	m.Cutoff = nc
	m.LastConfirmedSnapshot = m.Snapshot()
}

// Manifest fetches a voyage's manifest.
func (cs *CustomsService) Manifest(voyageID domain.VoyageID) (*domain.Manifest, error) {
	cs.Store.Lock()
	defer cs.Store.Unlock()
	m, ok := cs.Store.Manifests[voyageID]
	if !ok {
		return nil, fmt.Errorf("%w: manifest for %s", domain.ErrNotFound, voyageID)
	}
	return m, nil
}
