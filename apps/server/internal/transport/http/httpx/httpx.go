// Package httpx holds the HTTP helpers every handler shares: strict JSON
// decoding, error-to-response mapping, PATCH nullability and path parsing.
package httpx

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"strconv"

	"github.com/google/uuid"

	"github.com/XYZLZ/MyPark/apps/server/internal/domain/apperr"
	"github.com/XYZLZ/MyPark/apps/server/internal/domain/errcodes"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/validation"
	"github.com/XYZLZ/MyPark/apps/server/internal/transport/http/response"
)

// MaxBodyBytes caps request bodies at 64 KiB.
const MaxBodyBytes = 64 << 10

// DecodeJSON reads a JSON object body into T and validates it with v (skipped
// when v is nil). Failures are *apperr.Error: UnsupportedMedia for a
// non-JSON content type, TooLarge past MaxBodyBytes, BadRequest for an empty,
// null, non-object, malformed or trailing-garbage body, and Invalid
// (validation.failed, details.fields) for rule violations.
func DecodeJSON[T any](r *http.Request, v *validation.Validator) (T, error) {
	var out T

	mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mt != "application/json" {
		return out, apperr.New(apperr.KindUnsupportedMedia, errcodes.HTTPUnsupportedMediaType)
	}

	raw, err := io.ReadAll(http.MaxBytesReader(nil, r.Body, MaxBodyBytes))
	if err != nil {
		if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
			return out, apperr.New(apperr.KindTooLarge, errcodes.HTTPPayloadTooLarge)
		}
		return out, apperr.New(apperr.KindBadRequest, errcodes.HTTPInvalidJSON).Wrap(err)
	}

	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || raw[0] != '{' {
		return out, apperr.New(apperr.KindBadRequest, errcodes.HTTPInvalidJSON)
	}

	dec := json.NewDecoder(bytes.NewReader(raw))
	if err := dec.Decode(&out); err != nil {
		return out, apperr.New(apperr.KindBadRequest, errcodes.HTTPInvalidJSON).Wrap(err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return out, apperr.New(apperr.KindBadRequest, errcodes.HTTPInvalidJSON)
	}

	if v != nil {
		if err := v.Struct(out); err != nil {
			if verr, ok := errors.AsType[*validation.Error](err); ok {
				return out, apperr.Invalid(errcodes.ValidationFailed).
					WithDetails(map[string]any{"fields": verr.Fields}).Wrap(verr)
			}
			return out, err
		}
	}
	return out, nil
}

// WriteError writes err as the standard error envelope. Unknown errors, and
// KindInternal ones, become 500 internal.unexpected; their cause is logged and
// never sent to the client.
func WriteError(w http.ResponseWriter, r *http.Request, log *slog.Logger, err error) {
	ae, ok := errors.AsType[*apperr.Error](err)
	if !ok {
		ae = apperr.New(apperr.KindInternal, errcodes.InternalUnexpected).Wrap(err)
	}
	status, msg := statusOf(ae.Kind)
	if status == http.StatusInternalServerError {
		log.ErrorContext(r.Context(), "http: internal error",
			"code", string(ae.Code), "err", err)
		ae = apperr.New(apperr.KindInternal, errcodes.InternalUnexpected)
	}
	if secs, ok := ae.Details["retry_after_seconds"].(int); ok && ae.Kind == apperr.KindRateLimited {
		w.Header().Set("Retry-After", strconv.Itoa(secs))
	}
	response.Error(w, r, status, ae.Code, msg, ae.Details)
}

// statusOf maps a Kind to its HTTP status and fixed English debug message.
func statusOf(k apperr.Kind) (int, string) {
	switch k {
	case apperr.KindBadRequest:
		return http.StatusBadRequest, "malformed request"
	case apperr.KindUnauthenticated:
		return http.StatusUnauthorized, "authentication required"
	case apperr.KindForbidden:
		return http.StatusForbidden, "forbidden"
	case apperr.KindNotFound:
		return http.StatusNotFound, "not found"
	case apperr.KindConflict:
		return http.StatusConflict, "conflict"
	case apperr.KindTooLarge:
		return http.StatusRequestEntityTooLarge, "request body too large"
	case apperr.KindUnsupportedMedia:
		return http.StatusUnsupportedMediaType, "unsupported media type"
	case apperr.KindInvalid:
		return http.StatusUnprocessableEntity, "request validation failed"
	case apperr.KindRateLimited:
		return http.StatusTooManyRequests, "too many requests"
	default:
		return http.StatusInternalServerError, "internal error"
	}
}

// Nullable distinguishes an absent JSON field (Set false) from an explicit
// null (Set and Null true) and a value (Set true, Value filled), for PATCH.
type Nullable[T any] struct {
	Set   bool
	Null  bool
	Value T
}

// UnmarshalJSON implements json.Unmarshaler. It is only called when the field
// is present in the document.
func (n *Nullable[T]) UnmarshalJSON(b []byte) error {
	n.Set = true
	if bytes.Equal(bytes.TrimSpace(b), []byte("null")) {
		n.Null = true
		var zero T
		n.Value = zero
		return nil
	}
	n.Null = false
	return json.Unmarshal(b, &n.Value)
}

// PathUUID parses the named path value as a canonical 36-character UUID.
func PathUUID(r *http.Request, name string) (uuid.UUID, bool) {
	s := r.PathValue(name)
	if len(s) != 36 {
		return uuid.UUID{}, false
	}
	id, err := uuid.Parse(s)
	if err != nil {
		return uuid.UUID{}, false
	}
	return id, true
}
