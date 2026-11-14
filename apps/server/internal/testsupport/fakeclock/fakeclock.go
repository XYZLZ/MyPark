// Package fakeclock is a manually advanced clock for tests. It is a leaf
// package (the standard library plus the clock interfaces) so in-package
// tests of any feature can use it without the import cycle that testsupport
// would create.
package fakeclock

import (
	"sync"
	"time"

	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/clock"
)

// tickBuffer is how many undelivered ticks a fake ticker keeps. Unlike
// time.Ticker, which keeps one, a test sees every tick of one Advance.
const tickBuffer = 64

// Clock is a manually advanced clock, safe for concurrent use. It implements
// clock.Timers: Advance fires every due timer and ticker in time order,
// synchronously, before returning.
type Clock struct {
	advance sync.Mutex // serializes whole Advance calls

	mu     sync.Mutex
	now    time.Time
	seq    uint64
	timers []*timer
}

var _ clock.Timers = (*Clock)(nil)

type timer struct {
	c      *Clock
	at     time.Time
	seq    uint64 // breaks ties: earlier-scheduled fires first
	period time.Duration
	f      func()
	ch     chan time.Time
}

// New returns a clock fixed at t.
func New(t time.Time) *Clock { return &Clock{now: t} }

// Now returns the current fake time.
func (c *Clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// Set replaces the current time. It fires no timer.
func (c *Clock) Set(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = t
}

// AfterFunc calls f once the clock reaches now+d.
func (c *Clock) AfterFunc(d time.Duration, f func()) clock.Stopper {
	return c.add(&timer{f: f}, d)
}

// NewTicker ticks every d of fake time.
func (c *Clock) NewTicker(d time.Duration) clock.Ticker {
	if d <= 0 {
		panic("fakeclock: non-positive ticker interval")
	}
	return ticker{c.add(&timer{period: d, ch: make(chan time.Time, tickBuffer)}, d)}
}

func (c *Clock) add(t *timer, d time.Duration) *timer {
	c.mu.Lock()
	defer c.mu.Unlock()
	t.c, t.at, t.seq = c, c.now.Add(d), c.seq
	c.seq++
	c.timers = append(c.timers, t)
	return t
}

// Advance moves the clock forward by d. Each due timer fires with Now() at
// its due time; timers scheduled by a callback fire too when due within d.
// Callbacks run without the clock's lock held.
func (c *Clock) Advance(d time.Duration) {
	c.advance.Lock()
	defer c.advance.Unlock()
	c.mu.Lock()
	target := c.now.Add(d)
	c.mu.Unlock()
	for {
		c.mu.Lock()
		t := c.nextDue(target)
		if t == nil {
			c.now = target
			c.mu.Unlock()
			return
		}
		c.now = t.at
		if t.period > 0 {
			select {
			case t.ch <- t.at:
			default: // the reader is behind: drop, as time.Ticker does
			}
			t.at = t.at.Add(t.period)
			c.mu.Unlock()
			continue
		}
		c.remove(t)
		c.mu.Unlock()
		t.f()
	}
}

// nextDue returns the earliest timer due at or before target. c.mu is held.
func (c *Clock) nextDue(target time.Time) *timer {
	var next *timer
	for _, t := range c.timers {
		if t.at.After(target) {
			continue
		}
		if next == nil || t.at.Before(next.at) || (t.at.Equal(next.at) && t.seq < next.seq) {
			next = t
		}
	}
	return next
}

// remove drops t and reports whether it was pending. c.mu is held.
func (c *Clock) remove(t *timer) bool {
	for i, x := range c.timers {
		if x == t {
			c.timers = append(c.timers[:i], c.timers[i+1:]...)
			return true
		}
	}
	return false
}

// Stop cancels the timer; it reports whether the timer was still pending.
func (t *timer) Stop() bool {
	t.c.mu.Lock()
	defer t.c.mu.Unlock()
	return t.c.remove(t)
}

// ticker adapts a periodic timer to clock.Ticker.
type ticker struct{ t *timer }

func (k ticker) C() <-chan time.Time { return k.t.ch }
func (k ticker) Stop()               { k.t.Stop() }
