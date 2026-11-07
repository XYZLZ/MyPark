// Package apperr defines the application error type. A Kind classifies the
// failure and the transport layer maps it to a status code, so handlers and
// use cases never pick one. This package must not import net/http.
package apperr

import (
	"math"
	"time"

	"github.com/XYZLZ/MyPark/apps/server/internal/domain/errcodes"
)

// Kind classifies an application error.
type Kind int

// Kind values. They carry a Kind prefix because the shorthand constructors
// (Invalid, NotFound...) use the bare names.
const (
	KindInvalid Kind = iota + 1
	KindUnauthenticated
	KindForbidden
	KindNotFound
	KindConflict
	KindRateLimited
	KindInternal
	KindBadRequest
	KindTooLarge
	KindUnsupportedMedia
)

// Error is an application error with a stable code.
type Error struct {
	Code    errcodes.Code
	Kind    Kind
	Details map[string]any
	Err     error
}

func (e *Error) Error() string {
	if e.Err != nil {
		return string(e.Code) + ": " + e.Err.Error()
	}
	return string(e.Code)
}

// Unwrap returns the wrapped cause, if any.
func (e *Error) Unwrap() error { return e.Err }

// New creates an error of the given kind and code.
func New(kind Kind, code errcodes.Code) *Error { return &Error{Kind: kind, Code: code} }

// WithDetails sets the details object and returns e.
func (e *Error) WithDetails(d map[string]any) *Error { e.Details = d; return e }

// Wrap attaches an underlying cause (logged, never sent to clients) and returns e.
func (e *Error) Wrap(err error) *Error { e.Err = err; return e }

func Invalid(code errcodes.Code) *Error         { return New(KindInvalid, code) }
func Unauthenticated(code errcodes.Code) *Error { return New(KindUnauthenticated, code) }
func Forbidden(code errcodes.Code) *Error       { return New(KindForbidden, code) }
func NotFound(code errcodes.Code) *Error        { return New(KindNotFound, code) }
func Conflict(code errcodes.Code) *Error        { return New(KindConflict, code) }

// RateLimited builds a 429 error whose details.retry_after_seconds is
// retryAfter rounded up to whole seconds, minimum 1.
func RateLimited(retryAfter time.Duration) *Error {
	secs := max(1, int(math.Ceil(retryAfter.Seconds())))
	return New(KindRateLimited, errcodes.RateLimitExceeded).
		WithDetails(map[string]any{"retry_after_seconds": secs})
}
