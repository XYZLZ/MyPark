package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/XYZLZ/MyPark/apps/server/internal/transport/http/middleware"
)

func corsHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/x", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	return middleware.CORS([]string{"http://localhost:3000"})(middleware.JSONErrors(mux))
}

func TestCORS_AllowedOrigin(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/v1/x", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	rec := httptest.NewRecorder()
	corsHandler().ServeHTTP(rec, req)

	h := rec.Header()
	if got := h.Get("Access-Control-Allow-Origin"); got != "http://localhost:3000" {
		t.Errorf("ACAO = %q", got)
	}
	if !strings.Contains(h.Get("Vary"), "Origin") {
		t.Errorf("Vary = %q, want Origin", h.Get("Vary"))
	}
	if got := h.Get("Access-Control-Expose-Headers"); got != "X-Request-Id, Retry-After" {
		t.Errorf("Expose-Headers = %q", got)
	}
	if _, ok := h["Access-Control-Allow-Credentials"]; ok {
		t.Error("Allow-Credentials must not be set")
	}
}

func TestCORS_DisallowedOrigin(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/v1/x", nil)
	req.Header.Set("Origin", "http://evil.test")
	rec := httptest.NewRecorder()
	corsHandler().ServeHTTP(rec, req)
	for k := range rec.Header() {
		if strings.HasPrefix(k, "Access-Control-") {
			t.Errorf("unexpected header %s", k)
		}
	}
}

func TestCORS_PreflightOnPostOnlyRoute(t *testing.T) {
	req := httptest.NewRequest(http.MethodOptions, "/v1/x", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	req.Header.Set("Access-Control-Request-Method", "POST")
	rec := httptest.NewRecorder()
	corsHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	for k, want := range map[string]string{
		"Access-Control-Allow-Methods": "GET, POST, PATCH, DELETE",
		"Access-Control-Allow-Headers": "Authorization, Content-Type, X-Request-Id",
		"Access-Control-Max-Age":       "600",
		"Access-Control-Allow-Origin":  "http://localhost:3000",
	} {
		if got := rec.Header().Get(k); got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}
}
