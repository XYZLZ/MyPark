package middleware_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/XYZLZ/MyPark/apps/server/internal/transport/http/middleware"
	"github.com/XYZLZ/MyPark/apps/server/internal/transport/http/reqctx"
)

func logLine(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &m); err != nil {
		t.Fatalf("want exactly one JSON log line, got %q: %v", buf.String(), err)
	}
	return m
}

func TestAccessLog_LogsPanickedRequest(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, nil))
	mux := http.NewServeMux()
	mux.HandleFunc("GET /boom", func(http.ResponseWriter, *http.Request) { panic("boom") })
	h := middleware.Recover(discardLogger())(middleware.RequestID(middleware.AccessLog(log)(middleware.JSONErrors(mux))))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/boom", nil))

	m := logLine(t, &buf)
	if m["status"] != float64(500) || m["panicked"] != true || m["route"] != "GET /boom" {
		t.Fatalf("log = %v", m)
	}
}

func TestAccessLog_UserIDFromLogFields(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, nil))
	h := middleware.RequestID(middleware.AccessLog(log)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		reqctx.Fields(r.Context()).UserPublicID = "u1"
	})))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

	m := logLine(t, &buf)
	if m["user_id"] != "u1" {
		t.Fatalf("log = %v", m)
	}
	if m["panicked"] != nil {
		t.Errorf("panicked should be absent on a normal request: %v", m)
	}
}
