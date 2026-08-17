package domain

import "time"

// Identifier types keep the domain self-documenting and prevent accidental
// cross-assignment of unrelated keys.
type (
	ForwarderID string
	VoyageID    string
	VesselID    string
	PortID      string
	BerthID     string
	BookingID   string
	ContainerID string
)

// CargoCategory enumerates the high-value goods the Arctic Express carries.
type CargoCategory string

const (
	CargoEnergyStorage CargoCategory = "energy_storage"
	CargoPowerBattery  CargoCategory = "power_battery"
	CargoPVComponent   CargoCategory = "pv_component"
)

// Qualification is a category-specific freight forwarder qualification.
type Qualification string

const (
	QualPowerBattery Qualification = "power_battery"
)

// Forwarder is the booking specialist's organisation. Rule 1 gates
// temperature-controlled berths behind the power-battery qualification.
type Forwarder struct {
	ID             ForwarderID
	Name           string
	Qualifications map[Qualification]bool
}

func NewForwarder(id ForwarderID, name string) *Forwarder {
	return &Forwarder{ID: id, Name: name, Qualifications: map[Qualification]bool{}}
}

func (f *Forwarder) Grant(q Qualification)  { f.Qualifications[q] = true }
func (f *Forwarder) Revoke(q Qualification) { delete(f.Qualifications, q) }

func (f Forwarder) HasQualification(q Qualification) bool { return f.Qualifications[q] }
func (f Forwarder) HasBatteryQualification() bool         { return f.Qualifications[QualPowerBattery] }

// Port is a physical call port on the Arctic rotation.
type Port struct {
	ID   PortID
	Code string
	Name string
}

// Berth is a quay slot at a port that a vessel may occupy. Allocation requests
// carry a ClaimRequestedAt timestamp so that concurrent claims for the same
// berth are resolved in favour of the earliest submission (business rule).
type Berth struct {
	ID               BerthID
	PortID           PortID
	AllocatedVoyage  VoyageID
	AllocatedShip    string
	AllocatedAt      time.Time
	ClaimRequestedAt time.Time
}

func (b Berth) IsAllocated() bool { return b.AllocatedVoyage != "" }

// PortCall captures the schedule for one stop on a voyage. GateOpen is the
// 开港 instant (cargo acceptance start); Cutoff is the 截关 deadline.
type PortCall struct {
	PortID   PortID
	Sequence int
	ETA      time.Time
	ETD      time.Time
	GateOpen time.Time
	Cutoff   time.Time
}

// Vessel is the ship carrying the Arctic voyage.
type Vessel struct {
	ID              VesselID
	Name            string
	TempCapacityTEU int
}

type VoyageStatus string

const (
	VoyageScheduled VoyageStatus = "scheduled"
	VoyageLoading   VoyageStatus = "loading"
	VoyageClosed    VoyageStatus = "closed"
	VoyageFrozen    VoyageStatus = "frozen"
)

// Voyage owns the ordered rotation, the temperature-controlled capacity and the
// frozen flag (set when a manifest version conflict suspends loading).
type Voyage struct {
	ID             VoyageID
	VesselID       VesselID
	VoyageNumber   string
	PortCalls      []PortCall
	TempCapacity   int
	Status         VoyageStatus
	ManifestFrozen bool
}

// OversellLimit applies the 3% overbooking cap (rule 2) on top of the base
// temperature-controlled capacity.
func (v Voyage) OversellLimit() int {
	base := v.TempCapacity
	extra := (base*3 + 99) / 100 // ceil(base * 3%)
	return base + extra
}

// CallsSorted returns port calls ordered by Sequence, matching the maintained
// Felixstowe → Rotterdam → Hamburg → Gdynia rotation.
func (v Voyage) CallsSorted() []PortCall {
	cp := append([]PortCall(nil), v.PortCalls...)
	for i := 1; i < len(cp); i++ {
		for j := i; j > 0 && cp[j-1].Sequence > cp[j].Sequence; j-- {
			cp[j-1], cp[j] = cp[j], cp[j-1]
		}
	}
	return cp
}

func (v Voyage) FirstCall() (PortCall, bool) {
	s := v.CallsSorted()
	if len(s) == 0 {
		return PortCall{}, false
	}
	return s[0], true
}

func (v Voyage) CallByPort(portID PortID) (PortCall, bool) {
	for _, c := range v.PortCalls {
		if c.PortID == portID {
			return c, true
		}
	}
	return PortCall{}, false
}

// GateOpen is the loading port's 开港 instant used by the 72h deposit rule.
func (v Voyage) GateOpen() (time.Time, bool) {
	c, ok := v.FirstCall()
	if !ok {
		return time.Time{}, false
	}
	return c.GateOpen, true
}

// LoadingCutoff is the loading port's 截关 deadline used by the 48h port-change rule.
func (v Voyage) LoadingCutoff() (time.Time, bool) {
	c, ok := v.FirstCall()
	if !ok {
		return time.Time{}, false
	}
	return c.Cutoff, true
}

func (v *Voyage) FreezeLoading() { v.ManifestFrozen = true; v.Status = VoyageFrozen }
func (v *Voyage) UnfreezeLoading() {
	v.ManifestFrozen = false
	if v.Status == VoyageFrozen {
		v.Status = VoyageScheduled
	}
}
