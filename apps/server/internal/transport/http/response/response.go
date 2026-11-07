// Package response writes the baseline JSON envelope used by every route:
//
//	success: {"data":...,"meta":{"request_id":"..."}}
//	list:    {"data":[...],"meta":{"request_id":"...","page":{"limit":N,"next_cursor":null}}}
//	error:   {"error":{"code":"...","message":"...","details":{...}},"meta":{"request_id":"..."}}
//
// A 204 has no body.
package response

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/XYZLZ/MyPark/apps/server/internal/domain/errcodes"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/validation"
	"github.com/XYZLZ/MyPark/apps/server/internal/transport/http/reqctx"
)

// Page describes the window of a list response.
type Page struct {
	Limit      int     `json:"limit"`
	NextCursor *string `json:"next_cursor"`
}

// Meta is the envelope metadata present on every body.
type Meta struct {
	RequestID string `json:"request_id"`
	Page      *Page  `json:"page,omitempty"`
}

// ErrorBody is the inner object of the error envelope.
type ErrorBody struct {
	Code    errcodes.Code  `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details"`
}

// ErrorEnvelope is the top-level error response shape.
type ErrorEnvelope struct {
	Error ErrorBody `json:"error"`
	Meta  Meta      `json:"meta"`
}

type successEnvelope[T any] struct {
	Data T    `json:"data"`
	Meta Meta `json:"meta"`
}

// OK writes data inside the success envelope with the given status.
func OK[T any](w http.ResponseWriter, r *http.Request, status int, data T) {
	writeJSON(w, status, successEnvelope[T]{
		Data: data,
		Meta: Meta{RequestID: reqctx.RequestID(r.Context())},
	})
}

// List writes items inside the list envelope. A nil items slice encodes as [].
func List[T any](w http.ResponseWriter, r *http.Request, items []T, page Page) {
	if items == nil {
		items = []T{}
	}
	writeJSON(w, http.StatusOK, successEnvelope[[]T]{
		Data: items,
		Meta: Meta{RequestID: reqctx.RequestID(r.Context()), Page: &page},
	})
}

// NoContent writes a 204 with no body.
func NoContent(w http.ResponseWriter) {
	w.WriteHeader(http.StatusNoContent)
}

// Error writes the standard error envelope. msg is debug-only English text;
// clients must rely on code. A nil details map is written as {}.
func Error(w http.ResponseWriter, r *http.Request, status int, code errcodes.Code, msg string, details map[string]any) {
	if details == nil {
		details = map[string]any{}
	}
	writeJSON(w, status, ErrorEnvelope{
		Error: ErrorBody{Code: code, Message: msg, Details: details},
		Meta:  Meta{RequestID: reqctx.RequestID(r.Context())},
	})
}

// ValidationError writes a 422 validation.failed envelope whose details carry
// the failed fields: {"fields":[{"field","rule","param"}]}. A nil err writes
// an empty fields array.
func ValidationError(w http.ResponseWriter, r *http.Request, err *validation.Error) {
	fields := []validation.FieldError{}
	if err != nil && len(err.Fields) > 0 {
		fields = err.Fields
	}
	Error(w, r, http.StatusUnprocessableEntity, errcodes.ValidationFailed,
		"request validation failed", map[string]any{"fields": fields})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		// Headers are already sent; the best we can do is log.
		slog.Error("response: encode JSON body", "err", err)
	}
}
