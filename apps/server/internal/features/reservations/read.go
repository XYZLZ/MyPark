package reservations

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/XYZLZ/MyPark/apps/server/internal/domain/apperr"
	"github.com/XYZLZ/MyPark/apps/server/internal/domain/errcodes"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/validation"
)

// Page size bounds of the reservation list (design 7.5).
const (
	DefaultPageSize = 20
	MaxPageSize     = 50
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

// fieldError is a 422 validation.failed naming one field and rule.
func fieldError(field, rule, param string) error {
	verr := &validation.Error{Fields: []validation.FieldError{{Field: field, Rule: rule, Param: param}}}
	return apperr.Invalid(errcodes.ValidationFailed).
		WithDetails(map[string]any{"fields": verr.Fields}).Wrap(verr)
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
		return nil, "", fieldError("scope", "oneof", "active past")
	}
	if q.Limit < 1 {
		return nil, "", fieldError("limit", "min", "1")
	}
	if q.Limit > MaxPageSize {
		return nil, "", fieldError("limit", "max", fmt.Sprint(MaxPageSize))
	}
	f := ListFilter{Scope: q.Scope, Limit: q.Limit + 1}
	if q.Cursor != "" {
		c, err := decodeCursor(q.Cursor)
		if err != nil {
			return nil, "", fieldError("cursor", "cursor", "")
		}
		f.After = &c
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
		next = encodeCursor(Cursor{ArrivalAt: last.ArrivalAt, PublicID: last.ID})
	}
	out := make([]View, len(recs))
	for i, r := range recs {
		out[i] = r.View
	}
	return out, next, nil
}

var errBadCursor = errors.New("reservations: malformed cursor")

// encodeCursor is the opaque cursor: base64url of "<arrival_at>|<public_id>".
func encodeCursor(c Cursor) string {
	return base64.RawURLEncoding.EncodeToString([]byte(c.ArrivalAt.UTC().Format(time.RFC3339Nano) + "|" + c.PublicID.String()))
}

func decodeCursor(s string) (Cursor, error) {
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return Cursor{}, errBadCursor
	}
	at, id, ok := strings.Cut(string(raw), "|")
	if !ok {
		return Cursor{}, errBadCursor
	}
	t, err := time.Parse(time.RFC3339Nano, at)
	if err != nil {
		return Cursor{}, errBadCursor
	}
	pid, err := uuid.Parse(id)
	if err != nil {
		return Cursor{}, errBadCursor
	}
	return Cursor{ArrivalAt: t, PublicID: pid}, nil
}
