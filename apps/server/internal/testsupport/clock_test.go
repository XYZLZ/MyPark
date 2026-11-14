package testsupport_test

import (
	"testing"
	"time"

	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/clock"
	"github.com/XYZLZ/MyPark/apps/server/internal/testsupport"
)

var _ clock.Timers = (*testsupport.FakeClock)(nil)

func TestFakeClock_TimersFireOnAdvance(t *testing.T) {
	start := time.Date(2026, 11, 24, 14, 0, 0, 0, time.UTC)
	c := testsupport.NewFakeClock(start)

	var fired []time.Time
	c.AfterFunc(250*time.Millisecond, func() { fired = append(fired, c.Now()) })
	c.Advance(249 * time.Millisecond)
	if len(fired) != 0 {
		t.Fatalf("fired at +249ms: %v", fired)
	}
	c.Advance(time.Millisecond)
	if len(fired) != 1 || !fired[0].Equal(start.Add(250*time.Millisecond)) {
		t.Fatalf("fired = %v, want once at +250ms", fired)
	}
	c.Advance(time.Hour)
	if len(fired) != 1 {
		t.Fatalf("fired %d times, want 1", len(fired))
	}

	// Callbacks see Now() at their due time, and a timer scheduled by a
	// callback inside the same Advance still fires.
	var chained []time.Time
	base := c.Now()
	c.AfterFunc(time.Second, func() {
		chained = append(chained, c.Now())
		c.AfterFunc(time.Second, func() { chained = append(chained, c.Now()) })
	})
	c.Advance(10 * time.Second)
	if len(chained) != 2 || !chained[0].Equal(base.Add(time.Second)) || !chained[1].Equal(base.Add(2*time.Second)) {
		t.Fatalf("chained = %v", chained)
	}
	if !c.Now().Equal(base.Add(10 * time.Second)) {
		t.Fatalf("Now = %v after Advance", c.Now())
	}

	tk := c.NewTicker(15 * time.Second)
	c.Advance(30 * time.Second)
	if got := len(tk.C()); got != 2 {
		t.Fatalf("ticker fired %d times on Advance(30s), want 2", got)
	}
	tk.Stop()
	c.Advance(time.Minute)
	if got := len(tk.C()); got != 2 {
		t.Fatalf("stopped ticker fired: %d ticks", got)
	}

	s := c.AfterFunc(time.Second, func() { t.Error("stopped timer fired") })
	if !s.Stop() {
		t.Error("Stop on a pending timer = false")
	}
	if s.Stop() {
		t.Error("second Stop = true")
	}
	c.Advance(time.Minute)
}
