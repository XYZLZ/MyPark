package main

import (
	"net/http"
	"testing"
	"time"

	notificationspg "github.com/XYZLZ/MyPark/apps/server/internal/features/notifications/postgres"
	paymentspg "github.com/XYZLZ/MyPark/apps/server/internal/features/payments/postgres"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/reservations"
	reservationspg "github.com/XYZLZ/MyPark/apps/server/internal/features/reservations/postgres"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/database"
	"github.com/XYZLZ/MyPark/apps/server/internal/testsupport"
)

// expireNoShows runs the no-show job as cmd/worker builds it, at the clock's now.
func (e *feeEnv) expireNoShows() int {
	e.t.Helper()
	jobs := reservations.NewJobs(reservations.Deps{
		Tx: database.NewTxRunner(e.db.App), Repo: reservationspg.NewRepo(e.db.App), Clock: e.clk, Log: slogOf(e.logs),
		Fees: paymentspg.NewRepo(e.db.App), Notify: notificationspg.NewNotifier(e.clk),
	})
	n, err := jobs.ExpireNoShows(e.t.Context(), e.clk.Now())
	if err != nil {
		e.t.Fatalf("no-show job: %v", err)
	}
	return n
}

// FR-RES-17 (G6): the third no-show inside the window blocks the driver for the
// captured block days, the create answers 409 with the date, and the email says so.
func TestNoShow_BlocksAfterThreshold(t *testing.T) { assertThirdNoShowBlocks(t) }

// assertThirdNoShowBlocks is the whole flow, shared with TestM3BackendAcceptance.
func assertThirdNoShowBlocks(t *testing.T) {
	t.Helper()
	e := newFeeEnv(t)
	d := testsupport.SeedDrivers(t, e.db, 1)[0]
	id := e.reserve(d, arrivalInWindow)
	if code, body, _ := e.pay(e.token(d), id, "key-noshow-0001", goodCard()); code != http.StatusOK {
		t.Fatalf("pay = %d %v", code, body)
	}
	for _, ago := range []int{9, 5} {
		if _, err := e.db.App.Exec(t.Context(), `insert into reservations (user_id, vehicle_id, lot_id, arrival_at, hold_window, status, fee_cents, tariff_version_id,
		            policy_version_id, payment_due_at, created_at, updated_at, confirmed_at, qr_nonce, expired_at)
		        select user_id, vehicle_id, lot_id, arrival_at - $2 * interval '1 day',
		               tstzrange(lower(hold_window) - $2 * interval '1 day', upper(hold_window) - $2 * interval '1 day', '[)'),
		               'expired_no_show', fee_cents, tariff_version_id, policy_version_id, payment_due_at, created_at, updated_at,
		               confirmed_at, qr_nonce, $3::timestamptz - $2 * interval '1 day'
		        from reservations where public_id = $1`, id, ago, e.clk.Now()); err != nil {
			t.Fatal(err)
		}
	}
	var upper time.Time
	if err := e.db.App.QueryRow(t.Context(), `select upper(hold_window) from reservations where public_id = $1`, id).Scan(&upper); err != nil {
		t.Fatal(err)
	}
	e.clk.Set(upper.Add(time.Minute))
	if n := e.expireNoShows(); n != 1 {
		t.Fatalf("expired = %d, want 1", n)
	}
	want := e.clk.Now().Add(7 * 24 * time.Hour)
	var until time.Time
	if err := e.db.App.QueryRow(t.Context(), `select reservation_blocked_until from users where id = $1`, d.UserID).Scan(&until); err != nil {
		t.Fatal(err)
	}
	if !until.Equal(want) {
		t.Errorf("blocked until %v, want now + 7 d = %v", until, want)
	}

	code, body := (&rsvEnv{lotEnv: e.lotEnv}).post(e.token(d), "/v1/reservations",
		map[string]any{"lot_id": e.publicID, "vehicle_id": d.VehicleID.String(), "arrival_at": "2026-12-02T10:02:00Z"})
	if code != http.StatusConflict || errCode(body) != "reservation.blocked_no_shows" {
		t.Fatalf("create = %d %v, want 409 reservation.blocked_no_shows", code, body)
	}
	if got, _ := field(body, "error.details.blocked_until").(string); got != want.Format(time.RFC3339) {
		t.Errorf("details.blocked_until = %q, want %q", got, want.Format(time.RFC3339))
	}
	if n := e.count(`select count(*) from outbox_messages where template = 'reservation_no_show' and payload ->> 'blocked_until' <> ''`); n != 1 {
		t.Errorf("no-show emails with blocked_until = %d, want 1", n)
	}
	testsupport.AssertSlotInvariant(t, e.db.App, e.lotID)
}

// M3B-60: a check-in voided after its window closed returns to confirmed, and
// the no-show job picks it up.
func TestNoShow_StrandedCheckInAfterWindow(t *testing.T) {
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
		t.Fatalf("status = %s, want confirmed after the void", st)
	}
	if n := e.expireNoShows(); n != 1 {
		t.Fatalf("expired = %d, want 1", n)
	}
	if st := e.status(id); st != "expired_no_show" {
		t.Errorf("status = %s, want expired_no_show", st)
	}
	e.invariants()
}

// M6: the car of a stranded check-in passes after the no-show job expired its
// reservation. The pass is acked and counted, the stay becomes active, and
// the reservation stays expired_no_show (a late entry pass only checks in a
// confirmed reservation).
func TestNoShow_LateEntryPassAfterExpiry(t *testing.T) {
	e := newQREnv(t)
	id, _, qr := e.confirmed(arrivalInWindow)
	_, upper := e.window(id)
	e.clk.Set(upper.Add(-30 * time.Second))
	stay := wantQROpen(t, e.present(qr))
	e.clk.Set(upper.Add(31 * time.Second))
	if n := e.void(); n != 1 {
		t.Fatalf("voided = %d, want 1", n)
	}
	if n := e.expireNoShows(); n != 1 {
		t.Fatalf("expired = %d, want 1", n)
	}
	occupied := e.count(`select occupied from lot_occupancy where lot_id = $1`, e.lotID)

	if got := e.dev.pass(e.barrier, stay); got != (passData{Result: "ack"}) {
		t.Fatalf("late entry pass = %+v, want ack", got)
	}
	if n := e.count(`select count(*) from stays where public_id = $1 and status = 'active'`, stay); n != 1 {
		t.Error("want the stay active")
	}
	if o := e.count(`select occupied from lot_occupancy where lot_id = $1`, e.lotID); o != occupied+1 {
		t.Errorf("occupied = %d, want %d", o, occupied+1)
	}
	if st := e.status(id); st != "expired_no_show" {
		t.Errorf("status = %s, want expired_no_show (unchanged)", st)
	}
	e.invariants()
}

// T327: the no-show job only expires confirmed reservations. Checked-in and
// cancelled ones whose window closed are left alone: no status change, no
// slot change, no refund, no email. A confirmed one in the same run expires,
// so the job did run.
func TestNoShow_SkipsCheckedInAndCancelled(t *testing.T) {
	e := newFeeEnv(t)
	r := &rsvEnv{lotEnv: e.lotEnv}
	ds := testsupport.SeedDrivers(t, e.db, 3)
	arrival := rsvAt(time.Hour)
	checkedIn := r.seedRes(ds[0], "checked_in", arrival)
	cancelled := r.seedRes(ds[1], "cancelled", arrival)
	control := r.seedRes(ds[2], "confirmed", arrival)
	e.clk.Set(arrival.Add(21 * time.Minute)) // past upper(window)

	if n := e.expireNoShows(); n != 1 {
		t.Fatalf("expired = %d, want 1 (only the confirmed one)", n)
	}
	for id, want := range map[string]string{checkedIn.String(): "checked_in", cancelled.String(): "cancelled", control.String(): "expired_no_show"} {
		if got := e.status(id); got != want {
			t.Errorf("reservation %s = %s, want %s", id, got, want)
		}
	}
	if n := e.count(`select count(*) from reservation_slot_loads where held > 0`); n != 0 {
		t.Errorf("slots holding = %d, want 0 (only the control's hold, released)", n)
	}
	if n := e.count(`select count(*) from refunds`); n != 0 {
		t.Errorf("refunds = %d, want none", n)
	}
	if n := e.count(`select count(*) from outbox_messages where template = 'reservation_no_show'`); n != 1 {
		t.Errorf("no-show emails = %d, want 1 (the control)", n)
	}
	testsupport.AssertSlotInvariant(t, e.db.App, e.lotID)
}
