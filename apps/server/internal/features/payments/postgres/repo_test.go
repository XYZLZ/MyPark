package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/XYZLZ/MyPark/apps/server/internal/domain/apperr"
	"github.com/XYZLZ/MyPark/apps/server/internal/domain/errcodes"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/payments"
	paymentspg "github.com/XYZLZ/MyPark/apps/server/internal/features/payments/postgres"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/database"
	"github.com/XYZLZ/MyPark/apps/server/internal/testsupport"
)

var now = time.Date(2026, 12, 1, 13, 58, 10, 0, time.UTC)

type env struct {
	t    *testing.T
	db   *testsupport.TestDB
	tx   *database.TxRunner
	fees *paymentspg.Repo
	rsv  int64
	user int64
	pub  uuid.UUID
}

func newEnv(t *testing.T) *env {
	t.Helper()
	db := testsupport.NewTestDB(t)
	lot := testsupport.SeedPilotLot(t, db)
	d := testsupport.SeedDrivers(t, db, 1)
	arrival := now.Add(2 * time.Hour)
	testsupport.SeedReservations(t, db, lot, d, arrival, arrival.Add(-15*time.Minute), arrival.Add(20*time.Minute), now)
	e := &env{t: t, db: db, tx: database.NewTxRunner(db.App), fees: paymentspg.NewRepo(db.App), user: d[0].UserID}
	if err := db.App.QueryRow(t.Context(), `select id, public_id from reservations where user_id = $1`, e.user).Scan(&e.rsv, &e.pub); err != nil {
		t.Fatal(err)
	}
	return e
}

func (e *env) input(key string, fp byte) payments.AttemptInput {
	return payments.AttemptInput{
		ReservationID: e.rsv, UserID: e.user, ReservationPublicID: e.pub, AmountCents: 15000,
		ClientKey: key, Fingerprint: bytes32(fp), Brand: "visa", Last4: "4242", Now: now,
	}
}

func bytes32(b byte) []byte {
	out := make([]byte, 32)
	for i := range out {
		out[i] = b
	}
	return out
}

func appErrCode(err error) errcodes.Code {
	if ae, ok := errors.AsType[*apperr.Error](err); ok {
		return ae.Code
	}
	return ""
}

// A unique violation aborts the transaction; the insert's savepoint keeps it
// usable so the replay re-read works (M3B-7, M3B-8).
func TestStartAttempt_RacesAreMappedInsideOneTransaction(t *testing.T) {
	e := newEnv(t)
	err := e.tx.InTx(t.Context(), func(ctx context.Context) error {
		a, replayed, err := e.fees.StartAttempt(ctx, e.input("key-aaaa-1111", 1))
		if err != nil || replayed || a.Status != "pending" || a.GatewayKey != payments.GatewayKey(a.PublicID) {
			t.Fatalf("first start = %+v replayed=%v err=%v", a, replayed, err)
		}
		// The unique index fires on the same client key (the lock-free race).
		again, replayed, err := e.fees.StartAttempt(ctx, e.input("key-aaaa-1111", 1))
		if err != nil || !replayed || again.ID != a.ID {
			t.Errorf("same key = %+v replayed=%v err=%v, want the first attempt replayed", again, replayed, err)
		}
		if _, _, err := e.fees.StartAttempt(ctx, e.input("key-aaaa-1111", 2)); appErrCode(err) != errcodes.IdempotencyKeyReused {
			t.Errorf("same key, other fingerprint = %v, want idempotency.key_reused", err)
		}
		if _, _, err := e.fees.StartAttempt(ctx, e.input("key-bbbb-2222", 1)); appErrCode(err) != errcodes.PaymentInProgress {
			t.Errorf("other key while one is pending = %v, want payment.in_progress", err)
		}
		live, ok, err := e.fees.LiveFee(ctx, e.rsv)
		if err != nil || !ok || live.ID != a.ID {
			t.Errorf("live fee = %+v ok=%v err=%v", live, ok, err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestFinalize_IsFencedOnPending(t *testing.T) {
	e := newEnv(t)
	err := e.tx.InTx(t.Context(), func(ctx context.Context) error {
		a, _, err := e.fees.StartAttempt(ctx, e.input("key-aaaa-1111", 1))
		if err != nil {
			t.Fatal(err)
		}
		ok, err := e.fees.Finalize(ctx, a.ID, payments.ChargeResult{Outcome: payments.ChargeSucceeded, ProviderRef: "sim_ch_X"}, now)
		if err != nil || !ok {
			t.Fatalf("first finalize = %v %v", ok, err)
		}
		ok, err = e.fees.Finalize(ctx, a.ID, payments.ChargeResult{Outcome: payments.ChargeDeclined, DeclineCode: "card_declined"}, now)
		if err != nil || ok {
			t.Errorf("second finalize = %v %v, want not applied", ok, err)
		}
		f, err := e.fees.FeeByID(ctx, a.ID)
		if err != nil || f.Status != "succeeded" || f.ReservationID != e.rsv || f.AmountCents != 15000 {
			t.Errorf("fee = %+v %v", f, err)
		}
		r, err := e.fees.QueueRefund(ctx, a.ID, 15000, payments.RefundFacilityUnavailable, now)
		if err != nil || r.Status != "pending" || r.AmountCents != 15000 {
			t.Errorf("refund = %+v %v", r, err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestMarkNotSubmitted_AndLatestFee(t *testing.T) {
	e := newEnv(t)
	err := e.tx.InTx(t.Context(), func(ctx context.Context) error {
		a, _, err := e.fees.StartAttempt(ctx, e.input("key-aaaa-1111", 1))
		if err != nil {
			t.Fatal(err)
		}
		if ok, err := e.fees.MarkNotSubmitted(ctx, a.ID, now.Add(time.Second), now); err != nil || !ok {
			t.Fatalf("mark = %v %v", ok, err)
		}
		if _, ok, _ := e.fees.LiveFee(ctx, e.rsv); ok {
			t.Error("a failed attempt is not live")
		}
		b, _, err := e.fees.StartAttempt(ctx, e.input("key-bbbb-2222", 1))
		if err != nil {
			t.Fatalf("new attempt after a failed one: %v", err)
		}
		latest, ok, err := e.fees.LatestFee(ctx, e.rsv)
		if err != nil || !ok || latest.ID != b.ID {
			t.Errorf("latest = %+v ok=%v err=%v, want %d", latest, ok, err, b.ID)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// The guard holds even when the service is bypassed (M3B-8).
func TestOneLiveFeeByIndex(t *testing.T) {
	e := newEnv(t)
	err := e.tx.InTx(t.Context(), func(ctx context.Context) error {
		if _, _, err := e.fees.StartAttempt(ctx, e.input("key-aaaa-1111", 1)); err != nil {
			t.Fatal(err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = e.db.App.Exec(t.Context(),
		`insert into payments (kind, method, amount_cents, status, idempotency_key, reservation_id, user_id,
		                       client_key, request_fingerprint, card_brand, card_last4)
		 values ('reservation_fee', 'gateway_sim', 15000, 'pending', 'rsv-fee:direct', $1, $2,
		         'key-direct-3', $3, 'visa', '4242')`, e.rsv, e.user, bytes32(9))
	if pgErr, ok := errors.AsType[*pgconn.PgError](err); !ok || pgErr.Code != "23505" || pgErr.ConstraintName != "payments_reservation_live_key" {
		t.Errorf("second pending fee = %v, want 23505 payments_reservation_live_key", err)
	}
}

// M3B-10: MarkNotSubmitted acts only on an attempt created before the cutoff,
// and it needs the transaction that holds the reservation lock.
func TestMarkNotSubmitted_OnlyBeforeCutoffAndInATx(t *testing.T) {
	e := newEnv(t)
	var id int64
	if err := e.tx.InTx(t.Context(), func(ctx context.Context) error {
		a, _, err := e.fees.StartAttempt(ctx, e.input("key-aaaa-1111", 1))
		id = a.ID
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.fees.MarkNotSubmitted(t.Context(), id, now.Add(time.Hour), now); !errors.Is(err, database.ErrNoTx) {
		t.Errorf("outside a transaction = %v, want ErrNoTx", err)
	}
	if err := e.tx.InTx(t.Context(), func(ctx context.Context) error {
		if ok, err := e.fees.MarkNotSubmitted(ctx, id, now, now); err != nil || ok { // created_at == cutoff: not older
			t.Errorf("attempt created at the cutoff: ok=%v err=%v, want left alone", ok, err)
		}
		if ok, err := e.fees.MarkNotSubmitted(ctx, id, now.Add(time.Second), now.Add(time.Minute)); err != nil || !ok {
			t.Errorf("older attempt: ok=%v err=%v, want marked", ok, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	var status, code string
	var failedAt time.Time
	if err := e.db.App.QueryRow(t.Context(), `select status, failure_code, failed_at from payments where id = $1`, id).Scan(&status, &code, &failedAt); err != nil {
		t.Fatal(err)
	}
	if status != "failed" || code != "not_submitted" || !failedAt.Equal(now.Add(time.Minute)) {
		t.Errorf("payment = %s %s %v, want failed not_submitted at the given now", status, code, failedAt)
	}
}

func TestPendingFeesBefore(t *testing.T) {
	e := newEnv(t)
	if err := e.tx.InTx(t.Context(), func(ctx context.Context) error {
		_, _, err := e.fees.StartAttempt(ctx, e.input("key-aaaa-1111", 1))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	for name, tc := range map[string]struct {
		cutoff time.Time
		want   int
	}{"created at the cutoff": {now, 0}, "before the cutoff": {now.Add(time.Second), 1}} {
		got, err := e.fees.PendingFeesBefore(t.Context(), tc.cutoff, 20)
		if err != nil || len(got) != tc.want {
			t.Errorf("%s: %d rows, %v; want %d", name, len(got), err, tc.want)
		}
	}
	got, _ := e.fees.PendingFeesBefore(t.Context(), now.Add(time.Second), 20)
	if got[0].GatewayKey == "" || got[0].AmountCents != 15000 || got[0].ReservationID != e.rsv || got[0].Status != "pending" {
		t.Errorf("row = %+v", got[0])
	}
}
