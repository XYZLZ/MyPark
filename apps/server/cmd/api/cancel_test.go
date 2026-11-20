package main

import (
	"context"
	"fmt"
	"net/http"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	notificationspg "github.com/XYZLZ/MyPark/apps/server/internal/features/notifications/postgres"
	paymentspg "github.com/XYZLZ/MyPark/apps/server/internal/features/payments/postgres"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/reservations"
	reservationspg "github.com/XYZLZ/MyPark/apps/server/internal/features/reservations/postgres"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/database"
	"github.com/XYZLZ/MyPark/apps/server/internal/testsupport"
)

// arrivalInWindow normalizes to 14:05: its window 13:50 to 14:25 contains rsvNow.
const arrivalInWindow = "2026-12-01T14:02:00Z"

func (e *feeEnv) cancel(token, id string) (int, map[string]any) {
	e.t.Helper()
	return (&rsvEnv{lotEnv: e.lotEnv}).post(token, "/v1/reservations/"+id+"/cancel", nil)
}

// paid reserves and pays for a fresh driver, leaving a confirmed reservation.
func (e *feeEnv) paid(arrival string) (id, tok string) {
	e.t.Helper()
	d := testsupport.SeedDrivers(e.t, e.db, 1)[0]
	id, tok = e.reserve(d, arrival), e.token(d)
	if code, body, _ := e.pay(tok, id, "key-cancel-0001", goodCard()); code != http.StatusOK {
		e.t.Fatalf("pay = %d %v", code, body)
	}
	return id, tok
}

func (e *feeEnv) held() int {
	return e.count(`select count(*) from reservation_slot_loads where held > 0`)
}

func TestCancel_PendingWithoutAttempt(t *testing.T) {
	e := newFeeEnv(t)
	d := testsupport.SeedDrivers(t, e.db, 1)[0]
	id, tok := e.reserve(d, arrival1502), e.token(d)
	if e.held() == 0 {
		t.Fatal("the reservation must hold slots before the cancel")
	}

	code, body := e.cancel(tok, id)
	if code != http.StatusOK {
		t.Fatalf("cancel = %d %v, want 200", code, body)
	}
	for path, want := range map[string]any{
		"data.status": "cancelled", "data.cancelled_by": "driver", "data.refund_reason": nil, "data.refund": nil,
	} {
		if got := field(body, path); got != want {
			t.Errorf("%s = %v, want %v", path, got, want)
		}
	}
	if field(body, "data.cancelled_at") == nil {
		t.Error("cancelled_at is null")
	}
	if n := e.held(); n != 0 {
		t.Errorf("slots still held: %d rows", n)
	}
	if n := e.count(`select count(*) from outbox_messages where template = 'reservation_cancelled'`); n != 0 {
		t.Errorf("an unpaid cancel sent %d emails, want none", n)
	}
	testsupport.AssertSlotInvariant(t, e.db.App, e.lotID)
}

func TestCancel_PendingWithPendingAttempt(t *testing.T) {
	e := newFeeEnv(t)
	id, tok := e.payTimeout()
	held := e.held()

	rec := (&rsvEnv{lotEnv: e.lotEnv}).serve(jsonReq(http.MethodPost, "/v1/reservations/"+id+"/cancel", nil), tok)
	wantError(t, rec, http.StatusConflict, "payment.in_progress")
	if got := e.status(id); got != "pending_payment" {
		t.Errorf("status = %s, want pending_payment", got)
	}
	if n := e.held(); n != held {
		t.Errorf("held rows = %d, want unchanged %d", n, held)
	}
	testsupport.AssertSlotInvariant(t, e.db.App, e.lotID)
}

func TestCancel_ConfirmedBeforeArrival(t *testing.T) {
	e := newFeeEnv(t)
	id, tok := e.paid(arrival1502)

	code, body := e.cancel(tok, id)
	if code != http.StatusOK {
		t.Fatalf("cancel = %d %v, want 200", code, body)
	}
	for path, want := range map[string]any{
		"data.status": "cancelled", "data.cancelled_by": "driver", "data.refund_reason": "cancelled_before_arrival",
		"data.refund.status": "pending", "data.refund.amount_cents": float64(15000),
		"data.refund.reason": "cancelled_before_arrival", "data.refund.succeeded_at": nil,
	} {
		if got := field(body, path); got != want {
			t.Errorf("%s = %v, want %v", path, got, want)
		}
	}
	if n := e.count(`select count(*) from refunds where status = 'pending' and attempts = 0 and amount_cents = 15000
	                 and idempotency_key = 'refund:' || public_id and next_attempt_at = $1`, rsvNow); n != 1 {
		t.Error("want one pending refund of the full fee, due now, keyed by its public id")
	}
	var status, reason, refundReason string
	if err := e.db.App.QueryRow(t.Context(),
		`select payload->>'refund_status', payload->>'reservation_id', payload->>'refund_reason'
		 from outbox_messages where dedupe_key = $1`, "reservation_cancelled:"+id).Scan(&status, &reason, &refundReason); err != nil {
		t.Fatalf("outbox row: %v", err)
	}
	if status != "pending" || reason != id || refundReason != "cancelled_before_arrival" {
		t.Errorf("email payload = %s %s %s", status, reason, refundReason)
	}
	if n := e.held(); n != 0 {
		t.Errorf("slots still held: %d rows", n)
	}
	testsupport.AssertSlotInvariant(t, e.db.App, e.lotID)
}

func TestCancel_ConfirmedAtArrival(t *testing.T) {
	e := newFeeEnv(t)
	d := testsupport.SeedDrivers(t, e.db, 1)[0]
	id := e.reserve(d, arrival1502)
	if code, body, _ := e.pay(e.token(d), id, "key-cancel-0001", goodCard()); code != http.StatusOK {
		t.Fatalf("pay = %d %v", code, body)
	}
	e.clk.Set(time.Date(2026, 12, 1, 15, 5, 0, 0, time.UTC)) // arrival_at after normalization
	tok := e.token(d)                                        // the first token has expired by now

	code, body := e.cancel(tok, id)
	if code != http.StatusOK {
		t.Fatalf("cancel = %d %v, want 200", code, body)
	}
	if got := field(body, "data.refund_reason"); got != "none_after_arrival" {
		t.Errorf("refund_reason = %v, want none_after_arrival", got)
	}
	if got := field(body, "data.refund"); got != nil {
		t.Errorf("refund = %v, want null", got)
	}
	if n := e.count(`select count(*) from refunds`); n != 0 {
		t.Errorf("refund rows = %d, want none", n)
	}
	if n := e.count(`select count(*) from outbox_messages where dedupe_key = $1 and payload->>'refund_status' = 'none'`,
		"reservation_cancelled:"+id); n != 1 {
		t.Error("want one reservation_cancelled email with refund_status none")
	}
	if n := e.held(); n != 0 {
		t.Errorf("slots still held: %d rows", n)
	}
	testsupport.AssertSlotInvariant(t, e.db.App, e.lotID)
}

func TestCancel_InsideWindowBumpsVersion(t *testing.T) {
	t.Run("window contains now", func(t *testing.T) {
		e := newFeeEnv(t)
		id, tok := e.paid(arrivalInWindow)
		conn, err := e.db.App.Acquire(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Release()
		if _, err := conn.Exec(t.Context(), `listen lot_changed`); err != nil {
			t.Fatal(err)
		}
		before := e.version()
		if code, body := e.cancel(tok, id); code != http.StatusOK {
			t.Fatalf("cancel = %d %v", code, body)
		}
		if got := e.version(); got != before+1 {
			t.Errorf("version = %d, want %d", got, before+1)
		}
		ctx, stop := context.WithTimeout(t.Context(), 5*time.Second)
		defer stop()
		n, err := conn.Conn().WaitForNotification(ctx)
		if err != nil || n.Payload != fmt.Sprintf("%d:%d", e.lotID, before+1) {
			t.Fatalf("notification = %+v %v, want lot_changed %d:%d", n, err, e.lotID, before+1)
		}
		short, stop2 := context.WithTimeout(t.Context(), 200*time.Millisecond)
		defer stop2()
		if extra, err := conn.Conn().WaitForNotification(short); err == nil {
			t.Errorf("a second lot_changed arrived: %+v", extra)
		}
		testsupport.AssertSlotInvariant(t, e.db.App, e.lotID)
	})
	t.Run("window in the future", func(t *testing.T) {
		e := newFeeEnv(t)
		id, tok := e.paid(arrival1502)
		before := e.version()
		if code, body := e.cancel(tok, id); code != http.StatusOK {
			t.Fatalf("cancel = %d %v", code, body)
		}
		if got := e.version(); got != before {
			t.Errorf("version = %d, want unchanged %d", got, before)
		}
		testsupport.AssertSlotInvariant(t, e.db.App, e.lotID)
	})
	t.Run("pending reservation inside the window", func(t *testing.T) {
		e := newFeeEnv(t)
		d := testsupport.SeedDrivers(t, e.db, 1)[0]
		id, tok := e.reserve(d, arrivalInWindow), e.token(d)
		before := e.version()
		if code, body := e.cancel(tok, id); code != http.StatusOK {
			t.Fatalf("cancel = %d %v", code, body)
		}
		if got := e.version(); got != before {
			t.Errorf("version = %d, want unchanged %d: a pending hold never counted", got, before)
		}
		testsupport.AssertSlotInvariant(t, e.db.App, e.lotID)
	})
}

func TestCancel_Idempotent(t *testing.T) {
	e := newFeeEnv(t)
	id, tok := e.paid(arrival1502)
	code1, first := e.cancel(tok, id)
	e.clk.Advance(time.Minute)
	code2, second := e.cancel(tok, id)
	if code1 != http.StatusOK || code2 != http.StatusOK {
		t.Fatalf("cancels = %d, %d, want 200, 200", code1, code2)
	}
	delete(first, "meta")
	delete(second, "meta")
	if !reflect.DeepEqual(first, second) {
		t.Errorf("second body = %v, want the first %v", second, first)
	}
	if n := e.count(`select count(*) from refunds`); n != 1 {
		t.Errorf("refund rows = %d, want 1", n)
	}
	if n := e.count(`select count(*) from outbox_messages where template = 'reservation_cancelled'`); n != 1 {
		t.Errorf("cancellation emails = %d, want 1", n)
	}
	testsupport.AssertSlotInvariant(t, e.db.App, e.lotID)
}

func TestCancel_InvalidStates(t *testing.T) {
	for _, status := range []string{"checked_in", "completed", "expired_unpaid", "expired_no_show"} {
		t.Run(status, func(t *testing.T) {
			e := newFeeEnv(t)
			d := testsupport.SeedDrivers(t, e.db, 1)[0]
			arrival := time.Date(2026, 12, 1, 15, 5, 0, 0, time.UTC)
			id := (&rsvEnv{lotEnv: e.lotEnv}).seedRes(d, status, arrival).String()
			rec := (&rsvEnv{lotEnv: e.lotEnv}).serve(jsonReq(http.MethodPost, "/v1/reservations/"+id+"/cancel", nil), e.token(d))
			wantError(t, rec, http.StatusConflict, "reservation.invalid_state")
			if got := field(decodeBody(t, rec), "error.details.status"); got != status {
				t.Errorf("details.status = %v, want %s", got, status)
			}
		})
	}
	t.Run("cancelled by the system", func(t *testing.T) {
		e := newFeeEnv(t)
		d := testsupport.SeedDrivers(t, e.db, 1)[0]
		id := (&rsvEnv{lotEnv: e.lotEnv}).seedRes(d, "cancelled", time.Date(2026, 12, 1, 15, 5, 0, 0, time.UTC)).String()
		if _, err := e.db.Super.Exec(t.Context(), `update reservations set cancelled_by = 'system' where public_id = $1`, id); err != nil {
			t.Fatal(err)
		}
		rec := (&rsvEnv{lotEnv: e.lotEnv}).serve(jsonReq(http.MethodPost, "/v1/reservations/"+id+"/cancel", nil), e.token(d))
		wantError(t, rec, http.StatusConflict, "reservation.invalid_state")
	})
}

// Another driver's id, an unknown id and a malformed one are the same 404.
func TestCancel_NotTheOwners(t *testing.T) {
	e := newFeeEnv(t)
	ds := testsupport.SeedDrivers(t, e.db, 2)
	id := e.reserve(ds[0], arrival1502)
	other := e.token(ds[1])
	for _, target := range []string{id, "00000000-0000-4000-8000-000000000000", "nope"} {
		rec := (&rsvEnv{lotEnv: e.lotEnv}).serve(jsonReq(http.MethodPost, "/v1/reservations/"+target+"/cancel", nil), other)
		wantError(t, rec, http.StatusNotFound, "reservation.not_found")
	}
	if got := e.status(id); got != "pending_payment" {
		t.Errorf("status = %s, want untouched", got)
	}
}

// tickClock answers the times in order, then the last one forever.
type tickClock struct {
	mu    sync.Mutex
	times []time.Time
}

func (c *tickClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	t := c.times[0]
	if len(c.times) > 1 {
		c.times = c.times[1:]
	}
	return t
}

// F3: a cancel that waits on locks across arrival_at decides the refund at the
// time it holds them. The first read (lot-lock decision) is just before arrival,
// the next ones are at arrival.
func TestCancel_RefundDecidedAfterTheLocks(t *testing.T) {
	e := newFeeEnv(t)
	d := testsupport.SeedDrivers(t, e.db, 1)[0]
	id := e.reserve(d, arrival1502)
	if code, body, _ := e.pay(e.token(d), id, "key-cancel-0001", goodCard()); code != http.StatusOK {
		t.Fatalf("pay = %d %v", code, body)
	}
	arrival := time.Date(2026, 12, 1, 15, 5, 0, 0, time.UTC)
	svc := reservations.NewService(reservations.Deps{
		Tx: database.NewTxRunner(e.db.App), Repo: reservationspg.NewRepo(e.db.App),
		Clock: &tickClock{times: []time.Time{arrival.Add(-time.Second), arrival}}, Log: slogOf(e.logs),
		Fees: paymentspg.NewRepo(e.db.App), Notify: notificationspg.NewNotifier(e.clk),
	})
	v, err := svc.Cancel(t.Context(), d.UserID, uuid.MustParse(id))
	if err != nil {
		t.Fatal(err)
	}
	if v.RefundReason == nil || *v.RefundReason != "none_after_arrival" || v.Refund != nil {
		t.Errorf("refund_reason = %v refund = %v, want none_after_arrival and no refund", v.RefundReason, v.Refund)
	}
	if v.CancelledAt == nil || !v.CancelledAt.Equal(arrival) {
		t.Errorf("cancelled_at = %v, want %v (the time under the locks)", v.CancelledAt, arrival)
	}
	if n := e.count(`select count(*) from refunds`); n != 0 {
		t.Errorf("refund rows = %d, want none", n)
	}
}
