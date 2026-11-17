package main

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/XYZLZ/MyPark/apps/server/internal/features/notifications"
	notificationspg "github.com/XYZLZ/MyPark/apps/server/internal/features/notifications/postgres"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/payments"
	paymentspg "github.com/XYZLZ/MyPark/apps/server/internal/features/payments/postgres"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/reservations"
	reservationspg "github.com/XYZLZ/MyPark/apps/server/internal/features/reservations/postgres"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/database"
	"github.com/XYZLZ/MyPark/apps/server/internal/testsupport"
)

const timeoutCard = "4000000000009995"

// reconciler is the production reconcile job over the test database, applying
// through its own reservations service (as cmd/worker does).
func (e *feeEnv) reconciler() (*payments.Reconciler, *reservations.Service) {
	e.t.Helper()
	tx := database.NewTxRunner(e.db.App)
	fees := paymentspg.NewRepo(e.db.App)
	svc := reservations.NewService(reservations.Deps{
		Tx: tx, Repo: reservationspg.NewRepo(e.db.App), Clock: e.clk, Log: slogOf(e.logs),
		Fees: fees, Notify: notificationspg.NewNotifier(e.clk), QRKey: e.cfg.QRHMACKey(),
	})
	return &payments.Reconciler{
		Tx: tx, Fees: fees, Gateway: e.gw, Outcomes: svc, ChargeTimeout: e.cfg.PaymentChargeTimeout(), Log: slogOf(e.logs),
	}, svc
}

// payTimeout reserves, then pays with the timeout card: the charge is recorded
// at the gateway but its answer never arrives, so the attempt stays pending.
func (e *feeEnv) payTimeout() (id string, tok string) {
	e.t.Helper()
	d := testsupport.SeedDrivers(e.t, e.db, 1)[0]
	id, tok = e.reserve(d, arrival1502), e.token(d)
	code, body, hdr := e.pay(tok, id, "key-timeout-0001", cardWith(timeoutCard))
	if code != http.StatusAccepted {
		e.t.Fatalf("pay with the timeout card = %d %v, want 202", code, body)
	}
	if hdr.Get("Retry-After") != "2" {
		e.t.Errorf("Retry-After = %q, want 2", hdr.Get("Retry-After"))
	}
	return id, tok
}

func TestPay_TimeoutCardReturns202(t *testing.T) {
	e := newFeeEnv(t)
	d := testsupport.SeedDrivers(t, e.db, 1)[0]
	id, tok := e.reserve(d, arrival1502), e.token(d)

	code, body, hdr := e.pay(tok, id, "key-timeout-0001", cardWith(timeoutCard))
	if code != http.StatusAccepted || hdr.Get("Retry-After") != "2" {
		t.Fatalf("pay = %d retry-after %q %v, want 202 and 2", code, hdr.Get("Retry-After"), body)
	}
	if field(body, "data.reservation.status") != "pending_payment" || field(body, "data.payment.status") != "pending" || field(body, "data.payment.id") == nil {
		t.Errorf("body = %v, want a pending_payment reservation and a pending payment with an id", body)
	}
	rec := e.serve(jsonReq(http.MethodGet, "/v1/reservations/"+id, nil), tok)
	got := decodeBody(t, rec)
	if field(got, "data.latest_payment.status") != "pending" || field(got, "data.status") != "pending_payment" {
		t.Errorf("GET reservation = %v, want latest_payment pending", got)
	}
}

func TestReconcile_ConfirmsTimeoutCard(t *testing.T) {
	e := newFeeEnv(t)
	id, _ := e.payTimeout()
	rec, _ := e.reconciler()
	e.clk.Advance(61 * time.Second)

	n, err := rec.Run(t.Context(), e.clk.Now())
	if err != nil || n != 1 {
		t.Fatalf("Run = %d, %v; want 1", n, err)
	}
	if got := e.status(id); got != "confirmed" {
		t.Errorf("reservation = %s, want confirmed", got)
	}
	if c := e.count(`select count(*) from payments p join reservations r on r.id = p.reservation_id
		where r.public_id = $1 and p.status = 'succeeded' and p.provider_ref is not null`, id); c != 1 {
		t.Error("the payment must be succeeded with its provider ref")
	}
	if c := e.count(`select count(*) from outbox_messages where dedupe_key = $1`, "reservation_confirmed:"+id); c != 1 {
		t.Errorf("confirmation emails = %d, want 1", c)
	}
	if n, err := rec.Run(t.Context(), e.clk.Now()); err != nil || n != 0 {
		t.Errorf("second Run = %d, %v; want 0", n, err)
	}
	testsupport.AssertSlotInvariant(t, e.db.App, e.lotID)
}

func TestReconcile_SkipsYoungPending(t *testing.T) {
	e := newFeeEnv(t)
	id, _ := e.payTimeout()
	rec, _ := e.reconciler()
	charges := e.gw.charges.Load()
	e.clk.Advance(5 * time.Second) // ChargeTimeout 5 s: still inside timeout + 10 s

	if n, err := rec.Run(t.Context(), e.clk.Now()); err != nil || n != 0 {
		t.Fatalf("Run = %d, %v; want 0", n, err)
	}
	if e.gw.charges.Load() != charges {
		t.Error("a young attempt must not even be re-queried")
	}
	if got := e.status(id); got != "pending_payment" {
		t.Errorf("reservation = %s, want pending_payment", got)
	}
}

func TestReconcile_NotSubmitted(t *testing.T) {
	e := newFeeEnv(t)
	f := e.pendingAttempt(arrival1502) // a pending attempt the gateway never saw
	rec, _ := e.reconciler()

	// Younger than ChargeTimeout + 10 s: a charge may still be in flight.
	e.clk.Advance(14 * time.Second)
	if n, err := rec.Run(t.Context(), e.clk.Now()); err != nil || n != 0 {
		t.Fatalf("young Run = %d, %v; want 0", n, err)
	}
	e.clk.Advance(2 * time.Second)
	n, err := rec.Run(t.Context(), e.clk.Now())
	if err != nil || n != 1 {
		t.Fatalf("Run = %d, %v; want 1", n, err)
	}
	if c := e.count(`select count(*) from payments where id = $1 and status = 'failed' and failure_code = 'not_submitted' and failed_at = $2`,
		f.paymentID, e.clk.Now()); c != 1 {
		t.Error("payment must be failed/not_submitted with failed_at = now")
	}
	if got := e.status(f.rsvID); got != "pending_payment" {
		t.Errorf("reservation = %s, want pending_payment (the driver may pay again)", got)
	}
	testsupport.AssertSlotInvariant(t, e.db.App, e.lotID)
}

// The pay request's second transaction and the reconcile job both apply the
// same success: it is fenced on pending, so one wins and the other is a no-op.
func TestReconcile_RaceWithPayTx2(t *testing.T) {
	e := newFeeEnv(t)
	id, _ := e.payTimeout()
	rec, svc := e.reconciler()
	e.clk.Advance(61 * time.Second)
	var paymentID int64
	if err := e.db.App.QueryRow(t.Context(), `select p.id from payments p join reservations r on r.id = p.reservation_id where r.public_id = $1`, id).Scan(&paymentID); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	var applyErr, runErr error
	var ran int
	wg.Go(func() { applyErr = svc.Apply(context.WithoutCancel(t.Context()), paymentID, succeeded) })
	wg.Go(func() { ran, runErr = rec.Run(context.WithoutCancel(t.Context()), e.clk.Now()) })
	wg.Wait()
	if applyErr != nil || runErr != nil || ran != 1 {
		t.Fatalf("apply = %v, run = %d, %v; want both to succeed quietly", applyErr, ran, runErr)
	}
	if got := e.status(id); got != "confirmed" {
		t.Errorf("reservation = %s, want confirmed", got)
	}
	if c := e.count(`select count(*) from payments where id = $1 and status = 'succeeded'`, paymentID); c != 1 {
		t.Error("payment must be succeeded once")
	}
	if c := e.count(`select count(*) from outbox_messages where template = $1`, string(notifications.TplReservationConfirmed)); c != 1 {
		t.Errorf("confirmation emails = %d, want 1", c)
	}
	testsupport.AssertSlotInvariant(t, e.db.App, e.lotID)
}

// A charge the reconcile job resolves after the hold window closed is refunded.
func TestReconcile_LateResolution(t *testing.T) {
	e := newFeeEnv(t)
	id, _ := e.payTimeout()
	rec, _ := e.reconciler()
	e.clk.Set(time.Date(2026, 12, 1, 15, 25, 0, 0, time.UTC)) // upper(window)

	if n, err := rec.Run(t.Context(), e.clk.Now()); err != nil || n != 1 {
		t.Fatalf("Run = %d, %v; want 1", n, err)
	}
	if got := e.status(id); got != "expired_unpaid" {
		t.Errorf("reservation = %s, want expired_unpaid", got)
	}
	if c := e.count(`select count(*) from refunds f join payments p on p.id = f.payment_id join reservations r on r.id = p.reservation_id
		where r.public_id = $1 and f.status = 'pending' and f.reason = 'facility_unavailable'`, id); c != 1 {
		t.Error("want one pending facility_unavailable refund")
	}
	testsupport.AssertSlotInvariant(t, e.db.App, e.lotID)
}

// latest_payment is the newest attempt, with its failure code once it failed.
func TestGetReservation_LatestPaymentFailed(t *testing.T) {
	e := newFeeEnv(t)
	d := testsupport.SeedDrivers(t, e.db, 1)[0]
	id, tok := e.reserve(d, arrival1502), e.token(d)
	if code, body, _ := e.pay(tok, id, "key-declined-001", cardWith("4000000000000002")); code != http.StatusPaymentRequired {
		t.Fatalf("pay = %d %v, want 402", code, body)
	}
	got := decodeBody(t, e.serve(jsonReq(http.MethodGet, "/v1/reservations/"+id, nil), tok))
	if field(got, "data.latest_payment.status") != "failed" || field(got, "data.latest_payment.failure_code") != "card_declined" {
		t.Errorf("latest_payment = %v, want failed/card_declined", field(got, "data.latest_payment"))
	}
}
