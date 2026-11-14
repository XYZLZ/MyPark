package httpx

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"strconv"

	"github.com/XYZLZ/MyPark/apps/server/internal/domain/apperr"
	"github.com/XYZLZ/MyPark/apps/server/internal/domain/errcodes"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/validation"
)

// maxFieldNameEcho caps the length of an unknown key echoed back to the client.
const maxFieldNameEcho = 64

var unknownFieldMsg = regexp.MustCompile(`^json: unknown field (".*")$`)

// DecodeStrict decodes one JSON object from raw into out, rejecting what the
// device and simulator control APIs must never accept (M2C-52). raw is already
// read and size-limited, and the content type is the caller's check. Failures
// are *apperr.Error: 400 http.invalid_json for an empty, null, non-object,
// malformed, trailing-data or wrong-JSON-type body, and 422 validation.failed
// with a single `unknown_field` entry for a key out has no field for. The
// unknown key's name is echoed, never its value.
func DecodeStrict(raw []byte, out any) error { return DecodeStrictAt(raw, out, "") }

// DecodeStrictAt is DecodeStrict for a nested object: an unknown key is
// reported as prefix + "." + key, so a payload error reads "payload.x".
func DecodeStrictAt(raw []byte, out any, prefix string) error {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || raw[0] != '{' {
		return apperr.New(apperr.KindBadRequest, errcodes.HTTPInvalidJSON)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		if m := unknownFieldMsg.FindStringSubmatch(err.Error()); m != nil {
			name, uerr := strconv.Unquote(m[1])
			if uerr != nil {
				name = "?"
			}
			if len(name) > maxFieldNameEcho {
				name = name[:maxFieldNameEcho]
			}
			if prefix != "" {
				name = prefix + "." + name
			}
			return InvalidFields(validation.FieldError{Field: name, Rule: "unknown_field"})
		}
		return apperr.New(apperr.KindBadRequest, errcodes.HTTPInvalidJSON).Wrap(err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return apperr.New(apperr.KindBadRequest, errcodes.HTTPInvalidJSON)
	}
	return nil
}

// InvalidFields builds the 422 validation.failed error whose details list
// fields, the shape clients already read for every rule failure.
func InvalidFields(fields ...validation.FieldError) *apperr.Error {
	return apperr.Invalid(errcodes.ValidationFailed).WithDetails(map[string]any{"fields": fields})
}
