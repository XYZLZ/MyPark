package availability

import "time"

// CoalesceWindow is the minimum time between two availability pushes of one
// lot: at most four pushes per second (M2C-39).
const CoalesceWindow = 250 * time.Millisecond

// Coalescer rate-limits the pushes of one lot with a leading and a trailing
// edge: a change pushes at once when the last push is at least CoalesceWindow
// old; otherwise one trailing push runs at last push + CoalesceWindow and
// covers every change in between. The zero value is ready to use. It is not
// safe for concurrent use; the hub guards it.
type Coalescer struct {
	last    time.Time
	pushed  bool // last is set
	pending bool // a trailing push is scheduled
}

// Bump records a change at now. pushNow means push immediately. A non-zero
// trailingAt asks the caller to call Fire at that time; zero with pushNow
// false means a trailing push is already scheduled.
func (c *Coalescer) Bump(now time.Time) (pushNow bool, trailingAt time.Time) {
	if c.pending {
		return false, time.Time{}
	}
	if !c.pushed || now.Sub(c.last) >= CoalesceWindow {
		c.last, c.pushed = now, true
		return true, time.Time{}
	}
	c.pending = true
	return false, c.last.Add(CoalesceWindow)
}

// Fire runs the scheduled trailing edge at now. It reports whether to push
// (false when nothing was scheduled).
func (c *Coalescer) Fire(now time.Time) bool {
	if !c.pending {
		return false
	}
	c.pending = false
	c.last = now
	return true
}
