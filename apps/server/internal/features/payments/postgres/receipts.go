package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/XYZLZ/MyPark/apps/server/internal/features/payments"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/database"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/database/dbgen"
)

// txLimits bounds one driver-API transaction (M3B-55): a timeout rolls back
// and answers 500 internal.unexpected.
const txLimits = `select set_config('statement_timeout', '2s', true), set_config('lock_timeout', '1s', true)`

// ReceiptRepo implements payments.Receipts over dbgen. It works inside the
// transaction carried by the context, or on the pool when there is none.
type ReceiptRepo struct{ pool *pgxpool.Pool }

var _ payments.Receipts = (*ReceiptRepo)(nil)

// NewReceiptRepo returns a ReceiptRepo.
func NewReceiptRepo(pool *pgxpool.Pool) *ReceiptRepo { return &ReceiptRepo{pool: pool} }

// SetTxLimits implements payments.Receipts.
func (r *ReceiptRepo) SetTxLimits(ctx context.Context) error {
	tx, err := database.MustTx(ctx)
	if err != nil {
		return fmt.Errorf("payments: transaction limits: %w", err)
	}
	if _, err := tx.Exec(ctx, txLimits); err != nil {
		return fmt.Errorf("payments: set transaction limits: %w", err)
	}
	return nil
}

// Find implements payments.Receipts.
func (r *ReceiptRepo) Find(ctx context.Context, userID int64, f payments.ReceiptFilter) ([]payments.Receipt, error) {
	p := dbgen.ListReceiptsForUserParams{UserID: int8v(userID), RowLimit: int32(f.Limit)}
	if f.ID != nil {
		p.HasID, p.PublicID = true, *f.ID
	}
	if f.After != nil {
		p.HasCursor, p.CursorCreated, p.CursorPublicID = true, ts(f.After.CreatedAt), f.After.PublicID
	}
	rows, err := dbgen.New(database.Conn(ctx, r.pool)).ListReceiptsForUser(ctx, p)
	if err != nil {
		return nil, fmt.Errorf("payments: list receipts: %w", err)
	}
	out := make([]payments.Receipt, len(rows))
	for i, row := range rows {
		rc := payments.Receipt{
			RowID: row.ID, ID: row.PublicID, Kind: row.Kind, Method: row.Method, AmountCents: row.AmountCents,
			Status: row.Status, CardBrand: row.CardBrand.String, CardLast4: row.CardLast4.String,
			FailureCode: row.FailureCode.String, CreatedAt: row.CreatedAt.Time,
			SucceededAt: tsPtr(row.SucceededAt), RefundedAt: tsPtr(row.RefundedAt),
		}
		if row.ReservationPublicID.Valid {
			id := uuid.UUID(row.ReservationPublicID.Bytes)
			rc.ReservationID = &id
		}
		if row.RefundPublicID.Valid {
			rc.Refund = &payments.Refund{
				PublicID: uuid.UUID(row.RefundPublicID.Bytes), AmountCents: row.RefundAmountCents.Int64,
				Reason: row.RefundReason.String, Status: row.RefundStatus.String, SucceededAt: tsPtr(row.RefundSucceededAt),
			}
		}
		out[i] = rc
	}
	return out, nil
}

func tsPtr(t pgtype.Timestamptz) *time.Time {
	if !t.Valid {
		return nil
	}
	return &t.Time
}
