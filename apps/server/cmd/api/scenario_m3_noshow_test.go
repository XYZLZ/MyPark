package main

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/XYZLZ/MyPark/apps/server/internal/simulator"
)

// TestScenario_NoShow: reservations whose cars never come expire at the end of
// their window through the no-show job, their slots are released, and the
// effective holds follow the windows on both sides of the boundary tick.
func TestScenario_NoShow(t *testing.T) {
	r := newScenarioRig(t, weekdayStart)
	r.withDrivers()
	r.start(simulator.ScenarioNoShow, 42)

	// Run until the first reservation exists, then watch its window close.
	var first struct {
		id         int64
		start, end time.Time
	}
	for at := weekdayStart; first.id == 0; {
		if at = at.Add(5 * time.Minute); at.After(weekdayStart.Add(2 * time.Hour)) {
			t.Fatal("no reservation was booked in the first two hours")
		}
		r.pace.runTo(t, at)
		err := r.d.pool.QueryRow(t.Context(),
			`select id, lower(hold_window), upper(hold_window) from reservations order by id limit 1`).Scan(&first.id, &first.start, &first.end)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			t.Fatal(err)
		}
	}
	holds := func() (shown float64, want int64) {
		shown = r.availability()["effective_holds"].(float64)
		want = r.count(`select count(*) from reservations where status = 'confirmed' and hold_window @> $1::timestamptz`, r.clk.Now())
		return shown, want
	}
	// Just before the window closes the hold is effective.
	r.pace.runTo(t, first.end.Add(-time.Minute))
	if shown, want := holds(); want < 1 || shown != float64(want) {
		t.Fatalf("before the window closes: effective_holds = %v, want %d (at least 1)", shown, want)
	}
	// After the tick at the window's end the job has expired it and the hold is gone.
	r.pace.runTo(t, first.end.Add(voidTick))
	var status string
	var expiredAt time.Time
	if err := r.d.pool.QueryRow(t.Context(), `select status, expired_at from reservations where id = $1`, first.id).Scan(&status, &expiredAt); err != nil {
		t.Fatal(err)
	}
	if status != "expired_no_show" || expiredAt.Before(first.end) || expiredAt.After(first.end.Add(voidTick)) {
		t.Errorf("first reservation: %s expired at %s, want expired_no_show at %s (+15s at most)", status, expiredAt, first.end)
	}
	if shown, want := holds(); shown != float64(want) {
		t.Errorf("after the window closes: effective_holds = %v, want %d", shown, want)
	}

	r.pace.runTo(t, weekdayStart.Add(3*time.Hour))
	r.drain()
	// Let every remaining window close, then run the jobs once at that time.
	r.clk.Set(r.clk.Now().Add(2 * time.Hour))
	r.finish()
	if n := r.count(`select count(*) from reservations where status <> 'expired_no_show'`); n != 0 {
		t.Errorf("reservations not expired as no-show = %d, want 0", n)
	}
	if shown, want := holds(); shown != 0 || want != 0 {
		t.Errorf("after every window closed: effective_holds = %v, db = %d, want 0", shown, want)
	}
	if n := r.count(`select coalesce(sum(held), 0) from reservation_slot_loads`); n != 0 {
		t.Errorf("slots still held = %d, want 0 (released)", n)
	}
	if open, other := r.qrEntries(); open+other != 0 {
		t.Errorf("no-show cars presented %d QR codes, want 0", open+other)
	}
}

// TestScenario_ReservationDeterministic: one seed, two fresh databases, one
// trace of the scenario's draws and bookings (FR-SIM-7). Tokens, ids and QR
// values the server invents are scrubbed; the device requests and the driver
// API calls are compared.
func TestScenario_ReservationDeterministic(t *testing.T) {
	run := func() []string {
		r := newScenarioRig(t, weekdayStart)
		r.withDrivers()
		r.start(simulator.ScenarioReservationArrivals, 42)
		r.pace.runTo(t, weekdayStart.Add(2*time.Hour))
		trace := r.tap.trace()
		trace = append(trace, "--- driver API")
		return append(trace, r.dtap.trace()...)
	}
	a, b := run(), run()
	if len(a) < 100 {
		t.Fatalf("trace has %d lines; too few to compare", len(a))
	}
	var qr int
	for _, l := range a {
		if strings.Contains(l, `"value":"<qr>"`) {
			qr++
		}
	}
	if qr == 0 {
		t.Fatal("no reserved car reached the gate; the trace would not cover the reservation draws")
	}
	for i := range min(len(a), len(b)) {
		if a[i] != b[i] {
			t.Fatalf("traces differ at line %d:\n  run 1: %s\n  run 2: %s", i, a[i], b[i])
		}
	}
	if len(a) != len(b) {
		t.Fatalf("trace lengths differ: %d vs %d", len(a), len(b))
	}
}
