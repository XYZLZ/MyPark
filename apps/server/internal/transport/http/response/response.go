// Package response writes JSON bodies and the standard error envelope:
//
//	{"error":{"code":"...","message":"...","details":{...},"request_id":"..."}}
package response

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/XYZLZ/MyPark/apps/server/internal/domain/errcodes"
)

// RequestIDFunc extracts the request ID from a request context. It is a
// package variable so the middleware package can be wired in at startup
// (e.g. response.RequestIDFunc = middleware.RequestIDFrom) without an import
// cycle. The default returns "".
var RequestIDFunc = func(context.Context) string { return "" }

// ErrorBody is the inner object of the error envelope.
type ErrorBody struct {
	Code      errcodes.Code  `json:"code"`
	Message   string         `json:"message"`
	Details   map[string]any `json:"details"`
	RequestID string         `json:"request_id"`
}

// ErrorEnvelope is the top-level error response shape.
type ErrorEnvelope struct {
	Error ErrorBody `json:"error"`
}

// JSON writes v as a JSON body with the given status code.
func JSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		// Headers are already sent; the best we can do is log.
		slog.Error("response: encode JSON body", "err", err)
	}
}

// Error writes the standard error envelope. msg is debug-only English text;
// clients must rely on code. A nil details map is written as {}.
func Error(w http.ResponseWriter, r *http.Request, status int, code errcodes.Code, msg string, details map[string]any) {
	if details == nil {
		details = map[string]any{}
	}
	JSON(w, status, ErrorEnvelope{Error: ErrorBody{
		Code:      code,
		Message:   msg,
		Details:   details,
		RequestID: RequestIDFunc(r.Context()),
	}})
}
