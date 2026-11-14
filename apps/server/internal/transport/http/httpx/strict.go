package httpx

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"strings"
	"unicode/utf8"

	"github.com/XYZLZ/MyPark/apps/server/internal/domain/apperr"
	"github.com/XYZLZ/MyPark/apps/server/internal/domain/errcodes"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/validation"
)

// maxFieldNameEcho caps the length of an unknown key echoed back to the client.
const maxFieldNameEcho = 64

// errMalformedJSON is the only cause attached to a 400: the decoder's own
// errors can quote request values, so they are never wrapped.
var errMalformedJSON = errors.New("httpx: malformed json body")

func badJSON() *apperr.Error {
	return apperr.New(apperr.KindBadRequest, errcodes.HTTPInvalidJSON).Wrap(errMalformedJSON)
}

// DecodeStrict decodes one JSON object from raw into out, rejecting what the
// device and simulator control APIs must never accept (M2C-52). raw is already
// read and size-limited, and the content type is the caller's check. Failures
// are *apperr.Error: 400 http.invalid_json for an empty, null, non-object,
// malformed, trailing-data or wrong-JSON-type body, and 422 validation.failed
// with a single `unknown_field` entry for a key that is not exactly a json
// tag of the struct at that level (Go would match it case-insensitively) and
// for a key repeated within one object (Go would keep the last). The key's
// name is echoed, never its value.
func DecodeStrict(raw []byte, out any) error { return DecodeStrictAt(raw, out, "") }

// DecodeStrictAt is DecodeStrict for a nested object: an unknown key is
// reported as prefix + "." + key, so a payload error reads "payload.x".
func DecodeStrictAt(raw []byte, out any, prefix string) error {
	raw = bytes.TrimSpace(raw)
	// Syntax first: an unknown key ahead of a syntax error must not make a
	// malformed body a 422.
	if len(raw) == 0 || raw[0] != '{' || !json.Valid(raw) {
		return badJSON()
	}
	if err := checkKeys(raw, reflect.TypeOf(out), prefix); err != nil {
		return err
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		return badJSON()
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return badJSON()
	}
	return nil
}

// checkKeys walks the tokens of raw once and applies the key rules that
// encoding/json does not: exact (case-sensitive) names and no duplicates, per
// object and at every depth of struct-typed fields. Anything that is not a
// struct (RawMessage, maps, any) is skipped; RawMessage payloads are checked
// again when the caller decodes them.
func checkKeys(raw []byte, t reflect.Type, prefix string) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	if err := walkObject(dec, t, prefix); err != nil {
		return err
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return badJSON()
	}
	return nil
}

func walkObject(dec *json.Decoder, t reflect.Type, path string) error {
	if _, err := dec.Token(); err != nil { // the opening brace
		return badJSON()
	}
	return walkBody(dec, t, path)
}

// walkBody walks the members of an object whose opening brace is consumed.
func walkBody(dec *json.Decoder, t reflect.Type, path string) error {
	fields := structFields(t)
	seen := map[string]bool{}
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return badJSON()
		}
		key, _ := tok.(string)
		field := key
		if path != "" {
			field = path + "." + key
		}
		if seen[key] {
			return unknownField(field)
		}
		seen[key] = true
		var ft reflect.Type
		if fields != nil {
			var ok bool
			if ft, ok = fields[key]; !ok {
				return unknownField(field)
			}
		}
		if err := walkValue(dec, ft, field); err != nil {
			return err
		}
	}
	if _, err := dec.Token(); err != nil { // the closing brace
		return badJSON()
	}
	return nil
}

// walkValue consumes one value; it descends only into an object whose target
// type is a struct, and skips every other value.
func walkValue(dec *json.Decoder, ft reflect.Type, path string) error {
	tok, err := dec.Token()
	if err != nil {
		return badJSON()
	}
	d, ok := tok.(json.Delim)
	if !ok {
		return nil // a scalar
	}
	if d == '{' && structFields(ft) != nil {
		return walkBody(dec, ft, path)
	}
	return skipRest(dec)
}

// skipRest consumes the remainder of a container whose opening delimiter is consumed.
func skipRest(dec *json.Decoder) error {
	for depth := 1; depth > 0; {
		tok, err := dec.Token()
		if err != nil {
			return badJSON()
		}
		if d, ok := tok.(json.Delim); ok {
			if d == '{' || d == '[' {
				depth++
			} else {
				depth--
			}
		}
	}
	return nil
}

// structFields maps each exact json name of struct type t to its field type,
// or returns nil when t is not a struct (any key is then accepted here).
func structFields(t reflect.Type) map[string]reflect.Type {
	for t != nil && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t == nil || t.Kind() != reflect.Struct {
		return nil
	}
	m := make(map[string]reflect.Type, t.NumField())
	for f := range t.Fields() {
		if !f.IsExported() {
			continue
		}
		name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		switch name {
		case "-":
			continue
		case "":
			name = f.Name
		}
		m[name] = f.Type
	}
	return m
}

func unknownField(field string) error {
	if len(field) > maxFieldNameEcho {
		cut := maxFieldNameEcho
		for cut > 0 && !utf8.RuneStart(field[cut]) {
			cut--
		}
		field = field[:cut]
	}
	return InvalidFields(validation.FieldError{Field: field, Rule: "unknown_field"})
}

// InvalidFields builds the 422 validation.failed error whose details list
// fields, the shape clients already read for every rule failure.
func InvalidFields(fields ...validation.FieldError) *apperr.Error {
	return apperr.Invalid(errcodes.ValidationFailed).WithDetails(map[string]any{"fields": fields})
}
