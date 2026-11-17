package payments

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/XYZLZ/MyPark/apps/server/internal/domain/apperr"
	"github.com/XYZLZ/MyPark/apps/server/internal/domain/errcodes"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/gate"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/database"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/keyset"
)

// Receipt is one payment of a driver as design 7.10 shows it. It carries brand
// and last four only: no provider reference, no card data (FR-PAY-9).
type Receipt struct {
	// RowID is the internal id; it never leaves the service.
	RowID         int64
	ID            uuid.UUID
	ReceiptNo     string // "" unless succeeded or refunded
	Kind          string
	Method        string
	AmountCents   int64
	Status        string
	CardBrand     string
	CardLast4     string
	FailureCode   string
	ReservationID *uuid.UUID
	CreatedAt     time.Time
	SucceededAt   *time.Time
	RefundedAt    *time.Time
	Refund        *Refund
}

// ReceiptCursor is the keyset position: the last item of a page.
type ReceiptCursor struct {
	CreatedAt time.Time
	PublicID  uuid.UUID
}

// ReceiptFilter selects receipts. Limit counts rows; the service asks for one
// more than the page to learn whether a next page exists.
type ReceiptFilter struct {
	ID    *uuid.UUID
	After *ReceiptCursor
	Limit int
}

// Receipts is the storage port of the receipt reads. Every method runs on the
// transaction carried by the context.
type Receipts interface {
	// SetTxLimits applies the driver-API lock and statement timeouts (M3B-55).
	SetTxLimits(ctx context.Context) error
	// Find returns the user's payments, newest first, narrowed by f.
	Find(ctx context.Context, userID int64, f ReceiptFilter) ([]Receipt, error)
}

// ReceiptService reads a driver's receipts.
type ReceiptService struct {
	tx   database.TxRunnerPort
	repo Receipts
}

// NewReceiptService returns a ReceiptService.
func NewReceiptService(tx database.TxRunnerPort, repo Receipts) *ReceiptService {
	return &ReceiptService{tx: tx, repo: repo}
}

// receiptNo is set only for a payment that went through (M2C-34, M3B-37).
func receiptNo(r Receipt) string {
	if r.Status != StatusSucceeded && r.Status != StatusRefunded {
		return ""
	}
	return gate.ReceiptNo(r.RowID)
}

// List returns one page of the driver's receipts, newest first, and the cursor
// of the next page ("" on the last one). A limit or cursor out of range is a
// 422 validation.failed.
func (s *ReceiptService) List(ctx context.Context, userID int64, limit int, cursor string) ([]Receipt, string, error) {
	if err := keyset.CheckLimit(limit); err != nil {
		return nil, "", err
	}
	f := ReceiptFilter{Limit: limit + 1}
	if cursor != "" {
		at, id, err := keyset.DecodeField(cursor)
		if err != nil {
			return nil, "", err
		}
		f.After = &ReceiptCursor{CreatedAt: at, PublicID: id}
	}
	var recs []Receipt
	err := s.tx.InTx(ctx, func(ctx context.Context) error {
		if err := s.repo.SetTxLimits(ctx); err != nil {
			return err
		}
		var err error
		recs, err = s.repo.Find(ctx, userID, f)
		return err
	})
	if err != nil {
		return nil, "", err
	}
	next := ""
	if len(recs) > limit {
		recs = recs[:limit]
		last := recs[limit-1]
		next = keyset.Encode(last.CreatedAt, last.ID)
	}
	for i := range recs {
		recs[i].ReceiptNo = receiptNo(recs[i])
	}
	return recs, next, nil
}

// Get returns one of the driver's receipts. An unknown id and another driver's
// id are the same 404 (M3B-33).
func (s *ReceiptService) Get(ctx context.Context, userID int64, id uuid.UUID) (Receipt, error) {
	var out Receipt
	err := s.tx.InTx(ctx, func(ctx context.Context) error {
		if err := s.repo.SetTxLimits(ctx); err != nil {
			return err
		}
		recs, err := s.repo.Find(ctx, userID, ReceiptFilter{ID: &id, Limit: 1})
		if err != nil {
			return err
		}
		if len(recs) == 0 {
			return apperr.NotFound(errcodes.PaymentNotFound)
		}
		out = recs[0]
		return nil
	})
	out.ReceiptNo = receiptNo(out)
	return out, err
}
