package httpx_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/XYZLZ/MyPark/apps/server/internal/domain/apperr"
	"github.com/XYZLZ/MyPark/apps/server/internal/domain/errcodes"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/validation"
	"github.com/XYZLZ/MyPark/apps/server/internal/transport/http/httpx"
	"github.com/XYZLZ/MyPark/apps/server/internal/transport/http/reqctx"
)

type dto struct {
	Email string `json:"email" validate:"required"`
}

type loose struct {
	A int `json:"a"`
}

func newReq(body, contentType string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(body))
	if contentType != "" {
		r.Header.Set("Content-Type", contentType)
	}
	return r.WithContext(reqctx.WithRequestID(r.Context(), "rid-1"))
}

func errOf(t *testing.T, err error) *apperr.Error {
	t.Helper()
	e, ok := errors.AsType[*apperr.Error](err)
	if !ok {
		t.Fatalf("error %v is not *apperr.Error", err)
	}
	return e
}

func TestWriteError_KindToStatus(t *testing.T) {
	cases := []struct {
		err    *apperr.Error
		status int
	}{
		{apperr.New(apperr.KindBadRequest, errcodes.HTTPInvalidJSON), 400},
		{apperr.Unauthenticated(errcodes.AuthInvalidCredentials), 401},
		{apperr.Forbidden(errcodes.AuthForbidden), 403},
		{apperr.NotFound(errcodes.HTTPNotFound), 404},
		{apperr.Conflict(errcodes.UserEmailTaken), 409},
		{apperr.New(apperr.KindTooLarge, errcodes.HTTPPayloadTooLarge), 413},
		{apperr.New(apperr.KindUnsupportedMedia, errcodes.HTTPUnsupportedMediaType), 415},
		{apperr.Invalid(errcodes.ValidationFailed), 422},
		{apperr.RateLimited(1500 * time.Millisecond), 429},
		{apperr.New(apperr.KindInternal, errcodes.InternalUnexpected), 500},
	}
	for _, c := range cases {
		rec := httptest.NewRecorder()
		httpx.WriteError(rec, newReq("", ""), slog.New(slog.DiscardHandler), c.err)
		if rec.Code != c.status {
			t.Errorf("%v: status = %d, want %d", c.err.Code, rec.Code, c.status)
		}
		var body struct {
			Error struct {
				Code    string         `json:"code"`
				Details map[string]any `json:"details"`
			} `json:"error"`
			Meta struct {
				RequestID string `json:"request_id"`
			} `json:"meta"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if body.Error.Code != string(c.err.Code) || body.Meta.RequestID != "rid-1" || body.Error.Details == nil {
			t.Errorf("%v: body = %s", c.err.Code, rec.Body)
		}
		if c.status == 429 {
			if got := rec.Header().Get("Retry-After"); got != "2" {
				t.Errorf("Retry-After = %q, want 2", got)
			}
		}
	}
}

func TestWriteError_UnknownErrorIs500AndLogged(t *testing.T) {
	var logBuf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&logBuf, nil))
	rec := httptest.NewRecorder()
	httpx.WriteError(rec, newReq("", ""), log, errors.New("db down: secret-dsn"))

	if rec.Code != 500 {
		t.Fatalf("status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "internal.unexpected") {
		t.Errorf("body = %s", rec.Body)
	}
	if strings.Contains(rec.Body.String(), "secret-dsn") {
		t.Error("body leaks cause")
	}
	if !strings.Contains(logBuf.String(), "secret-dsn") {
		t.Error("cause not logged")
	}
}

func TestDecodeJSON_RequiresJSONContentType(t *testing.T) {
	_, err := httpx.DecodeJSON[loose](newReq(`{"a":1}`, "text/plain"), nil)
	if e := errOf(t, err); e.Kind != apperr.KindUnsupportedMedia || e.Code != errcodes.HTTPUnsupportedMediaType {
		t.Errorf("got %+v", e)
	}
	_, err = httpx.DecodeJSON[loose](newReq(`{"a":1}`, ""), nil)
	if e := errOf(t, err); e.Kind != apperr.KindUnsupportedMedia {
		t.Errorf("missing content type: %+v", e)
	}
	v, err := httpx.DecodeJSON[loose](newReq(`{"a":1}`, "application/json; charset=utf-8"), nil)
	if err != nil || v.A != 1 {
		t.Errorf("v=%+v err=%v", v, err)
	}
}

func TestDecodeJSON_TooLarge(t *testing.T) {
	pad := strings.Repeat("a", httpx.MaxBodyBytes)
	body := `{"a":1,"pad":"` + pad + `"}`
	_, err := httpx.DecodeJSON[loose](newReq(body, "application/json"), nil)
	if e := errOf(t, err); e.Kind != apperr.KindTooLarge || e.Code != errcodes.HTTPPayloadTooLarge {
		t.Errorf("got %+v", e)
	}
}

func TestDecodeJSON_RejectsNullEmptyArrayAndTrailing(t *testing.T) {
	for _, body := range []string{"", "null", "[]", "{} x", "{}}", "{}]", `{"a":1}{"a":2}`, `{"a":"x"}`, `"s"`, "5", "{"} {
		_, err := httpx.DecodeJSON[loose](newReq(body, "application/json"), nil)
		if err == nil {
			t.Errorf("body %q accepted", body)
			continue
		}
		if e := errOf(t, err); e.Kind != apperr.KindBadRequest || e.Code != errcodes.HTTPInvalidJSON {
			t.Errorf("body %q: got %+v", body, e)
		}
	}
	_, err := httpx.DecodeJSON[dto](newReq(`{"email":5}`, "application/json"), validation.New())
	if e := errOf(t, err); e.Code != errcodes.HTTPInvalidJSON {
		t.Errorf("type mismatch: %+v", e)
	}
	if _, err := httpx.DecodeJSON[loose](newReq(" \n{\"a\":1}\n ", "application/json"), nil); err != nil {
		t.Errorf("surrounding whitespace rejected: %v", err)
	}
}

func TestDecodeJSON_ValidationFieldsUseJSONNames(t *testing.T) {
	_, err := httpx.DecodeJSON[dto](newReq(`{}`, "application/json"), validation.New())
	e := errOf(t, err)
	if e.Kind != apperr.KindInvalid || e.Code != errcodes.ValidationFailed {
		t.Fatalf("got %+v", e)
	}
	fields, ok := e.Details["fields"].([]validation.FieldError)
	if !ok || len(fields) != 1 || fields[0] != (validation.FieldError{Field: "email", Rule: "required", Param: ""}) {
		t.Errorf("fields = %#v", e.Details["fields"])
	}
}

func TestNullable_AbsentNullValue(t *testing.T) {
	type patch struct {
		Phone httpx.Nullable[string] `json:"phone"`
	}
	var p patch
	if err := json.Unmarshal([]byte(`{}`), &p); err != nil || p.Phone.Set || p.Phone.Null {
		t.Errorf("absent: %+v %v", p, err)
	}
	p = patch{}
	if err := json.Unmarshal([]byte(`{"phone":null}`), &p); err != nil || !p.Phone.Set || !p.Phone.Null {
		t.Errorf("null: %+v %v", p, err)
	}
	p = patch{}
	if err := json.Unmarshal([]byte(`{"phone":"+18095550101"}`), &p); err != nil ||
		!p.Phone.Set || p.Phone.Null || p.Phone.Value != "+18095550101" {
		t.Errorf("value: %+v %v", p, err)
	}
	if err := json.Unmarshal([]byte(`{"phone":5}`), &p); err == nil {
		t.Error("wrong type accepted")
	}
}

func TestPathUUID_Malformed(t *testing.T) {
	mux := http.NewServeMux()
	var got bool
	mux.HandleFunc("GET /v/{id}", func(w http.ResponseWriter, r *http.Request) {
		_, got = httpx.PathUUID(r, "id")
	})
	for path, want := range map[string]bool{
		"/v/abc": false,
		"/v/0190f0e6-7b1c-7000-8000-000000000001":      true,
		"/v/urn:uuid:0190f0e6-7b1c-7000-8000-00000001": false,
		"/v/{0190f0e6-7b1c-7000-8000-000000000001}":    false,
	} {
		got = !want
		mux.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, path, nil))
		if got != want {
			t.Errorf("%s: ok = %v, want %v", path, got, want)
		}
	}
}
