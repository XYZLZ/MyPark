package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/XYZLZ/MyPark/apps/server/internal/features/gate"
	"github.com/XYZLZ/MyPark/apps/server/internal/testsupport"
)

// void runs the reservation-stay void job at the clock's now.
func (e *qrEnv) void() int64 {
	e.t.Helper()
	n, err := newReservationVoider(e.db.App, e.clk, slogOf(e.logs)).Run(e.t.Context(), e.db.App, e.clk.Now())
	if err != nil {
		e.t.Fatalf("reservation void: %v", err)
	}
	return n
}

func (e *qrEnv) window(id string) (lower, upper time.Time) {
	e.t.Helper()
	if err := e.db.App.QueryRow(e.t.Context(), `select lower(hold_window), upper(hold_window) from reservations where public_id = $1`,
		id).Scan(&lower, &upper); err != nil {
		e.t.Fatal(err)
	}
	return lower, upper
}

// stranded checks a reservation in by its QR and lets the 60 s pass by.
func (e *qrEnv) stranded() (id, qr string, stay uuid.UUID) {
	e.t.Helper()
	id, _, qr = e.confirmed(arrivalInWindow)
	stay = wantQROpen(e.t, e.present(qr))
	e.clk.Advance(61 * time.Second)
	return id, qr, stay
}

// M3B-60: a QR accepted without a barrier pass in 60 s returns the
// reservation to confirmed with its hold, in the void transaction.
func TestGateQR_StrandedCheckIn(t *testing.T) {
	t.Run("void restores the hold", func(t *testing.T) {
		e := newQREnv(t)
		id, _, stay := e.stranded()
		before := e.version()
		conn, err := e.db.Super.Acquire(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Release()
		if _, err := conn.Exec(t.Context(), `listen lot_changed`); err != nil {
			t.Fatal(err)
		}

		if n := e.void(); n != 1 {
			t.Fatalf("voided = %d, want 1", n)
		}
		if n := e.count(`select count(*) from stays where public_id = $1 and status = 'voided' and voided_at = $2`, stay, e.clk.Now()); n != 1 {
			t.Error("want the stay voided at now")
		}
		if n := e.count(`select count(*) from reservations where public_id = $1 and status = 'confirmed' and checked_in_at is null`, id); n != 1 {
			t.Error("want the reservation confirmed with checked_in_at cleared")
		}
		if h := e.held(); h == 0 {
			t.Error("want the slots held again")
		}
		if v := e.version(); v != before+1 {
			t.Errorf("version = %d, want %d", v, before+1)
		}
		if _, err := e.db.Super.Exec(t.Context(), `select pg_notify('lot_changed', 'sentinel')`); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
		defer cancel()
		for _, want := range []string{fmt.Sprintf("%d:%d", e.lotID, before+1), "sentinel"} {
			if got, err := conn.Conn().WaitForNotification(ctx); err != nil || got.Payload != want {
				t.Errorf("notification = %v, %v; want %s (exactly one lot_changed)", got, err, want)
			}
		}
		if n := e.void(); n != 0 {
			t.Errorf("second run voided %d, want 0", n)
		}
		e.invariants()
	})

	t.Run("restore above cap", func(t *testing.T) {
		e := newQREnv(t)
		id, _, _ := e.stranded()
		lower, upper := e.window(id)
		others := testsupport.SeedDrivers(t, e.db, 84)
		testsupport.SeedReservations(t, e.db, e.lotID, others, lower.Add(time.Minute), lower, upper, e.clk.Now())
		testsupport.HoldSlots(t, e.db, e.lotID, lower, upper, 84)

		if n := e.void(); n != 1 {
			t.Fatalf("voided = %d, want 1", n)
		}
		if n := e.count(`select count(*) from reservation_slot_loads where lot_id = $1 and held <> 85`, e.lotID); n != 0 {
			t.Errorf("%d slots not at 85, want every slot restored above the cap of 84", n)
		}
		if st := e.status(id); st != "confirmed" {
			t.Errorf("status = %s, want confirmed", st)
		}
		e.invariants()
	})

	t.Run("re-present reuses the voided stay", func(t *testing.T) {
		e := newQREnv(t)
		id, qr, stay := e.stranded()
		e.void()
		voidedAt := e.clk.Now()
		e.clk.Advance(10 * time.Second)

		if again := wantQROpen(t, e.present(qr)); again != stay {
			t.Errorf("stay_id = %s, want the voided %s", again, stay)
		}
		if n := e.count(`select count(*) from stays where public_id = $1 and status = 'entry_pending' and voided_at = $2`, stay, voidedAt); n != 1 {
			t.Error("want the stay entry_pending with voided_at kept")
		}
		if st := e.status(id); st != "checked_in" {
			t.Errorf("status = %s, want checked_in", st)
		}
		if n := e.count(`select count(*) from stays`); n != 1 {
			t.Errorf("stays = %d, want 1", n)
		}
		e.invariants()
	})

	t.Run("re-present while physically full", func(t *testing.T) {
		e := newQREnv(t)
		id, qr, _ := e.stranded()
		e.void()
		testsupport.FillLot(t, e.db, e.lotID, 280, e.clk.Now())

		wantQRDeny(t, e.present(qr), "gate.lot_physically_full")
		if n := e.count(`select count(*) from reservations where public_id = $1 and status = 'cancelled' and refund_reason = 'facility_unavailable'`, id); n != 1 {
			t.Error("want the reservation cancelled, facility_unavailable")
		}
		if n := e.count(`select count(*) from refunds where status = 'pending' and reason = 'facility_unavailable'`); n != 1 {
			t.Error("want one pending refund")
		}
		e.invariants()
	})

	t.Run("never returns", func(t *testing.T) {
		e := newQREnv(t)
		id, _, _ := e.stranded()
		e.void()
		_, upper := e.window(id)
		e.clk.Set(upper.Add(time.Minute))
		if n := e.void(); n != 0 {
			t.Errorf("voided = %d, want 0", n)
		}
		// The no-show job (S7.1) expires it; until then it stays confirmed.
		if st := e.status(id); st != "confirmed" {
			t.Errorf("status = %s, want confirmed until the no-show job", st)
		}
		e.invariants()
	})

	t.Run("void after the window", func(t *testing.T) {
		e := newQREnv(t)
		id, _, qr := e.confirmed(arrivalInWindow)
		_, upper := e.window(id)
		e.clk.Set(upper.Add(-30 * time.Second))
		wantQROpen(t, e.present(qr))
		e.clk.Set(upper.Add(31 * time.Second))

		if n := e.void(); n != 1 {
			t.Fatalf("voided = %d, want 1", n)
		}
		if st := e.status(id); st != "confirmed" {
			t.Errorf("status = %s, want confirmed (the no-show job picks it up)", st)
		}
		e.invariants()
	})
}

// age makes the stay's QR acceptance 61 s old without moving the clock, so
// many rounds fit inside one reservation window.
func (e *qrEnv) age(stay uuid.UUID) {
	e.t.Helper()
	if _, err := e.db.App.Exec(e.t.Context(), `update stays set accepted_at = $2 where public_id = $1`,
		stay, e.clk.Now().Add(-61*time.Second)); err != nil {
		e.t.Fatal(err)
	}
}

// race runs the void job and one device request together, failing on a
// hang (deadlock).
func (e *qrEnv) race(round int, req *http.Request) *httptest.ResponseRecorder {
	e.t.Helper()
	ctx, stop := context.WithTimeout(e.t.Context(), 10*time.Second)
	defer stop()
	voider := newReservationVoider(e.db.App, e.clk, slogOf(e.logs))
	rec, start := httptest.NewRecorder(), make(chan struct{})
	var voidErr error
	var wg sync.WaitGroup
	wg.Go(func() { <-start; _, voidErr = voider.Run(ctx, e.db.App, e.clk.Now()) })
	wg.Go(func() { <-start; e.h.ServeHTTP(rec, req.WithContext(ctx)) })
	close(start)
	wg.Wait()
	if ctx.Err() != nil || voidErr != nil {
		e.t.Fatalf("round %d: did not finish within 10 s (deadlock?) or failed: %v", round, voidErr)
	}
	return rec
}

// design 12.7: the car of a voided stay passes after all; its confirmed
// reservation is checked in again and its hold released.
func TestGateQR_LateEntryPassAfterVoid(t *testing.T) {
	e := newQREnv(t)
	id, _, stay := e.stranded()
	e.void()
	occupied := e.count(`select occupied from lot_occupancy where lot_id = $1`, e.lotID)

	if got := e.dev.pass(e.barrier, stay); got != (passData{Result: "ack"}) {
		t.Fatalf("late entry pass = %+v, want ack", got)
	}
	if n := e.count(`select count(*) from stays where public_id = $1 and status = 'active'`, stay); n != 1 {
		t.Error("want the stay active")
	}
	if n := e.count(`select count(*) from reservations where public_id = $1 and status = 'checked_in' and checked_in_at = $2`, id, e.clk.Now()); n != 1 {
		t.Error("want the reservation checked_in at now")
	}
	if h := e.held(); h != 0 {
		t.Errorf("slots holding = %d, want 0 (released)", h)
	}
	if o := e.count(`select occupied from lot_occupancy where lot_id = $1`, e.lotID); o != occupied+1 {
		t.Errorf("occupied = %d, want %d", o, occupied+1)
	}
	e.invariants()
}

// A reservation cancelled after the void stays cancelled; only the stay
// changes, and the car inside is billed normally.
func TestGateQR_LateEntryPassAfterCancel(t *testing.T) {
	e := newQREnv(t)
	id, tok, qr := e.confirmed(arrivalInWindow)
	stay := wantQROpen(t, e.present(qr))
	e.clk.Advance(61 * time.Second)
	e.void()
	if code, body := e.cancel(tok, id); code != http.StatusOK {
		t.Fatalf("cancel = %d %v", code, body)
	}

	if got := e.dev.pass(e.barrier, stay); got != (passData{Result: "ack"}) {
		t.Fatalf("late entry pass = %+v, want ack", got)
	}
	if n := e.count(`select count(*) from stays where public_id = $1 and status = 'active'`, stay); n != 1 {
		t.Error("want the stay active")
	}
	if st := e.status(id); st != "cancelled" {
		t.Errorf("status = %s, want cancelled", st)
	}
	if h := e.held(); h != 0 {
		t.Errorf("slots holding = %d, want 0", h)
	}
	e.invariants()
}

// The void job and the entry pass race on a stale stay: the lot lock
// orders them. Either the pass wins (the void's re-check rolls back) or the
// void wins and the pass is a late entry pass; both end active and
// checked_in, with the hold released.
func TestGateQR_VoidRacesLateEntryPass(t *testing.T) {
	e := newQREnv(t)
	for round := range 20 {
		id, _, qr := e.confirmed(arrivalInWindow)
		stay := wantQROpen(t, e.present(qr))
		e.age(stay)
		req := e.barrier.Request(t, deviceEventsPath, e.dev.passBody(gate.DirectionEntry, stay), e.clk.Now())

		if got := passOf(t, e.race(round, req)); got.Result != "ack" {
			t.Errorf("round %d: pass = %+v, want ack", round, got)
		}
		if n := e.count(`select count(*) from stays s join reservations r on r.id = s.reservation_id
		                 where r.public_id = $1 and s.status = 'active' and r.status = 'checked_in'`, id); n != 1 {
			t.Errorf("round %d: want the stay active and the reservation checked_in", round)
		}
	}
	t.Logf("void won %d of 20 rounds", e.count(`select count(*) from stays where voided_at is not null`))
	if h := e.held(); h != 0 {
		t.Errorf("slots holding = %d, want 0", h)
	}
	e.invariants()
}

// M3B-24: a stay past its 60 s is not refreshed. Re-presented before the
// void job ran, it is voided and its hold restored in the QR transaction,
// then the confirmed path reuses it.
func TestGateQR_RePresentStale(t *testing.T) {
	e := newQREnv(t)
	id, qr, stay := e.stranded()
	if again := wantQROpen(t, e.present(qr)); again != stay {
		t.Errorf("stay_id = %s, want the same %s", again, stay)
	}
	if n := e.count(`select count(*) from stays where public_id = $1 and status = 'entry_pending' and accepted_at = $2 and voided_at = $2`,
		stay, e.clk.Now()); n != 1 {
		t.Error("want the stay voided and reopened at now")
	}
	if st := e.status(id); st != "checked_in" {
		t.Errorf("status = %s, want checked_in", st)
	}
	if h := e.held(); h != 0 {
		t.Errorf("slots holding = %d, want 0", h)
	}
	if n := e.void(); n != 0 {
		t.Errorf("voided = %d, want 0 (the stay is fresh again)", n)
	}
	e.invariants()
}

// A stale stay re-presented into a physically full lot: voided, and the
// confirmed reservation is cancelled with a refund (design 11.5).
func TestGateQR_RePresentStalePhysicallyFull(t *testing.T) {
	e := newQREnv(t)
	id, qr, stay := e.stranded()
	testsupport.FillLot(t, e.db, e.lotID, 280, e.clk.Now())

	wantQRDeny(t, e.present(qr), "gate.lot_physically_full")
	if st := e.status(id); st != "cancelled" {
		t.Errorf("status = %s, want cancelled", st)
	}
	if n := e.count(`select count(*) from stays where public_id = $1 and status = 'voided'`, stay); n != 1 {
		t.Error("want the stay voided")
	}
	if n := e.count(`select count(*) from refunds where status = 'pending' and reason = 'facility_unavailable'`); n != 1 {
		t.Error("want one pending refund")
	}
	e.invariants()
}

// The void job and a QR re-presented on a stale stay race: the lot lock
// orders them. Either the QR voids and reopens it inline (the job's
// re-check rolls back) or the job voids it and the QR reopens it. The QR
// opens the same stay every round and never adds a second one.
func TestGateQR_VoidRacesRePresent(t *testing.T) {
	e := newQREnv(t)
	for round := range 20 {
		id, _, qr := e.confirmed(arrivalInWindow)
		stay := wantQROpen(t, e.present(qr))
		e.age(stay)
		req := e.barrier.Request(t, deviceEventsPath, e.qrBody(qr), e.clk.Now())

		if got := entryOf(t, e.race(round, req)); got.Result != "open" || got.StayID == nil || *got.StayID != stay {
			t.Errorf("round %d: qr = %+v, want open with stay %s", round, got, stay)
		}
		if n := e.count(`select count(*) from stays s join reservations r on r.id = s.reservation_id
		                 where r.public_id = $1 and s.status = 'entry_pending' and s.accepted_at = $2 and r.status = 'checked_in'`,
			id, e.clk.Now()); n != 1 {
			t.Errorf("round %d: want the stay entry_pending at now and the reservation checked_in", round)
		}
	}
	if n := e.count(`select count(*) from stays`); n != 20 {
		t.Errorf("stays = %d, want one per reservation", n)
	}
	if h := e.held(); h != 0 {
		t.Errorf("slots holding = %d, want 0", h)
	}
	e.invariants()
}

// The voider is on the device hot path (it takes the lot lock): its
// transaction runs under the device limits, so a held lot lock times it out
// within about a second, the candidate is logged and retried next tick.
func TestGateQR_VoidLockTimeout(t *testing.T) {
	e := newQREnv(t)
	id, _, _ := e.stranded()
	side, err := e.db.Super.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = side.Rollback(context.WithoutCancel(t.Context())) }()
	if _, err := side.Exec(t.Context(), `select 1 from lot_occupancy where lot_id = $1 for update`, e.lotID); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	n, err := newReservationVoider(e.db.App, e.clk, slogOf(e.logs)).Run(ctx, e.db.App, e.clk.Now())
	if err != nil || n != 0 || ctx.Err() != nil {
		t.Fatalf("Run under a held lot lock = %d, %v (ctx %v); want 0 within the 1 s lock timeout", n, err, ctx.Err())
	}
	if !strings.Contains(e.logs.String(), "55P03") {
		t.Error("want the lock timeout logged")
	}
	if err := side.Rollback(t.Context()); err != nil {
		t.Fatal(err)
	}
	if n := e.void(); n != 1 {
		t.Errorf("next tick voided %d, want 1", n)
	}
	if st := e.status(id); st != "confirmed" {
		t.Errorf("status = %s, want confirmed", st)
	}
	e.invariants()
}
