package main

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"

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
