package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strconv"
	"strings"
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

// applyFixture is a reservation with a pending fee attempt, as the pay flow
// leaves it when the charge answer is applied later (reconcile, late success).
type applyFixture struct {
	svc       *reservations.Service
	rsvID     string
	rsvRow    int64
	paymentID int64
}

// pendingAttempt reserves for a fresh driver and records a pending fee
// attempt through the same port the pay flow uses.
func (e *feeEnv) pendingAttempt(arrival string) applyFixture {
	e.t.Helper()
	d := testsupport.SeedDrivers(e.t, e.db, 1)[0]
	f := applyFixture{rsvID: e.reserve(d, arrival)}
	tx := database.NewTxRunner(e.db.App)
	fees := paymentspg.NewRepo(e.db.App)
	f.svc = reservations.NewService(reservations.Deps{
		Tx: tx, Repo: reservationspg.NewRepo(e.db.App), Clock: e.clk, Log: slogOf(e.logs),
		Fees: fees, Notify: notificationspg.NewNotifier(e.clk), QRKey: e.cfg.QRHMACKey(),
	})
	var pub = f.rsvID
	if err := e.db.App.QueryRow(e.t.Context(), `select id from reservations where public_id = $1`, pub).Scan(&f.rsvRow); err != nil {
		e.t.Fatal(err)
	}
	err := tx.InTx(e.t.Context(), func(ctx context.Context) error {
		a, _, err := fees.StartAttempt(ctx, payments.AttemptInput{
			ReservationID: f.rsvRow, UserID: d.UserID, AmountCents: 15000, ClientKey: "key-apply-0001",
			Fingerprint: make([]byte, 32), Brand: "visa", Last4: "4242", Now: e.clk.Now(),
		})
		f.paymentID = a.ID
		return err
	})
	if err != nil {
		e.t.Fatalf("start attempt: %v", err)
	}
	return f
}

var succeeded = payments.ChargeResult{Outcome: payments.ChargeSucceeded, ProviderRef: "sim_ch_TESTREF"}

func (e *feeEnv) version() int64 {
	e.t.Helper()
	var v int64
	if err := e.db.App.QueryRow(e.t.Context(), `select version from lot_occupancy where lot_id = $1`, e.lotID).Scan(&v); err != nil {
		e.t.Fatal(err)
	}
	return v
}

// M3B-10: a charge that succeeds after the hold window closed expires the
// reservation and refunds the fee in full; it never bumps the lot version.
func TestPay_LateSuccessExpiresAndRefunds(t *testing.T) {
	e := newFeeEnv(t)
	f := e.pendingAttempt(arrival1502)
	before := e.version()
	e.clk.Set(time.Date(2026, 12, 1, 15, 25, 0, 0, time.UTC)) // upper(window)

	if err := f.svc.Apply(t.Context(), f.paymentID, succeeded); err != nil {
		t.Fatal(err)
	}
	var status, reason string
	if err := e.db.App.QueryRow(t.Context(), `select status, refund_reason from reservations where id = $1`, f.rsvRow).Scan(&status, &reason); err != nil {
		t.Fatal(err)
	}
	if status != "expired_unpaid" || reason != "facility_unavailable" {
		t.Errorf("reservation = %s/%s, want expired_unpaid/facility_unavailable", status, reason)
	}
	if n := e.count(`select count(*) from payments where id = $1 and status = 'succeeded' and provider_ref = 'sim_ch_TESTREF'`, f.paymentID); n != 1 {
		t.Error("payment must be succeeded with its provider ref")
	}
	if n := e.count(`select count(*) from refunds where payment_id = $1 and status = 'pending' and amount_cents = 15000
	                 and reason = 'facility_unavailable' and idempotency_key = 'refund:' || public_id`, f.paymentID); n != 1 {
		t.Error("want exactly one pending full refund, facility_unavailable")
	}
	if n := e.count(`select count(*) from reservation_slot_loads where held > 0`); n != 0 {
		t.Errorf("slots still held: %d rows", n)
	}
	if n := e.count(`select count(*) from outbox_messages where template = $1 and dedupe_key = $2`,
		string(notifications.TplReservationCancelled), "reservation_cancelled:"+f.rsvID); n != 1 {
		t.Errorf("reservation_cancelled messages = %d, want 1", n)
	}
	if n := e.count(`select count(*) from outbox_messages where template = $1`, string(notifications.TplReservationConfirmed)); n != 0 {
		t.Error("a late success must not send the confirmation")
	}
	if got := e.version(); got != before {
		t.Errorf("version = %d, want unchanged %d", got, before)
	}
	if !strings.Contains(e.logs.String(), "level=WARN") {
		t.Error("late success must be logged at warn")
	}
	// Applying the same result again changes nothing.
	if err := f.svc.Apply(t.Context(), f.paymentID, succeeded); err != nil {
		t.Fatal(err)
	}
	if n := e.count(`select count(*) from refunds`); n != 1 {
		t.Errorf("refunds after a repeated Apply = %d, want 1", n)
	}
	testsupport.AssertSlotInvariant(t, e.db.App, e.lotID)
}

// M3B-10 step 5: a success on a reservation that cannot be confirmed is kept
// as a succeeded payment with a refund queued, and logged at error.
func TestPay_DefensiveSuccessRefunds(t *testing.T) {
	e := newFeeEnv(t)
	f := e.pendingAttempt(arrival1502)
	// Cancel by hand, releasing the slots like the cancel flow does.
	if _, err := e.db.Super.Exec(t.Context(),
		`update reservations set status = 'cancelled', cancelled_at = $2, cancelled_by = 'driver' where id = $1`, f.rsvRow, rsvNow); err != nil {
		t.Fatal(err)
	}
	if _, err := e.db.Super.Exec(t.Context(), `update reservation_slot_loads set held = held - 1 where held > 0`); err != nil {
		t.Fatal(err)
	}

	if err := f.svc.Apply(t.Context(), f.paymentID, succeeded); err != nil {
		t.Fatal(err)
	}
	if got := e.status(f.rsvID); got != "cancelled" {
		t.Errorf("reservation = %s, want cancelled (unchanged)", got)
	}
	if n := e.count(`select count(*) from payments where id = $1 and status = 'succeeded'`, f.paymentID); n != 1 {
		t.Error("payment must be succeeded")
	}
	if n := e.count(`select count(*) from refunds where payment_id = $1 and status = 'pending' and amount_cents = 15000`, f.paymentID); n != 1 {
		t.Error("want one pending full refund")
	}
	if !strings.Contains(e.logs.String(), "level=ERROR") {
		t.Error("the defensive branch must be logged at error")
	}
	testsupport.AssertSlotInvariant(t, e.db.App, e.lotID)
}

func TestApply_DeclineFailsThePaymentOnly(t *testing.T) {
	e := newFeeEnv(t)
	f := e.pendingAttempt(arrival1502)
	res := payments.ChargeResult{Outcome: payments.ChargeDeclined, DeclineCode: "card_declined"}
	for range 2 { // idempotent
		if err := f.svc.Apply(t.Context(), f.paymentID, res); err != nil {
			t.Fatal(err)
		}
	}
	if n := e.count(`select count(*) from payments where id = $1 and status = 'failed' and failure_code = 'card_declined' and failed_at is not null`, f.paymentID); n != 1 {
		t.Error("payment must be failed with its code")
	}
	if got := e.status(f.rsvID); got != "pending_payment" {
		t.Errorf("reservation = %s, want pending_payment", got)
	}
	testsupport.AssertSlotInvariant(t, e.db.App, e.lotID)
}

func slogOf(w *testsupport.SyncBuffer) *slog.Logger { return slog.New(slog.NewTextHandler(w, nil)) }

// F2: a success that meets an already succeeded or refunded payment is a quiet
// no-op; one that meets a failed payment is loud (error log, public ids only) and returns
// payments.ErrSuccessAfterFinal.
func TestApply_SuccessMeetingAFinalPayment(t *testing.T) {
	t.Run("already succeeded is quiet", func(t *testing.T) {
		e := newFeeEnv(t)
		f := e.pendingAttempt(arrival1502)
		for range 2 {
			if err := f.svc.Apply(t.Context(), f.paymentID, succeeded); err != nil {
				t.Fatalf("apply = %v, want nil", err)
			}
		}
		if strings.Contains(e.logs.String(), "level=ERROR") {
			t.Errorf("a repeated success logged an error: %s", e.logs.String())
		}
		if n := e.count(`select count(*) from refunds`); n != 0 {
			t.Errorf("refunds = %d, want 0", n)
		}
		if got := e.status(f.rsvID); got != "confirmed" {
			t.Errorf("status = %s, want confirmed", got)
		}
	})
	t.Run("already refunded is quiet", func(t *testing.T) {
		e := newFeeEnv(t)
		f := e.pendingAttempt(arrival1502)
		if err := f.svc.Apply(t.Context(), f.paymentID, succeeded); err != nil {
			t.Fatal(err)
		}
		if _, err := e.db.Super.Exec(t.Context(),
			`update payments set status = 'refunded', refunded_at = $2 where id = $1`, f.paymentID, e.clk.Now()); err != nil {
			t.Fatal(err)
		}
		if err := f.svc.Apply(t.Context(), f.paymentID, succeeded); err != nil {
			t.Fatalf("apply = %v, want nil", err)
		}
		if strings.Contains(e.logs.String(), "level=ERROR") {
			t.Errorf("a success on a refunded payment logged an error: %s", e.logs.String())
		}
		if n := e.count(`select count(*) from payments where id = $1 and status = 'refunded'`, f.paymentID); n != 1 {
			t.Error("the refunded payment must stay refunded")
		}
	})
	t.Run("already failed is loud", func(t *testing.T) {
		e := newFeeEnv(t)
		f := e.pendingAttempt(arrival1502)
		declined := payments.ChargeResult{Outcome: payments.ChargeDeclined, DeclineCode: "card_declined"}
		if err := f.svc.Apply(t.Context(), f.paymentID, declined); err != nil {
			t.Fatal(err)
		}
		err := f.svc.Apply(t.Context(), f.paymentID, succeeded)
		if !errors.Is(err, payments.ErrSuccessAfterFinal) {
			t.Fatalf("apply = %v, want ErrSuccessAfterFinal", err)
		}
		logs := e.logs.String()
		if !strings.Contains(logs, "level=ERROR") || !strings.Contains(logs, f.rsvID) {
			t.Errorf("want an error log naming the reservation, got %q", logs)
		}
		if strings.Contains(logs, "sim_ch_TESTREF") {
			t.Error("the log must not carry the provider ref")
		}
		if got := e.status(f.rsvID); got != "pending_payment" {
			t.Errorf("status = %s, want pending_payment (unchanged)", got)
		}
	})
}

// The confirmation email carries what its template shows (design 12.6), in lot time.
func TestApply_ConfirmEmailPayload(t *testing.T) {
	e := newFeeEnv(t)
	f := e.pendingAttempt(arrival1502)
	if err := f.svc.Apply(t.Context(), f.paymentID, succeeded); err != nil {
		t.Fatal(err)
	}
	var plate string
	var fee int64
	if err := e.db.App.QueryRow(t.Context(), `select v.plate_display, r.fee_cents from reservations r
		join vehicles v on v.id = r.vehicle_id where r.id = $1`, f.rsvRow).Scan(&plate, &fee); err != nil {
		t.Fatal(err)
	}
	var got map[string]string
	var raw []byte
	if err := e.db.Super.QueryRow(t.Context(), `select payload from outbox_messages where dedupe_key = $1`,
		"reservation_confirmed:"+f.rsvID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"reservation_id": f.rsvID, "arrival_at": "2026-12-01 11:05", "window": "2026-12-01 10:50 - 2026-12-01 11:25",
		"plate_display": plate, "fee_cents": strconv.FormatInt(fee, 10),
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("payload[%s] = %q, want %q", k, got[k], v)
		}
	}
	if got["qr_token"] == "" || len(got) != len(want)+1 {
		t.Errorf("payload = %v, want the five keys plus qr_token", got)
	}
}
