// Package clock abstracts time so tests can control it.
package clock

import "time"

// Clock returns the current time.
type Clock interface{ Now() time.Time }

// Stopper cancels a pending AfterFunc. Stop reports whether it prevented the
// call (false when it already ran or was stopped).
type Stopper interface{ Stop() bool }

// Ticker delivers ticks on C until stopped.
type Ticker interface {
	C() <-chan time.Time
	Stop()
}

// Timers is a Clock that can also schedule work, so timer-driven code (the
// availability hub, SSE heartbeats) runs on fake time in tests.
type Timers interface {
	Clock
	AfterFunc(d time.Duration, f func()) Stopper
	NewTicker(d time.Duration) Ticker
}

// System is the real clock, in UTC.
type System struct{}

var _ Timers = System{}

// Now returns the current UTC time.
func (System) Now() time.Time { return time.Now().UTC() }

// AfterFunc calls f in its own goroutine after d.
func (System) AfterFunc(d time.Duration, f func()) Stopper { return time.AfterFunc(d, f) }

// NewTicker returns a ticker that ticks every d.
func (System) NewTicker(d time.Duration) Ticker { return systemTicker{time.NewTicker(d)} }

type systemTicker struct{ t *time.Ticker }

func (s systemTicker) C() <-chan time.Time { return s.t.C }
func (s systemTicker) Stop()               { s.t.Stop() }
