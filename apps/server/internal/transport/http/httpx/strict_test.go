package httpx_test

import (
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/XYZLZ/MyPark/apps/server/internal/domain/apperr"
	"github.com/XYZLZ/MyPark/apps/server/internal/domain/errcodes"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/validation"
	"github.com/XYZLZ/MyPark/apps/server/internal/transport/http/httpx"
)

type strictInner struct {
	N int64 `json:"n"`
}

type strictDTO struct {
	A     string      `json:"a"`
	Inner strictInner `json:"inner"`
	Amt   int64       `json:"amt"`
}

func strictErr(t *testing.T, raw string) *apperr.Error {
	t.Helper()
	var out strictDTO
	err := httpx.DecodeStrict([]byte(raw), &out)
	if err == nil {
		t.Fatalf("DecodeStrict(%s) = nil, want error", raw)
	}
	ae, ok := errors.AsType[*apperr.Error](err)
	if !ok {
		t.Fatalf("DecodeStrict(%s) error %T is not *apperr.Error", raw, err)
	}
	return ae
}

func TestDecodeStrict_Valid(t *testing.T) {
	var out strictDTO
	if err := httpx.DecodeStrict([]byte(` {"a":"x","inner":{"n":3},"amt":9223372036854775807} `), &out); err != nil {
		t.Fatalf("err = %v", err)
	}
	if out.A != "x" || out.Inner.N != 3 || out.Amt != 9223372036854775807 {
		t.Fatalf("out = %+v", out)
	}
}

func TestDecodeStrict_UnknownField(t *testing.T) {
	for name, raw := range map[string]string{
		"top level": `{"a":"x","zzz":1}`,
		"nested":    `{"inner":{"n":1,"extra":true}}`,
	} {
		t.Run(name, func(t *testing.T) {
			ae := strictErr(t, raw)
			if ae.Kind != apperr.KindInvalid || ae.Code != errcodes.ValidationFailed {
				t.Fatalf("kind/code = %v/%v, want invalid/validation.failed", ae.Kind, ae.Code)
			}
			fields, ok := ae.Details["fields"].([]validation.FieldError)
			if !ok || len(fields) != 1 || fields[0].Rule != "unknown_field" || fields[0].Field == "" {
				t.Fatalf("fields = %#v", ae.Details["fields"])
			}
		})
	}
}

func TestDecodeStrict_UnknownFieldNeverEchoesValue(t *testing.T) {
	ae := strictErr(t, `{"a":"x","zzz":"super-secret-value"}`)
	fields := ae.Details["fields"].([]validation.FieldError)
	if fields[0].Field != "zzz" {
		t.Fatalf("field = %q, want zzz", fields[0].Field)
	}
	if strings.Contains(ae.Error(), "super-secret-value") {
		t.Fatal("error text echoes the value")
	}
}

func TestDecodeStrict_InvalidJSONIs400(t *testing.T) {
	for name, raw := range map[string]string{
		"empty":           ``,
		"null":            `null`,
		"array":           `[]`,
		"number":          `5`,
		"string":          `"x"`,
		"malformed":       `{"a":`,
		"trailing object": `{"a":"x"} {"a":"y"}`,
		"trailing junk":   `{"a":"x"} junk`,
		"wrong type":      `{"a":5}`,
		"nested wrong":    `{"inner":{"n":"3"}}`,
		"float for int":   `{"amt":5000.5}`,
		"string for int":  `{"amt":"5000"}`,
		"int64 overflow":  `{"amt":1e20}`,
	} {
		t.Run(name, func(t *testing.T) {
			ae := strictErr(t, raw)
			if ae.Kind != apperr.KindBadRequest || ae.Code != errcodes.HTTPInvalidJSON {
				t.Fatalf("kind/code = %v/%v, want bad_request/http.invalid_json", ae.Kind, ae.Code)
			}
		})
	}
}

func TestDecodeStrictAt_PrefixesFieldPath(t *testing.T) {
	var out strictDTO
	err := httpx.DecodeStrictAt([]byte(`{"zzz":1}`), &out, "payload")
	ae, _ := errors.AsType[*apperr.Error](err)
	fields, _ := ae.Details["fields"].([]validation.FieldError)
	if len(fields) != 1 || fields[0].Field != "payload.zzz" {
		t.Fatalf("fields = %#v", ae.Details["fields"])
	}
}

func TestInvalidFields(t *testing.T) {
	err := httpx.InvalidFields(validation.FieldError{Field: "a", Rule: "required"})
	ae, ok := errors.AsType[*apperr.Error](err)
	if !ok || ae.Kind != apperr.KindInvalid || ae.Code != errcodes.ValidationFailed {
		t.Fatalf("err = %v", err)
	}
}

func TestDecodeStrict_CaseAndDuplicateKeys(t *testing.T) {
	for name, tc := range map[string]struct{ raw, field string }{
		"case variant top":      {`{"A":"x"}`, "A"},
		"case variant nested":   {`{"inner":{"N":1}}`, "inner.N"},
		"duplicate top":         {`{"a":"x","a":"y"}`, "a"},
		"duplicate nested":      {`{"inner":{"n":1,"n":2}}`, "inner.n"},
		"duplicate after other": {`{"a":"x","inner":{"n":1},"a":"z"}`, "a"},
	} {
		t.Run(name, func(t *testing.T) {
			ae := strictErr(t, tc.raw)
			fields, _ := ae.Details["fields"].([]validation.FieldError)
			if ae.Kind != apperr.KindInvalid || len(fields) != 1 || fields[0].Rule != "unknown_field" || fields[0].Field != tc.field {
				t.Fatalf("kind=%v fields=%#v, want unknown_field on %q", ae.Kind, ae.Details["fields"], tc.field)
			}
		})
	}
}

func TestDecodeStrict_WrapsNoRequestValue(t *testing.T) {
	ae := strictErr(t, `{"amt":12345678901234567890123}`)
	if ae.Kind != apperr.KindBadRequest {
		t.Fatalf("kind = %v", ae.Kind)
	}
	if strings.Contains(ae.Error(), "12345678") || (ae.Err != nil && strings.Contains(ae.Err.Error(), "12345678")) {
		t.Fatalf("error chain echoes the value: %v", ae.Err)
	}
}

func TestDecodeStrict_KeyTruncatedOnRuneBoundary(t *testing.T) {
	key := strings.Repeat("é", 40) // 80 bytes
	ae := strictErr(t, `{"`+key+`":1}`)
	f := ae.Details["fields"].([]validation.FieldError)[0].Field
	if !utf8.ValidString(f) || len(f) > 64 {
		t.Fatalf("field %q (%d bytes) is not a valid truncation", f, len(f))
	}
}
