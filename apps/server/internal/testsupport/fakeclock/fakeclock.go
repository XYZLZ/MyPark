// Package fakeclock is a manually advanced clock for tests. It is a leaf
// package (standard library only) so in-package tests of any feature can use
// it without the import cycle that testsupport would create.
package fakeclock

import (
	"sync"
	"time"
)

// Clock is a manually advanced clock, safe for concurrent use.
type Clock struct {
	mu  sync.Mutex
	now time.Time
}

// New returns a clock fixed at t.
func New(t time.Time) *Clock { return &Clock{now: t} }

// Now returns the current fake time.
func (c *Clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// Advance moves the clock forward by d.
func (c *Clock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// Set replaces the current time.
func (c *Clock) Set(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = t
}
