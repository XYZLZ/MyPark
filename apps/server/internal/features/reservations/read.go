package reservations

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/XYZLZ/MyPark/apps/server/internal/domain/apperr"
	"github.com/XYZLZ/MyPark/apps/server/internal/domain/errcodes"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/keyset"
)

// Scopes of the reservation list.
const (
	ScopeActive = "active"
	ScopePast   = "past"
)

// ListQuery is a list request. Scope is "", "active" or "past".
type ListQuery struct {
	Scope  string
	Limit  int
	Cursor string
}

// Get returns one of the driver's reservations. An unknown id and another
// driver's id are the same 404 (M3B-33). It runs in a transaction with the
// driver-API limits, like every reservation path (M3B-55).
func (s *Service) Get(ctx context.Context, userID int64, id uuid.UUID) (View, error) {
	var out View
	err := s.tx.InTx(ctx, func(ctx context.Context) error {
		if err := s.repo.SetTxLimits(ctx); err != nil {
			return err
		}
		rec, err := s.repo.Get(ctx, userID, id)
		if errors.Is(err, ErrNotFound) {
			return apperr.NotFound(errcodes.ReservationNotFound)
		}
		out = rec.View
		if err == nil {
			err = s.withLatestPayment(ctx, rec.RowID, &out)
		}
		return err
	})
	return out, err
}

// List returns one page of the driver's reservations, newest arrival first,
// and the cursor of the next page ("" on the last one). A scope, limit or
// cursor out of range is a 422 validation.failed.
func (s *Service) List(ctx context.Context, userID int64, q ListQuery) ([]View, string, error) {
	switch q.Scope {
	case "", ScopeActive, ScopePast:
	default:
		return nil, "", keyset.FieldError("scope", "oneof", "active past")
	}
	if err := keyset.CheckLimit(q.Limit); err != nil {
		return nil, "", err
	}
	f := ListFilter{Scope: q.Scope, Limit: q.Limit + 1}
	if q.Cursor != "" {
		at, id, err := keyset.DecodeField(q.Cursor)
		if err != nil {
			return nil, "", err
		}
		f.After = &Cursor{ArrivalAt: at, PublicID: id}
	}
	var recs []Record
	err := s.tx.InTx(ctx, func(ctx context.Context) error {
		if err := s.repo.SetTxLimits(ctx); err != nil {
			return err
		}
		var err error
		recs, err = s.repo.List(ctx, userID, f)
		for i := 0; err == nil && i < len(recs); i++ {
			err = s.withLatestPayment(ctx, recs[i].RowID, &recs[i].View)
		}
		return err
	})
	if err != nil {
		return nil, "", err
	}
	next := ""
	if len(recs) > q.Limit {
		recs = recs[:q.Limit]
		last := recs[q.Limit-1]
		next = keyset.Encode(last.ArrivalAt, last.ID)
	}
	out := make([]View, len(recs))
	for i, r := range recs {
		out[i] = r.View
	}
	return out, next, nil
}
