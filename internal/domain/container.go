package domain

import "time"

// ContainerStatus tracks the cold-chain lifecycle of a temperature-controlled unit.
type ContainerStatus string

const (
	ContainerBound    ContainerStatus = "bound"
	ContainerNormal   ContainerStatus = "normal"
	ContainerAlert    ContainerStatus = "alert"
	ContainerTransfer ContainerStatus = "transfer"
	ContainerFrozen   ContainerStatus = "frozen"
)

// TempReading is a single temperature sample taken by the warehouse.
type TempReading struct {
	At    time.Time
	Value float64
}

// TempContainer is bound to a booking and records its temperature curve. Rule 3
// fires a standby transfer once readings stay out of bounds for 10 minutes.
type TempContainer struct {
	ID               ContainerID
	BookingID        BookingID
	SetpointLow      float64
	SetpointHigh     float64
	Readings         []TempReading
	Status           ContainerStatus
	OutOfBoundsSince time.Time
	Frozen           bool
	TransferOrderID  string
}

// AddReading appends a sample and tracks the continuous out-of-bounds interval.
// It returns true when the reading is currently outside the setpoint band.
func (c *TempContainer) AddReading(r TempReading) bool {
	c.Readings = append(c.Readings, r)
	out := r.Value < c.SetpointLow || r.Value > c.SetpointHigh
	if out {
		if c.OutOfBoundsSince.IsZero() {
			c.OutOfBoundsSince = r.At
		}
		c.Status = ContainerAlert
	} else {
		c.OutOfBoundsSince = time.Time{}
		if c.Status == ContainerAlert {
			c.Status = ContainerNormal
		}
	}
	return out
}

// OutOfBoundsDuration is how long the container has been continuously out of
// bounds, measured against now.
func (c *TempContainer) OutOfBoundsDuration(now time.Time) time.Duration {
	if c.OutOfBoundsSince.IsZero() {
		return 0
	}
	return now.Sub(c.OutOfBoundsSince)
}

// Freeze marks the original container as frozen so it cannot be loaded (rule 3).
func (c *TempContainer) Freeze() {
	c.Frozen = true
	c.Status = ContainerFrozen
}

// TransferOrder is the standby-container reallocation created on rule 3 breach.
type TransferOrder struct {
	ID          string
	ContainerID ContainerID
	BookingID   BookingID
	Reason      string
	IssuedAt    time.Time
}
