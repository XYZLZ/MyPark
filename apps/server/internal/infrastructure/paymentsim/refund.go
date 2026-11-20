package paymentsim

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/XYZLZ/MyPark/apps/server/internal/features/payments"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/database/dbgen"
)

// ErrRefundRejected means the charge cannot be refunded: it is unknown,
// declined, already refunded under another key, or the amount differs.
var ErrRefundRejected = errors.New("paymentsim: refund rejected")

var _ payments.Gateway = (*Gateway)(nil)

// Refund refunds a succeeded charge in full, once. A repeat with the same key
// returns the first provider ref.
func (g *Gateway) Refund(ctx context.Context, req payments.RefundRequest) (payments.RefundResult, error) {
	ref, err := g.ref("sim_rf_")
	if err != nil {
		return payments.RefundResult{}, err
	}
	got, err := dbgen.New(g.pool).RefundSimCharge(ctx, dbgen.RefundSimChargeParams{
		RefundKey:         pgtype.Text{String: req.IdempotencyKey, Valid: true},
		RefundProviderRef: pgtype.Text{String: ref, Valid: true},
		Now:               pgtype.Timestamptz{Time: g.clock.Now(), Valid: true},
		ChargeKey:         req.ChargeKey,
		AmountCents:       req.AmountCents,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return payments.RefundResult{}, ErrRefundRejected
	}
	if err != nil {
		return payments.RefundResult{}, fmt.Errorf("paymentsim: refund: %w", err)
	}
	return payments.RefundResult{ProviderRef: got}, nil
}
