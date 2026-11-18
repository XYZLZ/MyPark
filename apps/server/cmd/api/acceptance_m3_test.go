package main

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/XYZLZ/MyPark/apps/server/internal/domain/authz"
	"github.com/XYZLZ/MyPark/apps/server/internal/testsupport"
)

// m3Criteria maps every spec §8 M3 exit criterion (design §15.7) to the
// subtest that asserts it and to the test that holds the fuller evidence.
// A failure prints it.
var m3Criteria = [][2]string{
	{"200 parallel attempts never exceed the cap",
		"subtest 'cap holds under 200 parallel' (100 attempts); the full run is TestReservations_CapConcurrent200"},
	{"Guarantee scenario end to end with the simulator",
		"subtest 'guarantee'; end to end: TestScenario_Guarantee, TestGateQR_Guarantee"},
	{"Cancel before arrival refunds, after arrival does not, a no-show expires",
		"subtest 'cancel and no-show'; also the cancel suite, the refund job and TestNoShow_*"},
	{"Simulated decline (402) and timeout (202) paths work",
		"subtest 'decline and timeout'; also TestPay_DeclineThenRetry, TestReconcile_ConfirmsTimeoutCard"},
	{"QR while physically full gives gate.lot_physically_full, cancels and the refund succeeds",
		"subtest 'physically full'; also TestGateQR_PhysicallyFull"},
	{"3 no-shows in 30 days give reservation.blocked_no_shows",
		"subtest 'three no-shows block'; also TestNoShow_BlocksAfterThreshold"},
}

// Spec §8 M3 exit criteria, on the backend. Each subtest runs a minimal real
// flow through the production handler and asserts its outcome; the long-form
// evidence is in the test each criterion names. The UI side is Track B.
func TestM3BackendAcceptance(t *testing.T) {
	t.Cleanup(func() {
		if !t.Failed() {
			return
		}
		for _, c := range m3Criteria {
			t.Logf("M3 exit criterion %q is proven by %s", c[0], c[1])
		}
	})

	t.Run("cap holds under 200 parallel", func(t *testing.T) {
		e := newRsvEnv(t)
		created, capacity, other := e.race(testsupport.SeedDrivers(t, e.db, 100))
		if created != 84 || capacity != 16 || len(other) != 0 {
			t.Fatalf("created = %d, capacity_reached = %d, other = %v, want 84, 16, none", created, capacity, other)
		}
		e.assertAllSlotsHeld(84)
		testsupport.AssertSlotInvariant(t, e.db.App, e.lotID)
	})

	t.Run("guarantee", func(t *testing.T) {
		e := newQREnv(t)
		_, _, qr := e.confirmed(arrivalInWindow)
		testsupport.FillLot(t, e.db, e.lotID, 279, e.clk.Now())
		wantDeny(t, e.dev.ticketRequest(e.dev.device(authz.DeviceDispenser)), "gate.lot_full")
		wantQROpen(t, e.present(qr))
		e.invariants()
	})

	t.Run("cancel and no-show", func(t *testing.T) {
		t.Run("before arrival refunds", func(t *testing.T) {
			e := newFeeEnv(t)
			id, tok := e.paid(arrival1502)
			if code, body := e.cancel(tok, id); code != http.StatusOK || field(body, "data.refund_reason") != "cancelled_before_arrival" {
				t.Fatalf("cancel = %d %v, want 200 cancelled_before_arrival", code, body)
			}
			if n, err := e.processor(e.simGateway(&refundGateway{})).Run(t.Context(), e.clk.Now()); err != nil || n != 1 {
				t.Fatalf("refund Run = %d, %v; want 1", n, err)
			}
			if e.count(`select count(*) from refunds where status = 'succeeded' and amount_cents = 15000`) != 1 ||
				e.count(`select count(*) from payments where status = 'refunded'`) != 1 {
				t.Error("want one succeeded full refund and the payment refunded")
			}
			testsupport.AssertSlotInvariant(t, e.db.App, e.lotID)
		})
		t.Run("after arrival does not", func(t *testing.T) {
			e := newFeeEnv(t)
			d := testsupport.SeedDrivers(t, e.db, 1)[0]
			id := e.reserve(d, arrival1502)
			if code, body, _ := e.pay(e.token(d), id, "key-accept-0001", goodCard()); code != http.StatusOK {
				t.Fatalf("pay = %d %v", code, body)
			}
			e.clk.Set(time.Date(2026, 12, 1, 15, 5, 0, 0, time.UTC)) // arrival_at after normalization
			code, body := e.cancel(e.token(d), id)
			if code != http.StatusOK || field(body, "data.refund_reason") != "none_after_arrival" || field(body, "data.refund") != nil {
				t.Fatalf("cancel = %d %v, want 200 none_after_arrival and no refund", code, body)
			}
			if n := e.count(`select count(*) from refunds`); n != 0 {
				t.Errorf("refund rows = %d, want none", n)
			}
			testsupport.AssertSlotInvariant(t, e.db.App, e.lotID)
		})
		t.Run("no-show expires", func(t *testing.T) {
			e := newQREnv(t)
			id, _, _ := e.confirmed(arrivalInWindow)
			_, upper := e.window(id)
			e.clk.Set(upper.Add(time.Minute))
			if n := e.expireNoShows(); n != 1 {
				t.Fatalf("expired = %d, want 1", n)
			}
			if st := e.status(id); st != "expired_no_show" {
				t.Errorf("status = %s, want expired_no_show", st)
			}
			if h := e.held(); h != 0 {
				t.Errorf("slots still held: %d rows", h)
			}
			e.invariants()
		})
	})

	t.Run("decline and timeout", func(t *testing.T) {
		e := newFeeEnv(t)
		d := testsupport.SeedDrivers(t, e.db, 1)[0]
		id, tok := e.reserve(d, arrival1502), e.token(d)
		if code, body, _ := e.pay(tok, id, "key-accept-decl", cardWith("4000000000000002")); code != http.StatusPaymentRequired || errCode(body) != "payment.declined" {
			t.Fatalf("declined pay = %d %v, want 402 payment.declined", code, body)
		}
		if code, body, _ := e.pay(tok, id, "key-accept-good", goodCard()); code != http.StatusOK || e.status(id) != "confirmed" {
			t.Fatalf("retry = %d %v, status %s; want 200 confirmed", code, body, e.status(id))
		}

		pendingID, _ := e.payTimeout() // asserts the 202 and Retry-After
		rec, _ := e.reconciler()
		e.clk.Advance(61 * time.Second)
		if n, err := rec.Run(t.Context(), e.clk.Now()); err != nil || n != 1 {
			t.Fatalf("reconcile Run = %d, %v; want 1", n, err)
		}
		if st := e.status(pendingID); st != "confirmed" {
			t.Errorf("timeout reservation = %s, want confirmed", st)
		}
		testsupport.AssertSlotInvariant(t, e.db.App, e.lotID)
	})

	t.Run("physically full", func(t *testing.T) {
		e := newQREnv(t)
		id, _, qr := e.confirmed(arrivalInWindow)
		testsupport.FillLot(t, e.db, e.lotID, 280, e.clk.Now())
		wantQRDeny(t, e.present(qr), "gate.lot_physically_full")
		if st := e.status(id); st != "cancelled" {
			t.Fatalf("status = %s, want cancelled", st)
		}
		if n, err := e.processor(e.simGateway(&refundGateway{})).Run(t.Context(), e.clk.Now()); err != nil || n != 1 {
			t.Fatalf("refund Run = %d, %v; want 1", n, err)
		}
		if e.count(`select count(*) from refunds where status = 'succeeded' and reason = 'facility_unavailable'`) != 1 {
			t.Error("want the facility_unavailable refund succeeded")
		}
		e.invariants()
	})

	t.Run("three no-shows block", func(t *testing.T) { assertThirdNoShowBlocks(t) })
}

// M3B-55: a lock timeout (55P03) and a statement timeout (57014) inside a
// driver-API transaction answer 500 internal.unexpected, leak nothing of the
// database error to the client and store nothing.
func TestDriverTx_TimeoutsAre500AndStoreNothing(t *testing.T) {
	check := func(t *testing.T, e *rsvEnv, d testsupport.Driver, sqlstate string) {
		t.Helper()
		start := time.Now()
		rec := e.serve(jsonReq(http.MethodPost, "/v1/reservations", e.createBody(d, arrival1502)), e.token(d))
		body := rec.Body.String()
		if rec.Code != http.StatusInternalServerError || errCode(decodeBody(t, rec)) != "internal.unexpected" {
			t.Fatalf("create = %d %s, want 500 internal.unexpected", rec.Code, body)
		}
		if elapsed := time.Since(start); elapsed > 10*time.Second {
			t.Errorf("the request took %v; the timeout did not cut it", elapsed)
		}
		for _, leak := range []string{sqlstate, "timeout", "canceling", "SQLSTATE"} {
			if strings.Contains(strings.ToLower(body), strings.ToLower(leak)) {
				t.Errorf("the error body leaks %q: %s", leak, body)
			}
		}
		if !strings.Contains(e.logs.String(), sqlstate) {
			t.Errorf("the cause (SQLSTATE %s) was not logged:\n%.600s", sqlstate, e.logs.String())
		}
		if n := e.count(`select count(*) from reservations`); n != 0 {
			t.Errorf("reservations = %d, want 0 (rolled back)", n)
		}
		if n := e.count(`select count(*) from reservation_slot_loads where held > 0`); n != 0 {
			t.Errorf("slot rows holding = %d, want 0", n)
		}
		testsupport.AssertSlotInvariant(t, e.db.App, e.lotID)
	}

	t.Run("lock timeout", func(t *testing.T) {
		e := newRsvEnv(t)
		d := testsupport.SeedDrivers(t, e.db, 1)[0]
		side, err := e.db.Super.Begin(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = side.Rollback(context.WithoutCancel(t.Context())) }()
		// The create takes the vehicle FOR SHARE, so a writer lock holds it up.
		if _, err := side.Exec(t.Context(), `select 1 from vehicles where public_id = $1 for update`, d.VehicleID); err != nil {
			t.Fatal(err)
		}
		check(t, e, d, "55P03")
	})

	t.Run("statement timeout", func(t *testing.T) {
		e := newRsvEnv(t)
		d := testsupport.SeedDrivers(t, e.db, 1)[0]
		// A statement that outlives the 2 s limit: the insert fires a slow trigger.
		for _, stmt := range []string{
			`create function slow_insert() returns trigger language plpgsql as $$ begin perform pg_sleep(4); return new; end $$`,
			`create trigger slow_insert before insert on reservations for each row execute function slow_insert()`,
		} {
			if _, err := e.db.Super.Exec(t.Context(), stmt); err != nil {
				t.Fatal(err)
			}
		}
		check(t, e, d, "57014")
	})
}
