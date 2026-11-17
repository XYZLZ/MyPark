// Package postgres is the PostgreSQL adapter of the payments feature.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/XYZLZ/MyPark/apps/server/internal/domain/apperr"
	"github.com/XYZLZ/MyPark/apps/server/internal/domain/errcodes"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/payments"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/database"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/database/dbgen"
)

// Index names that decide the pay races (M3B-7, M3B-8).
const (
	clientKeyIndex = "payments_reservation_client_key"
	liveKeyIndex   = "payments_reservation_live_key"
)

// Repo implements payments.Fees over dbgen. It works inside the transaction
// carried by the context, or on the pool when there is none.
type Repo struct{ pool *pgxpool.Pool }

var _ payments.Fees = (*Repo)(nil)

// NewRepo returns a Repo over pool.
func NewRepo(pool *pgxpool.Pool) *Repo { return &Repo{pool: pool} }

func (r *Repo) q(ctx context.Context) *dbgen.Queries {
	return dbgen.New(database.Conn(ctx, r.pool))
}

func ts(t time.Time) pgtype.Timestamptz { return pgtype.Timestamptz{Time: t, Valid: true} }

func text(s string) pgtype.Text { return pgtype.Text{String: s, Valid: true} }

func int8v(n int64) pgtype.Int8 { return pgtype.Int8{Int64: n, Valid: true} }

// FindAttempt implements payments.Fees.
func (r *Repo) FindAttempt(ctx context.Context, reservationID int64, clientKey string, fingerprint []byte) (payments.Attempt, bool, error) {
	row, err := r.q(ctx).GetFeeAttemptByClientKey(ctx, dbgen.GetFeeAttemptByClientKeyParams{
		ReservationID: int8v(reservationID), ClientKey: text(clientKey),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return payments.Attempt{}, false, nil
	}
	if err != nil {
		return payments.Attempt{}, false, fmt.Errorf("payments: find attempt: %w", err)
	}
	if replay, code := payments.DecideReplay(row.RequestFingerprint, fingerprint); !replay {
		return payments.Attempt{}, false, apperr.Conflict(code)
	}
	return payments.Attempt{
		ID: row.ID, PublicID: row.PublicID, Status: row.Status, FailureCode: row.FailureCode, GatewayKey: row.IdempotencyKey,
	}, true, nil
}

// StartAttempt implements payments.Fees. The insert runs in a savepoint: a
// unique violation aborts the enclosing transaction, and the replay re-read
// below needs it alive.
func (r *Repo) StartAttempt(ctx context.Context, in payments.AttemptInput) (payments.Attempt, bool, error) {
	tx, err := database.MustTx(ctx)
	if err != nil {
		return payments.Attempt{}, false, fmt.Errorf("payments: start attempt: %w", err)
	}
	id, err := uuid.NewV7()
	if err != nil {
		return payments.Attempt{}, false, fmt.Errorf("payments: payment id: %w", err)
	}
	sp, err := tx.Begin(ctx)
	if err != nil {
		return payments.Attempt{}, false, fmt.Errorf("payments: savepoint: %w", err)
	}
	row, err := dbgen.New(sp).InsertFeeAttempt(ctx, dbgen.InsertFeeAttemptParams{
		PublicID: id, AmountCents: in.AmountCents, IdempotencyKey: payments.GatewayKey(id),
		ReservationID: int8v(in.ReservationID), UserID: int8v(in.UserID), ClientKey: text(in.ClientKey),
		RequestFingerprint: in.Fingerprint, CardBrand: text(in.Brand), CardLast4: text(in.Last4), Now: ts(in.Now),
	})
	if err != nil {
		_ = sp.Rollback(ctx)
		if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok && pgErr.Code == "23505" {
			// Either index may report first when both are violated, so the same
			// client key is looked up before concluding another attempt is live.
			if pgErr.ConstraintName == clientKeyIndex || pgErr.ConstraintName == liveKeyIndex {
				a, found, ferr := r.FindAttempt(ctx, in.ReservationID, in.ClientKey, in.Fingerprint)
				if ferr != nil {
					return payments.Attempt{}, false, ferr
				}
				if found {
					return a, true, nil
				}
			}
			if pgErr.ConstraintName == liveKeyIndex {
				return payments.Attempt{}, false, apperr.Conflict(errcodes.PaymentInProgress)
			}
		}
		return payments.Attempt{}, false, fmt.Errorf("payments: insert attempt: %w", err)
	}
	if err := sp.Commit(ctx); err != nil {
		return payments.Attempt{}, false, fmt.Errorf("payments: release savepoint: %w", err)
	}
	return payments.Attempt{
		ID: row.ID, PublicID: row.PublicID, Status: row.Status, FailureCode: row.FailureCode, GatewayKey: row.IdempotencyKey,
	}, false, nil
}

func fee(id int64, pub uuid.UUID, status, failure, key string, reservation pgtype.Int8, amount int64) payments.FeePayment {
	return payments.FeePayment{
		Attempt:       payments.Attempt{ID: id, PublicID: pub, Status: status, FailureCode: failure, GatewayKey: key},
		ReservationID: reservation.Int64, AmountCents: amount,
	}
}

// FeeByID implements payments.Fees.
func (r *Repo) FeeByID(ctx context.Context, paymentID int64) (payments.FeePayment, error) {
	row, err := r.q(ctx).GetFeeByID(ctx, paymentID)
	if err != nil {
		return payments.FeePayment{}, fmt.Errorf("payments: read fee %d: %w", paymentID, err)
	}
	return fee(row.ID, row.PublicID, row.Status, row.FailureCode, row.IdempotencyKey, row.ReservationID, row.AmountCents), nil
}

// Finalize implements payments.Fees.
func (r *Repo) Finalize(ctx context.Context, paymentID int64, res payments.ChargeResult, now time.Time) (bool, error) {
	var (
		n   int64
		err error
	)
	switch res.Outcome {
	case payments.ChargeSucceeded:
		n, err = r.q(ctx).FinalizeFeeSucceeded(ctx, dbgen.FinalizeFeeSucceededParams{
			ID: paymentID, Now: ts(now), ProviderRef: text(res.ProviderRef),
		})
	case payments.ChargeDeclined:
		n, err = r.q(ctx).FinalizeFeeFailed(ctx, dbgen.FinalizeFeeFailedParams{
			ID: paymentID, Now: ts(now), FailureCode: text(res.DeclineCode),
		})
	default:
		return false, fmt.Errorf("payments: finalize %d: unknown outcome %d", paymentID, res.Outcome)
	}
	if err != nil {
		return false, fmt.Errorf("payments: finalize %d: %w", paymentID, err)
	}
	return n == 1, nil
}

// MarkNotSubmitted implements payments.Fees.
func (r *Repo) MarkNotSubmitted(ctx context.Context, paymentID int64, now time.Time) (bool, error) {
	n, err := r.q(ctx).FinalizeFeeFailed(ctx, dbgen.FinalizeFeeFailedParams{
		ID: paymentID, Now: ts(now), FailureCode: text(payments.FailureNotSubmitted),
	})
	if err != nil {
		return false, fmt.Errorf("payments: mark %d not submitted: %w", paymentID, err)
	}
	return n == 1, nil
}

// LiveFee implements payments.Fees.
func (r *Repo) LiveFee(ctx context.Context, reservationID int64) (payments.FeePayment, bool, error) {
	row, err := r.q(ctx).GetLiveFee(ctx, int8v(reservationID))
	if errors.Is(err, pgx.ErrNoRows) {
		return payments.FeePayment{}, false, nil
	}
	if err != nil {
		return payments.FeePayment{}, false, fmt.Errorf("payments: live fee: %w", err)
	}
	return fee(row.ID, row.PublicID, row.Status, row.FailureCode, row.IdempotencyKey, row.ReservationID, row.AmountCents), true, nil
}

// LatestFee implements payments.Fees.
func (r *Repo) LatestFee(ctx context.Context, reservationID int64) (payments.FeePayment, bool, error) {
	row, err := r.q(ctx).GetLatestFee(ctx, int8v(reservationID))
	if errors.Is(err, pgx.ErrNoRows) {
		return payments.FeePayment{}, false, nil
	}
	if err != nil {
		return payments.FeePayment{}, false, fmt.Errorf("payments: latest fee: %w", err)
	}
	return fee(row.ID, row.PublicID, row.Status, row.FailureCode, row.IdempotencyKey, row.ReservationID, row.AmountCents), true, nil
}

// QueueRefund implements payments.Fees.
func (r *Repo) QueueRefund(ctx context.Context, paymentID, amountCents int64, reason string, now time.Time) (payments.Refund, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return payments.Refund{}, fmt.Errorf("payments: refund id: %w", err)
	}
	row, err := r.q(ctx).InsertRefund(ctx, dbgen.InsertRefundParams{
		PublicID: id, PaymentID: paymentID, AmountCents: amountCents, Reason: reason,
		IdempotencyKey: payments.RefundKey(id), Now: ts(now),
	})
	if err != nil {
		return payments.Refund{}, fmt.Errorf("payments: queue refund: %w", err)
	}
	return payments.Refund{
		ID: row.ID, PublicID: row.PublicID, PaymentID: row.PaymentID, AmountCents: row.AmountCents,
		Reason: row.Reason, Status: row.Status,
	}, nil
}
