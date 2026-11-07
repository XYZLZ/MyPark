package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/XYZLZ/MyPark/apps/server/internal/transport/http/middleware"
)

func assertSecurityHeaders(t *testing.T, h http.Header) {
	t.Helper()
	want := map[string]string{
		"X-Content-Type-Options":  "nosniff",
		"Cache-Control":           "no-store",
		"Referrer-Policy":         "no-referrer",
		"Content-Security-Policy": "default-src 'none'; frame-ancestors 'none'",
	}
	for k, v := range want {
		if got := h.Get(k); got != v {
			t.Errorf("%s = %q, want %q", k, got, v)
		}
	}
}

func TestSecureHeaders_SetOnEveryResponse(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ok", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("GET /boom", func(http.ResponseWriter, *http.Request) { panic("boom") })
	h := middleware.Recover(discardLogger())(middleware.RequestID(middleware.SecureHeaders(middleware.JSONErrors(mux))))

	for _, tc := range []struct {
		path string
		code int
	}{{"/ok", 200}, {"/missing", 404}, {"/boom", 500}} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tc.path, nil))
		if rec.Code != tc.code {
			t.Fatalf("%s: status = %d, want %d", tc.path, rec.Code, tc.code)
		}
		assertSecurityHeaders(t, rec.Header())
	}
}
