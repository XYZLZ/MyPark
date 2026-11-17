package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/XYZLZ/MyPark/apps/server/internal/features/payments"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/database"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/database/dbgen"
)

// RefundRepo implements payments.Refunds over dbgen. It works inside the
// transaction carried by the context.
type RefundRepo struct{ pool *pgxpool.Pool }

var _ payments.Refunds = (*RefundRepo)(nil)

// NewRefundRepo returns a RefundRepo.
func NewRefundRepo(pool *pgxpool.Pool) *RefundRepo { return &RefundRepo{pool: pool} }

// Claim implements payments.Refunds.
func (r *RefundRepo) Claim(ctx context.Context, now, leaseUntil time.Time, limit int) ([]payments.RefundClaim, error) {
	tx, err := database.MustTx(ctx)
	if err != nil {
		return nil, fmt.Errorf("payments: claim refunds: %w", err)
	}
	rows, err := dbgen.New(tx).ClaimDueRefunds(ctx, dbgen.ClaimDueRefundsParams{Now: ts(now), LeaseUntil: ts(leaseUntil), Batch: int32(limit), MaxAttempts: payments.RefundMaxAttempts})
	if err != nil {
		return nil, fmt.Errorf("payments: claim refunds: %w", err)
	}
	out := make([]payments.RefundClaim, len(rows))
	for i, row := range rows {
		out[i] = payments.RefundClaim{
			ID: row.ID, PublicID: row.PublicID, PaymentID: row.PaymentID, AmountCents: row.AmountCents,
			Attempts: int(row.Attempts), GatewayKey: row.IdempotencyKey, ChargeKey: row.ChargeKey,
			PaymentPublicID: row.PaymentPublicID, ReservationPublicID: row.ReservationPublicID,
			ReservationID: row.ReservationID, LotID: row.LotID,
		}
	}
	return out, nil
}

// Succeed implements payments.Refunds. The payment is locked before the
// refund (design 8.1); a refund whose fence no longer matches changes nothing.
func (r *RefundRepo) Succeed(ctx context.Context, c payments.RefundClaim, providerRef string, now time.Time) (bool, error) {
	tx, err := database.MustTx(ctx)
	if err != nil {
		return false, fmt.Errorf("payments: finalize refund %d: %w", c.ID, err)
	}
	q := dbgen.New(tx)
	if _, err := q.LockPaymentForRefund(ctx, c.PaymentID); err != nil {
		return false, fmt.Errorf("payments: lock payment %d: %w", c.PaymentID, err)
	}
	n, err := q.FinalizeRefundSucceeded(ctx, dbgen.FinalizeRefundSucceededParams{
		ID: c.ID, Attempts: int16(c.Attempts), ProviderRef: text(providerRef), Now: ts(now),
	})
	if err != nil || n == 0 {
		return false, wrap(err, "finalize refund", c.ID)
	}
	if n, err = q.MarkPaymentRefunded(ctx, dbgen.MarkPaymentRefundedParams{ID: c.PaymentID, Now: ts(now)}); err != nil || n != 1 {
		if err == nil {
			err = fmt.Errorf("payment is not succeeded")
		}
		return false, wrap(err, "refund payment of", c.ID)
	}
	return true, nil
}

// Retry implements payments.Refunds.
func (r *RefundRepo) Retry(ctx context.Context, c payments.RefundClaim, lastError string, nextAttempt time.Time) (bool, error) {
	n, err := dbgen.New(database.Conn(ctx, r.pool)).FinalizeRefundRetry(ctx, dbgen.FinalizeRefundRetryParams{
		ID: c.ID, Attempts: int16(c.Attempts), LastError: text(lastError), NextAttemptAt: ts(nextAttempt),
	})
	return n == 1, wrap(err, "retry refund", c.ID)
}

// Fail implements payments.Refunds.
func (r *RefundRepo) Fail(ctx context.Context, c payments.RefundClaim, lastError string, now time.Time) (bool, error) {
	n, err := dbgen.New(database.Conn(ctx, r.pool)).FinalizeRefundFailed(ctx, dbgen.FinalizeRefundFailedParams{
		ID: c.ID, Attempts: int16(c.Attempts), LastError: text(lastError), Now: ts(now),
	})
	return n == 1, wrap(err, "fail refund", c.ID)
}

func wrap(err error, what string, id int64) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("payments: %s %d: %w", what, id, err)
}
