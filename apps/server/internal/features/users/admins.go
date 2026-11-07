package users

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/XYZLZ/MyPark/apps/server/internal/domain/apperr"
	"github.com/XYZLZ/MyPark/apps/server/internal/domain/authz"
	"github.com/XYZLZ/MyPark/apps/server/internal/domain/errcodes"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/auth"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/validation"
)

// Page sizes of the admin list (spec 6.1).
const (
	DefaultAdminPageSize = 50
	MaxAdminPageSize     = 100
)

// CreateAdminInput is an admin invitation. Strings must already be trimmed
// and validated. Locale is empty for the default.
type CreateAdminInput struct {
	Email, FirstName, LastName, Locale string
}

// CreateAdmin invites an admin on behalf of p (OC-19, OC-39). The bool is
// true when the invitation is new (201) and false when it was re-issued to a
// pending invitee (200). The role comes from the use case, never the client.
func (s *Service) CreateAdmin(ctx context.Context, p authz.Principal, in CreateAdminInput) (AdminUser, bool, error) {
	if !p.Is(authz.RoleAdmin) {
		return AdminUser{}, false, apperr.Forbidden(errcodes.AuthForbidden)
	}
	res, err := s.d.Accounts.CreateInvitedAdmin(ctx, p, auth.InviteInput{
		Email: in.Email, FirstName: in.FirstName, LastName: in.LastName, Locale: in.Locale,
	})
	if err != nil {
		return AdminUser{}, false, err
	}
	v := res.User
	return AdminUser{
		PublicID: v.PublicID, Email: v.Email, FirstName: v.FirstName, LastName: v.LastName,
		Status: v.Status, Locale: v.Locale, CreatedAt: v.CreatedAt,
	}, res.Created, nil
}

// ListAdmins returns one page of admins, newest first, and the cursor of the
// next page, or nil on the last one. A limit outside 1..MaxAdminPageSize or a
// cursor this service did not issue is a 422 validation.failed.
func (s *Service) ListAdmins(ctx context.Context, p authz.Principal, limit int, cursor string) ([]AdminUser, *string, error) {
	if !p.Is(authz.RoleAdmin) {
		return nil, nil, apperr.Forbidden(errcodes.AuthForbidden)
	}
	if limit < 1 {
		return nil, nil, fieldError("limit", "min", "1")
	}
	if limit > MaxAdminPageSize {
		return nil, nil, fieldError("limit", "max", fmt.Sprint(MaxAdminPageSize))
	}
	var after *AdminCursor
	if cursor != "" {
		c, err := decodeCursor(cursor)
		if err != nil {
			return nil, nil, fieldError("cursor", "cursor", "")
		}
		after = &c
	}
	// One extra row tells whether another page follows.
	rows, err := s.d.Repo.ListAdmins(ctx, limit+1, after)
	if err != nil {
		return nil, nil, err
	}
	if len(rows) <= limit {
		return rows, nil, nil
	}
	rows = rows[:limit]
	next := encodeCursor(AdminCursor{At: rows[limit-1].CreatedAt, ID: rows[limit-1].ID})
	return rows, &next, nil
}

// cursorWire is the opaque cursor: base64url of {"t":<RFC3339Nano>,"id":<int>}.
type cursorWire struct {
	T  string `json:"t"`
	ID int64  `json:"id"`
}

func encodeCursor(c AdminCursor) string {
	raw, _ := json.Marshal(cursorWire{T: c.At.UTC().Format(time.RFC3339Nano), ID: c.ID})
	return base64.RawURLEncoding.EncodeToString(raw)
}

var errBadCursor = errors.New("users: malformed cursor")

func decodeCursor(s string) (AdminCursor, error) {
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return AdminCursor{}, errBadCursor
	}
	var w cursorWire
	if err := json.Unmarshal(raw, &w); err != nil || w.ID < 1 {
		return AdminCursor{}, errBadCursor
	}
	at, err := time.Parse(time.RFC3339Nano, w.T)
	if err != nil {
		return AdminCursor{}, errBadCursor
	}
	return AdminCursor{At: at, ID: w.ID}, nil
}

// fieldError is a 422 validation.failed for one query parameter. The value
// is never echoed.
func fieldError(field, rule, param string) error {
	verr := &validation.Error{Fields: []validation.FieldError{{Field: field, Rule: rule, Param: param}}}
	return apperr.Invalid(errcodes.ValidationFailed).
		WithDetails(map[string]any{"fields": verr.Fields}).Wrap(verr)
}
