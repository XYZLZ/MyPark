package main

import (
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/XYZLZ/MyPark/apps/server/internal/testsupport"
)

// The window of arrival1502 (normalized 15:05): 14:50 to 15:25, seven slots.
var (
	capWindowStart = time.Date(2026, 12, 1, 14, 50, 0, 0, time.UTC)
	capWindowEnd   = time.Date(2026, 12, 1, 15, 25, 0, 0, time.UTC)
)

// fillWindow makes n drivers hold the window, with matching slot rows, so the
// slot invariant holds. It returns the drivers it consumed.
func (e *rsvEnv) fillWindow(n int) {
	e.t.Helper()
	holders := testsupport.SeedDrivers(e.t, e.db, n)
	arrival := capWindowStart.Add(15 * time.Minute)
	testsupport.SeedReservations(e.t, e.db, e.lotID, holders, arrival, capWindowStart, capWindowEnd, rsvNow)
	testsupport.HoldSlots(e.t, e.db, e.lotID, capWindowStart, capWindowEnd, n)
}

// race posts one create per driver at once and tallies the answers.
func (e *rsvEnv) race(ds []testsupport.Driver) (created int, capacity int, other []string) {
	e.t.Helper()
	type res struct {
		code int
		err  string
	}
	toks := make([]string, len(ds))
	for i, d := range ds {
		toks[i] = e.token(d)
	}
	out := make([]res, len(ds))
	var wg sync.WaitGroup
	for i, d := range ds {
		wg.Go(func() {
			code, body := e.post(toks[i], "/v1/reservations", e.createBody(d, arrival1502))
			out[i] = res{code, errCode(body)}
		})
	}
	wg.Wait()
	for _, r := range out {
		switch {
		case r.code == http.StatusCreated:
			created++
		case r.code == http.StatusConflict && r.err == "reservation.capacity_reached":
			capacity++
		default:
			other = append(other, r.err)
		}
	}
	return created, capacity, other
}

func (e *rsvEnv) assertAllSlotsHeld(want int) {
	e.t.Helper()
	if n := e.count(`select count(*) from reservation_slot_loads where lot_id = $1 and held = $2`, e.lotID, want); n != 7 {
		e.t.Errorf("slots held %d = %d, want 7", want, n)
	}
}

// Two creates race at the 83 to 84 boundary: exactly one fits.
func TestReservations_CapAtomicPair(t *testing.T) {
	e := newRsvEnv(t)
	e.fillWindow(83)
	created, capacity, other := e.race(testsupport.SeedDrivers(t, e.db, 2))
	if created != 1 || capacity != 1 || len(other) != 0 {
		t.Fatalf("created = %d, capacity_reached = %d, other = %v, want 1, 1, none", created, capacity, other)
	}
	e.assertAllSlotsHeld(84)
	testsupport.AssertSlotInvariant(t, e.db.App, e.lotID)
}

func TestReservations_CapConcurrent200(t *testing.T) {
	e := newRsvEnv(t)
	created, capacity, other := e.race(testsupport.SeedDrivers(t, e.db, 200))
	if created != 84 || capacity != 116 || len(other) != 0 {
		t.Fatalf("created = %d, capacity_reached = %d, other = %v, want 84, 116, none", created, capacity, other)
	}
	if logs := e.logs.String(); strings.Contains(logs, "40P01") || strings.Contains(logs, "internal error") {
		t.Errorf("deadlock or internal error in logs: %.500s", logs)
	}
	e.assertAllSlotsHeld(84)
	testsupport.AssertSlotInvariant(t, e.db.App, e.lotID)
}

func TestReservations_CapRollbackLeavesNoRow(t *testing.T) {
	e := newRsvEnv(t)
	e.fillWindow(84)
	d := testsupport.SeedDrivers(t, e.db, 1)[0]
	code, body := e.post(e.token(d), "/v1/reservations", e.createBody(d, arrival1502))
	if code != http.StatusConflict || errCode(body) != "reservation.capacity_reached" {
		t.Fatalf("create = %d %v, want 409 reservation.capacity_reached", code, body)
	}
	if n := e.count(`select count(*) from reservations where user_id = $1`, d.UserID); n != 0 {
		t.Errorf("reservations of the driver = %d, want 0 (insert rolled back)", n)
	}
	e.assertAllSlotsHeld(84)
	testsupport.AssertSlotInvariant(t, e.db.App, e.lotID)
}
