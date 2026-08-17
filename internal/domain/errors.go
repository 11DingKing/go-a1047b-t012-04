package domain

import (
	"errors"
	"time"
)

// Sentinel errors used across the service layer.
var (
	ErrNotFound         = errors.New("not found")
	ErrConflict         = errors.New("conflict")
	ErrUnauthorized     = errors.New("unauthorized")
	ErrInvalidState     = errors.New("invalid state transition")
	ErrLockHeld         = errors.New("edit lock held by another role")
	ErrCapacity         = errors.New("capacity exceeded")
	ErrDeadlineExceeded = errors.New("deadline exceeded")
	ErrFrozen           = errors.New("resource frozen")
)

// Clock abstracts time so background workers and services are deterministic in tests.
type Clock interface {
	Now() time.Time
}

// SystemClock reads the wall clock.
type SystemClock struct{}

func (SystemClock) Now() time.Time { return time.Now() }

// FakeClock is a controllable clock for tests.
type FakeClock struct {
	t time.Time
}

func NewFakeClock(t time.Time) *FakeClock    { return &FakeClock{t: t} }
func (f *FakeClock) Now() time.Time          { return f.t }
func (f *FakeClock) Set(t time.Time)         { f.t = t }
func (f *FakeClock) Advance(d time.Duration) { f.t = f.t.Add(d) }
