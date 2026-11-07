package middleware_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/XYZLZ/MyPark/apps/server/internal/transport/http/middleware"
	"github.com/XYZLZ/MyPark/apps/server/internal/transport/http/reqctx"
	"github.com/XYZLZ/MyPark/apps/server/internal/transport/http/response"
)

var uuidV7 = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

// serveRequestID runs a request with the given inbound X-Request-Id header
// through middleware.RequestID and returns the response header value and the
// value seen in the handler context.
func serveRequestID(t *testing.T, inbound string) (header, fromCtx string) {
	t.Helper()
	h := middleware.RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fromCtx = reqctx.RequestID(r.Context())
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if inbound != "" {
		req.Header.Set(middleware.HeaderRequestID, inbound)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Header().Get(middleware.HeaderRequestID), fromCtx
}

func TestRequestID_GeneratesWhenMissing(t *testing.T) {
	header, fromCtx := serveRequestID(t, "")
	if len(header) != 36 {
		t.Fatalf("X-Request-Id = %q, want a 36-char id", header)
	}
	if !uuidV7.MatchString(header) {
		t.Errorf("X-Request-Id = %q, want a canonical UUIDv7", header)
	}
	if fromCtx != header {
		t.Errorf("context id = %q, want %q", fromCtx, header)
	}
}

func TestRequestID_KeepsValidInbound(t *testing.T) {
	header, fromCtx := serveRequestID(t, "abc-123")
	if header != "abc-123" || fromCtx != "abc-123" {
		t.Fatalf("header=%q ctx=%q, want both %q", header, fromCtx, "abc-123")
	}
}

func TestRequestID_ReplacesInvalidInbound(t *testing.T) {
	for _, inbound := range []string{"<script>", strings.Repeat("a", 65), "a b"} {
		header, _ := serveRequestID(t, inbound)
		if header == inbound || !uuidV7.MatchString(header) {
			t.Errorf("inbound %q: X-Request-Id = %q, want a generated UUIDv7", inbound, header)
		}
	}
}

func TestRequestID_AcceptsMaxLengthInbound(t *testing.T) {
	inbound := strings.Repeat("a", 64)
	if header, _ := serveRequestID(t, inbound); header != inbound {
		t.Fatalf("64-char inbound replaced by %q", header)
	}
}

func TestRequestID_GeneratesUniqueIDs(t *testing.T) {
	a, _ := serveRequestID(t, "")
	b, _ := serveRequestID(t, "")
	if a == b {
		t.Fatalf("two generated ids are equal: %q", a)
	}
}

func TestRecover_PanicEnvelopeHasMetaRequestID(t *testing.T) {
	var logs bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&logs, nil))
	h := middleware.Recover(log)(middleware.RequestID(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom")
	})))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	var env response.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode body %q: %v", rec.Body.String(), err)
	}
	if env.Error.Code != "internal.unexpected" {
		t.Errorf("code = %q, want internal.unexpected", env.Error.Code)
	}
	id := rec.Header().Get(middleware.HeaderRequestID)
	if id == "" || env.Meta.RequestID != id {
		t.Errorf("request_id = %q, want header id %q", env.Meta.RequestID, id)
	}
	if !strings.Contains(logs.String(), "boom") || !strings.Contains(logs.String(), id) {
		t.Errorf("panic log missing value or request id: %s", logs.String())
	}
}

func TestRecover_AfterHeadersWrittenDoesNotRewrite(t *testing.T) {
	log := slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil))
	h := middleware.Recover(log)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		panic("late")
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusAccepted || rec.Body.Len() != 0 {
		t.Fatalf("status=%d body=%q, want original 202 and no envelope", rec.Code, rec.Body.String())
	}
}

func TestAccessLog_Fields(t *testing.T) {
	var logs bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&logs, nil))
	mux := http.NewServeMux()
	mux.HandleFunc("GET /items/{id}", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})
	h := middleware.RequestID(middleware.AccessLog(log)(mux))

	for _, tc := range []struct {
		path, route string
		status      float64
	}{
		{"/items/7", "GET /items/{id}", http.StatusTeapot},
		{"/nope", "unmatched", http.StatusNotFound},
	} {
		logs.Reset()
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tc.path, nil))

		var entry map[string]any
		if err := json.Unmarshal(logs.Bytes(), &entry); err != nil {
			t.Fatalf("%s: decode log %q: %v", tc.path, logs.String(), err)
		}
		if entry["route"] != tc.route {
			t.Errorf("%s: route = %v, want %q", tc.path, entry["route"], tc.route)
		}
		if entry["status"] != tc.status {
			t.Errorf("%s: status = %v, want %v", tc.path, entry["status"], tc.status)
		}
		if entry["method"] != http.MethodGet {
			t.Errorf("%s: method = %v", tc.path, entry["method"])
		}
		if entry["request_id"] != rec.Header().Get(middleware.HeaderRequestID) {
			t.Errorf("%s: request_id = %v", tc.path, entry["request_id"])
		}
		if _, ok := entry["duration_ms"].(float64); !ok {
			t.Errorf("%s: duration_ms = %v, want a number", tc.path, entry["duration_ms"])
		}
	}
}

func TestRequestID_InstallsLogFields(t *testing.T) {
	var f *reqctx.LogFields
	h := middleware.RequestID(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		f = reqctx.Fields(r.Context())
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	if f == nil {
		t.Fatal("RequestID did not install reqctx.LogFields")
	}
}
