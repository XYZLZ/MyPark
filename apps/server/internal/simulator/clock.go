// Package simulator is the discrete-event engine and the signing device client
// of the parking simulator (design §13.1). The engine runs on a virtual clock;
// the client reaches the API only through the signed device API.
package simulator

import (
	"context"
	"sync"
	"time"

	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/clock"
)

// Clock is the simulator's virtual time. It is distinct from the clock that
// signs requests (ClientConfig.SignClock).
type Clock interface {
	Now() time.Time
	// WaitUntil returns once virtual time has reached t, or ctx is done.
	WaitUntil(ctx context.Context, t time.Time) error
}

// SleepFunc waits d of wall time or until ctx is done.
type SleepFunc func(ctx context.Context, d time.Duration) error

// sleepCtx is the real SleepFunc.
func sleepCtx(ctx context.Context, d time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if d <= 0 {
		return nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// RealClock maps wall time to virtual time at a speed factor: virtual time
// is start + speed × (wall elapsed since construction).
type RealClock struct {
	start     time.Time
	wallStart time.Time
	speed     int
	wall      clock.Clock
	sleep     SleepFunc
}

// RealClockOption configures a RealClock.
type RealClockOption func(*RealClock)

// WithSleep replaces the wall-time sleep, so tests never wait.
func WithSleep(s SleepFunc) RealClockOption { return func(c *RealClock) { c.sleep = s } }

// NewRealClock returns a clock whose virtual time starts at start and runs
// speed times faster than wall. A speed below 1 is treated as 1.
func NewRealClock(start time.Time, speed int, wall clock.Clock, opts ...RealClockOption) *RealClock {
	c := &RealClock{start: start, wallStart: wall.Now(), speed: max(speed, 1), wall: wall, sleep: sleepCtx}
	for _, o := range opts {
		o(c)
	}
	return c
}

// Now returns the current virtual time.
func (c *RealClock) Now() time.Time {
	return c.start.Add(time.Duration(c.speed) * c.wall.Now().Sub(c.wallStart))
}

// WaitUntil sleeps (t − now)/speed of wall time. A past t returns at once.
func (c *RealClock) WaitUntil(ctx context.Context, t time.Time) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	d := t.Sub(c.Now()) / time.Duration(c.speed)
	if d <= 0 {
		return nil
	}
	return c.sleep(ctx, d)
}

// StepClock is virtual time that only moves when waited on: WaitUntil jumps
// straight to t. It drives the CI scenarios.
type StepClock struct {
	mu  sync.Mutex
	now time.Time
}

// NewStepClock returns a StepClock at start.
func NewStepClock(start time.Time) *StepClock { return &StepClock{now: start} }

// Now returns the current virtual time.
func (c *StepClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// WaitUntil jumps to t without waiting. Time never moves backwards.
func (c *StepClock) WaitUntil(ctx context.Context, t time.Time) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if t.After(c.now) {
		c.now = t
	}
	return nil
}
