package main

import (
	"encoding/json"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/XYZLZ/MyPark/apps/server/internal/devicefleet"
	"github.com/XYZLZ/MyPark/apps/server/internal/domain/authz"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/availability"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/gate"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/clock"
	"github.com/XYZLZ/MyPark/apps/server/internal/simulator"
)

// m2Criteria maps every spec §8 M2 exit criterion to the subtest that asserts
// it and to the test that holds the evidence it cites. A failure prints it.
var m2Criteria = [][2]string{
	{"The weekday scenario runs and availability updates in < 2 s (backend: SSE)",
		"subtest 'weekday availability'; latency figure: TestAvailability_PassToSSELatency"},
	{"fill_to_full reaches 0 and the dispenser denies with gate.lot_full",
		"subtest 'fill_to_full'; also TestScenario_FillToFull"},
	{"The tariff calculator passes a table-driven suite (grace, cap, multi-day, lost ticket)",
		"subtest 'tariff table'; the pure suite is in internal/features/tariffs"},
	{"A replayed device event (same id) is a no-op. A bad signature returns 401",
		"subtest 'replay and bad signature'; also TestDeviceEvents_ReplayReturnsStoredResponse"},
}

// Spec §8 M2 exit criteria, on the backend. Each subtest is one criterion; the
// web and mobile availability screens and the Simulator console are Track B.
func TestM2BackendAcceptance(t *testing.T) {
	t.Cleanup(func() {
		if !t.Failed() {
			return
		}
		for _, c := range m2Criteria {
			t.Logf("M2 exit criterion %q is proven by %s", c[0], c[1])
		}
	})

	t.Run("weekday availability", func(t *testing.T) {
		// Real timers for the hub, so its coalescing window runs on wall time;
		// the business clock stays the shared fake one.
		r := newScenarioRig(t, weekdayStart, withTimers(clock.System{}), func(d *deps) {
			d.avail = newAvailabilityService(d.pool, d.clock)
			d.hub = availability.NewHub(d.avail, d.timers, d.log)
		})
		se := &streamEnv{deviceEnv: r.deviceEnv, publicID: r.publicID.String(), hub: r.d.hub}
		se.serve()
		first, frames := se.openOK(se.streamURL(""), nil)

		var (
			mu     sync.Mutex
			latest = first
			moved  = make(chan struct{}, 1)
		)
		go func() {
			for f := range frames {
				var snap availability.Snapshot
				if f.Event != "availability" || json.Unmarshal([]byte(f.Data), &snap) != nil {
					continue
				}
				mu.Lock()
				if snap.Version > latest.Version {
					latest = snap
				}
				mu.Unlock()
				select {
				case moved <- struct{}{}:
				default:
				}
			}
		}()

		r.start(simulator.ScenarioWeekday, 42)
		r.pace.runTo(t, weekdayStart.Add(time.Hour))

		// The engine is parked between steps, so the last entry or exit pass
		// has committed. The client must hold that version.
		occ, ver := r.occupancy()
		if ver <= first.Version {
			t.Fatalf("lot_occupancy.version = %d after a virtual hour, still the first snapshot's %d: no pass committed", ver, first.Version)
		}
		deadline := time.After(2 * time.Second)
		for {
			mu.Lock()
			got := latest
			mu.Unlock()
			if got.Version == ver {
				if int64(got.Occupied) != occ {
					t.Fatalf("snapshot %+v at version %d, want occupied %d", got, ver, occ)
				}
				break
			}
			select {
			case <-moved:
			case <-deadline:
				t.Fatalf("the client holds version %d, want lot_occupancy.version %d within 2 s", got.Version, ver)
			}
		}
		r.invariant()
	})

	t.Run("fill_to_full", func(t *testing.T) {
		r := newScenarioRig(t, time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC))
		r.pace.release() // no limit: the scenario ends itself
		r.start(simulator.ScenarioFillToFull, 42)
		r.waitFor("the lot to fill", func() bool {
			return r.runner.Status().ReachedFull && r.tap.n("sensor.space_changed ack") == 280
		})
		if got := r.availability(); got["available"] != 0.0 {
			t.Errorf("availability = %v, want 0 available", got)
		}
		rep, err := r.client.Send(t.Context(), devicefleet.ByKind(authz.DeviceDispenser), simulator.Outgoing{
			EventID: uuid.New(), Type: "entry.credential_presented", Payload: map[string]any{"kind": "ticket_request"},
		})
		if err != nil || rep.Result != "deny" || rep.Reason != "gate.lot_full" {
			t.Errorf("next ticket_request = %+v, %v; want deny gate.lot_full", rep, err)
		}
		r.invariant()
	})

	// The amounts are the spec §5.1 values through the device API on the fake
	// clock: a stay entered at t0 is scanned (or marked lost) at t0 + at.
	t.Run("tariff table", func(t *testing.T) {
		for _, c := range []struct {
			name string
			at   time.Duration
			lost bool
			want int64
		}{
			{"grace", 10 * time.Minute, false, 0},
			{"cap", 15*time.Hour + time.Second, false, 40000},
			{"multi-day", 75 * time.Hour, false, 130000},
			{"lost ticket", 26 * time.Hour, true, 57500},
		} {
			t.Run(c.name, func(t *testing.T) {
				e := newPayEnv(t)
				e.at(c.at)
				var got payData
				if c.lost {
					got = e.lost(e.stayID)
				} else {
					got = e.scan(gate.KindTicket, e.ticketCode)
				}
				if got.Result != "ack" || got.amount() != c.want || (c.lost && got.kind() != "lost_ticket") {
					t.Fatalf("at %v: %+v, want ack with %d due", c.at, got, c.want)
				}
				e.invariant()
			})
		}
	})

	t.Run("replay and bad signature", func(t *testing.T) {
		e := newPayEnv(t)
		e.at(90 * time.Minute)
		if got := e.scan(gate.KindTicket, e.ticketCode); got.amount() != 7500 {
			t.Fatalf("scan = %+v, want 7500 due", got)
		}

		// The same cash_paid event id twice: the second is the stored answer.
		body := e.cashBody(uuid.New(), 7500)
		first := e.send(e.ps, body)
		occ, ver := e.occupancy()
		second := e.send(e.ps, body)
		a, b := payOf(t, first), payOf(t, second)
		if a.Result != "ack" || a.ReceiptNo == "" || b.ReceiptNo != a.ReceiptNo {
			t.Fatalf("first %+v, replay %+v: want ack and the same receipt_no", a, b)
		}
		if first.Header().Get("Idempotent-Replayed") != "" || second.Header().Get("Idempotent-Replayed") != "true" {
			t.Errorf("Idempotent-Replayed = %q then %q, want empty then true",
				first.Header().Get("Idempotent-Replayed"), second.Header().Get("Idempotent-Replayed"))
		}
		if n := e.payments(); n != 1 {
			t.Errorf("payments = %d, want 1", n)
		}
		if occ2, ver2 := e.occupancy(); occ2 != occ || ver2 != ver {
			t.Errorf("occupancy %d v%d after the replay, was %d v%d", occ2, ver2, occ, ver)
		}
		e.invariant()

		// A flipped signature is 401 device.unauthenticated and stores nothing.
		req := e.ps.Request(t, deviceEventsPath, e.eventBody(uuid.New(), "heartbeat", struct{}{}), e.clk.Now())
		sig := []byte(req.Header.Get("X-Signature"))
		sig[0] = flipHex(sig[0])
		req.Header.Set("X-Signature", string(sig))
		rows := e.eventRows()
		rec := e.serve(req, "")
		wantError(t, rec, http.StatusUnauthorized, "device.unauthenticated")
		if e.eventRows() != rows {
			t.Error("a request with a bad signature was stored")
		}
	})
}

// flipHex returns another hex digit.
func flipHex(c byte) byte {
	if c == '0' {
		return '1'
	}
	return '0'
}
