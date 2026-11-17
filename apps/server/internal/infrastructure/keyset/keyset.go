// Package keyset is the shared plumbing of keyset-paginated lists: the opaque
// (time, public id) cursor and the 422 answers for a bad limit or cursor.
package keyset

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/XYZLZ/MyPark/apps/server/internal/domain/apperr"
	"github.com/XYZLZ/MyPark/apps/server/internal/domain/errcodes"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/validation"
)

// Page size bounds shared by the list endpoints (design 7.5, 7.10).
const (
	DefaultLimit = 20
	MaxLimit     = 50
)

// maxCursorLen bounds the decoded cursor: a real one is under 64 bytes.
const maxCursorLen = 128

// ErrBadCursor is returned for a cursor that does not decode.
var ErrBadCursor = errors.New("keyset: malformed cursor")

// FieldError is a 422 validation.failed naming one field and rule.
func FieldError(field, rule, param string) error {
	verr := &validation.Error{Fields: []validation.FieldError{{Field: field, Rule: rule, Param: param}}}
	return apperr.Invalid(errcodes.ValidationFailed).
		WithDetails(map[string]any{"fields": verr.Fields}).Wrap(verr)
}

// CheckLimit is nil for 1..MaxLimit, else a 422 naming the bound.
func CheckLimit(limit int) error {
	if limit < 1 {
		return FieldError("limit", "min", "1")
	}
	if limit > MaxLimit {
		return FieldError("limit", "max", fmt.Sprint(MaxLimit))
	}
	return nil
}

// ParseLimit reads a raw limit query value: "" is DefaultLimit and a
// non-number is a 422. The bounds are checked by CheckLimit.
func ParseLimit(raw string) (int, error) {
	if raw == "" {
		return DefaultLimit, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, FieldError("limit", "numeric", "")
	}
	return n, nil
}

// Encode is the opaque cursor: base64url of "<time>|<public id>".
func Encode(t time.Time, id uuid.UUID) string {
	return base64.RawURLEncoding.EncodeToString([]byte(t.UTC().Format(time.RFC3339Nano) + "|" + id.String()))
}

// Decode reverses Encode. Any malformed or oversized input is ErrBadCursor.
func Decode(s string) (time.Time, uuid.UUID, error) {
	if base64.RawURLEncoding.DecodedLen(len(s)) > maxCursorLen {
		return time.Time{}, uuid.Nil, ErrBadCursor
	}
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return time.Time{}, uuid.Nil, ErrBadCursor
	}
	at, id, ok := strings.Cut(string(raw), "|")
	if !ok {
		return time.Time{}, uuid.Nil, ErrBadCursor
	}
	t, err := time.Parse(time.RFC3339Nano, at)
	if err != nil {
		return time.Time{}, uuid.Nil, ErrBadCursor
	}
	pid, err := uuid.Parse(id)
	if err != nil {
		return time.Time{}, uuid.Nil, ErrBadCursor
	}
	return t, pid, nil
}

// DecodeField is Decode with the 422 a list answers for a bad cursor.
func DecodeField(s string) (time.Time, uuid.UUID, error) {
	t, id, err := Decode(s)
	if err != nil {
		return time.Time{}, uuid.Nil, FieldError("cursor", "cursor", "")
	}
	return t, id, nil
}
