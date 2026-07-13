package fsdclient

import "time"

// Clock abstracts time for deterministic tests.
// A nil Clock on Config is treated as RealClock.
type Clock interface {
	Now() time.Time
}

// RealClock uses time.Now.
type RealClock struct{}

// Now returns the current wall time.
func (RealClock) Now() time.Time { return time.Now() }

// ManualClock is a mutable clock for tests.
type ManualClock struct {
	t time.Time
}

// NewManualClock returns a ManualClock fixed at t.
func NewManualClock(t time.Time) *ManualClock {
	return &ManualClock{t: t}
}

// Now returns the fixed time.
func (c *ManualClock) Now() time.Time { return c.t }

// Advance moves the clock forward by d.
func (c *ManualClock) Advance(d time.Duration) { c.t = c.t.Add(d) }

// Set sets the clock to t.
func (c *ManualClock) Set(t time.Time) { c.t = t }

func resolveClock(c Clock) Clock {
	if c == nil {
		return RealClock{}
	}
	return c
}
