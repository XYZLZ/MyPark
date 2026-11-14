package ratelimit

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/XYZLZ/MyPark/apps/server/internal/testsupport"
)

// memStart is 250 ms into a one-second window (and 10 s into a minute).
var memStart = time.Date(2026, 11, 20, 12, 0, 10, 250_000_000, time.UTC)

func TestMemory_FixedWindowWithFakeClock(t *testing.T) {
	clk := testsupport.NewFakeClock(memStart)
	m := NewMemory(clk)
	rule := Rule{Name: "t", Limit: 3, Window: time.Second, Prefix: "t:"}
	for i := range 3 {
		d, err := m.Allow(t.Context(), rule, "k")
		if err != nil || !d.Allowed {
			t.Fatalf("call %d = %+v, %v", i+1, d, err)
		}
	}
	d, err := m.Allow(t.Context(), rule, "k")
	if err != nil || d.Allowed {
		t.Fatalf("4th = %+v, %v, want denied", d, err)
	}
	if d.RetryAfter != 750*time.Millisecond {
		t.Errorf("RetryAfter = %v, want 750ms (window end - now)", d.RetryAfter)
	}
	if d, _ := m.Allow(t.Context(), rule, "other"); !d.Allowed {
		t.Error("another key shares the bucket")
	}
	clk.Advance(750 * time.Millisecond) // exactly the next window
	if d, _ := m.Allow(t.Context(), rule, "k"); !d.Allowed {
		t.Error("not allowed in the next window")
	}
}

func TestMemory_PrefixNamespacesBuckets(t *testing.T) {
	m := NewMemory(testsupport.NewFakeClock(memStart))
	a := Rule{Limit: 1, Window: time.Minute, Prefix: "a:"}
	b := Rule{Limit: 1, Window: time.Minute, Prefix: "b:"}
	_, _ = m.Allow(t.Context(), a, "k")
	if d, _ := m.Allow(t.Context(), b, "k"); !d.Allowed {
		t.Error("rules with different prefixes share a bucket")
	}
}

func TestMemory_ConcurrentAllow(t *testing.T) {
	m := NewMemory(testsupport.NewFakeClock(memStart))
	rule := Rule{Limit: 20, Window: time.Minute, Prefix: "c:"}
	var allowed atomic.Int64
	var wg sync.WaitGroup
	for range 50 {
		wg.Go(func() {
			if d, err := m.Allow(t.Context(), rule, "k"); err == nil && d.Allowed {
				allowed.Add(1)
			}
		})
	}
	wg.Wait()
	if got := allowed.Load(); got != 20 {
		t.Errorf("allowed = %d, want exactly 20", got)
	}
}

func TestMemory_SweepDropsExpired(t *testing.T) {
	clk := testsupport.NewFakeClock(memStart)
	m := NewMemory(clk)
	rule := Rule{Limit: 5, Window: time.Second, Prefix: "s:"}
	for i := range 100 {
		_, _ = m.Allow(t.Context(), rule, fmt.Sprintf("k%d", i))
	}
	if got := m.len(); got != 100 {
		t.Fatalf("buckets = %d, want 100", got)
	}
	clk.Advance(2 * time.Minute)
	_, _ = m.Allow(t.Context(), rule, "fresh")
	if got := m.len(); got != 1 {
		t.Errorf("buckets after sweep = %d, want 1", got)
	}
}

func TestMemory_SweepRunsAtMostOncePerMinute(t *testing.T) {
	clk := testsupport.NewFakeClock(memStart)
	m := NewMemory(clk)
	rule := Rule{Limit: 5, Window: time.Second, Prefix: "s:"}
	_, _ = m.Allow(t.Context(), rule, "a")
	clk.Advance(30 * time.Second) // "a" expired, but the last sweep was 30 s ago
	_, _ = m.Allow(t.Context(), rule, "b")
	if got := m.len(); got != 2 {
		t.Errorf("buckets = %d, want 2 (no sweep inside a minute)", got)
	}
}
