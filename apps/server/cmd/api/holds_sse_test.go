package main

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/XYZLZ/MyPark/apps/server/internal/features/availability"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/reservations"
	"github.com/XYZLZ/MyPark/apps/server/internal/testsupport"
)

// holdsSSEEnv is the production handler with the simulated gateway and a hub
// fed by a real LISTEN connection, with the stream open on the pilot lot.
type holdsSSEEnv struct {
	*feeEnv
	frames <-chan testsupport.SSEFrame
	snap   availability.Snapshot
}

func newHoldsSSEEnv(t *testing.T) *holdsSSEEnv {
	t.Helper()
	fe := newFeeEnv(t, func(d *deps) {
		d.avail = newAvailabilityService(d.pool, d.clock)
		d.hub = availability.NewHub(d.avail, d.timers, d.log)
	})
	se := &streamEnv{deviceEnv: &deviceEnv{apiEnv: fe.apiEnv, lotID: fe.lotID}, publicID: fe.publicID, hub: fe.d.hub}
	srv := httptest.NewServer(se.h)
	t.Cleanup(srv.Close)
	se.base = srv.URL
	se.listen()
	snap, frames := se.openOK(se.streamURL(""), nil)
	return &holdsSSEEnv{feeEnv: fe, frames: frames, snap: snap}
}

// next waits for the next availability event and requires it to be newer.
func (e *holdsSSEEnv) next() availability.Snapshot {
	e.t.Helper()
	got := snapshotOf(e.t, testsupport.NextSSE(e.t, e.frames, 2*time.Second))
	if got.Version <= e.snap.Version {
		e.t.Fatalf("version %d after %d, want newer", got.Version, e.snap.Version)
	}
	e.snap = got
	return got
}

func TestHolds_ConfirmToSSE(t *testing.T) {
	e := newHoldsSSEEnv(t)
	d := testsupport.SeedDrivers(t, e.db, 1)[0]
	// Normalized to 14:05: the window 13:50 to 14:25 contains the clock.
	id := e.reserve(d, "2026-12-01T14:02:00Z")
	if code, body, _ := e.pay(e.token(d), id, "key-sse-confirm", goodCard()); code != http.StatusOK {
		t.Fatalf("pay = %d %v", code, body)
	}
	if got := e.next(); got.EffectiveHolds != 1 {
		t.Errorf("effective_holds = %d, want 1", got.EffectiveHolds)
	}
}

// TestHolds_BoundaryToSSE checks a window opening with no transaction reaches
// the stream through the boundary job (M3B-39).
func TestHolds_BoundaryToSSE(t *testing.T) {
	e := newHoldsSSEEnv(t)
	d := testsupport.SeedDrivers(t, e.db, 1)[0]
	arrival := rsvAt(2 * time.Hour)
	(&rsvEnv{lotEnv: e.lotEnv}).seedRes(d, "confirmed", arrival) // window [arrival-15m, arrival+20m)
	b := reservations.NewHoldBoundary(e.db.App, slog.New(slog.NewTextHandler(io.Discard, nil)))
	tick := func(now time.Time, want int) {
		t.Helper()
		if n, err := b.Tick(t.Context(), now); err != nil || n != want {
			t.Fatalf("Tick = %d, %v; want %d", n, err, want)
		}
	}
	tick(rsvNow, 1) // baseline
	if got := e.next(); got.EffectiveHolds != 0 {
		t.Fatalf("effective_holds = %d before the window, want 0", got.EffectiveHolds)
	}
	e.clk.Set(arrival.Add(-15 * time.Minute))
	tick(e.clk.Now(), 1)
	if got := e.next(); got.EffectiveHolds != 1 {
		t.Errorf("effective_holds = %d once the window opened, want 1", got.EffectiveHolds)
	}
	e.clk.Set(arrival.Add(20 * time.Minute))
	tick(e.clk.Now(), 1)
	if got := e.next(); got.EffectiveHolds != 0 {
		t.Errorf("effective_holds = %d once the window closed, want 0", got.EffectiveHolds)
	}
}
