package middleware_test

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/XYZLZ/MyPark/apps/server/internal/transport/http/middleware"
)

func discardLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func TestRecover_StaleHeadersDropped(t *testing.T) {
	h := middleware.Recover(discardLogger())(middleware.RequestID(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "3")
		w.Header().Set("Location", "/elsewhere")
		w.Header().Set("Cache-Control", "public")
		panic("boom")
	})))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(middleware.HeaderRequestID, "rid-1")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d", rec.Code)
	}
	var body struct {
		Error struct{ Code string } `json:"error"`
		Meta  struct {
			RequestID string `json:"request_id"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body not JSON: %v (%q)", err, rec.Body.String())
	}
	if body.Meta.RequestID != "rid-1" {
		t.Errorf("meta.request_id = %q", body.Meta.RequestID)
	}
	if rec.Header().Get("Location") != "" {
		t.Error("Location must be dropped")
	}
	if rec.Header().Get("Content-Length") == "3" {
		t.Error("stale Content-Length kept")
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q", got)
	}
	if got := rec.Header().Get(middleware.HeaderRequestID); got != "rid-1" {
		t.Errorf("X-Request-Id = %q", got)
	}
	assertSecurityHeaders(t, rec.Header())
}

func TestRecover_ErrAbortHandlerWrapped(t *testing.T) {
	h := middleware.Recover(discardLogger())(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic(fmt.Errorf("x: %w", http.ErrAbortHandler))
	}))
	defer func() {
		if recover() == nil {
			t.Fatal("wrapped ErrAbortHandler must be re-panicked")
		}
	}()
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
}
