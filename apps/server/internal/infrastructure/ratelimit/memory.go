package ratelimit

import (
	"context"
	"sync"
	"time"

	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/clock"
)

// sweepInterval bounds how often expired buckets are dropped (clock time).
const sweepInterval = time.Minute

// Memory is a fixed-window Limiter held in process memory. Windows are aligned
// to the Unix epoch, like the Postgres adapter's date_bin.
//
// State is PER INSTANCE: with N API instances the effective limit is up to N
// times Rule.Limit, and a restart resets every count. Use it only for rules
// marked ScopeLocal (capacity protection on hot paths where a Postgres write
// per request is too expensive). It never errors.
type Memory struct {
	clock clock.Clock

	mu        sync.Mutex
	buckets   map[string]*bucket
	lastSweep time.Time
}

type bucket struct {
	windowEnd time.Time
	count     int
}

// NewMemory returns an in-memory Limiter driven by c.
func NewMemory(c clock.Clock) *Memory {
	return &Memory{clock: c, buckets: make(map[string]*bucket)}
}

// Allow counts one request for rule and key in the current window.
func (m *Memory) Allow(_ context.Context, rule Rule, key string) (Decision, error) {
	now := m.clock.Now()
	start := now.Truncate(rule.Window)
	end := start.Add(rule.Window)
	id := rule.Prefix + key

	m.mu.Lock()
	defer m.mu.Unlock()
	m.sweep(now)

	b, ok := m.buckets[id]
	if !ok || !b.windowEnd.After(now) {
		b = &bucket{windowEnd: end}
		m.buckets[id] = b
	}
	if b.count >= rule.Limit {
		return Decision{RetryAfter: end.Sub(now)}, nil
	}
	b.count++
	return Decision{Allowed: true}, nil
}

// sweep drops expired buckets, at most once per sweepInterval. Caller holds mu.
func (m *Memory) sweep(now time.Time) {
	if now.Sub(m.lastSweep) < sweepInterval {
		return
	}
	m.lastSweep = now
	for id, b := range m.buckets {
		if !b.windowEnd.After(now) {
			delete(m.buckets, id)
		}
	}
}

// len reports the bucket count (tests only).
func (m *Memory) len() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.buckets)
}
