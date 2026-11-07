package middleware_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/XYZLZ/MyPark/apps/server/internal/transport/http/middleware"
	"github.com/XYZLZ/MyPark/apps/server/internal/transport/http/response"
)

func serveJSONErrors(t *testing.T, method, path string) (*httptest.ResponseRecorder, response.ErrorEnvelope) {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ok", func(http.ResponseWriter, *http.Request) {})
	h := middleware.RequestID(middleware.JSONErrors(mux))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
	var env response.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode body %q: %v", rec.Body.String(), err)
	}
	return rec, env
}

func TestUnknownRoute_ReturnsEnvelope(t *testing.T) {
	rec, env := serveJSONErrors(t, http.MethodGet, "/nope")
	if rec.Code != http.StatusNotFound || env.Error.Code != "http.not_found" {
		t.Fatalf("got %d %q, want 404 http.not_found", rec.Code, env.Error.Code)
	}
	if id := rec.Header().Get(middleware.HeaderRequestID); id == "" || env.Meta.RequestID != id {
		t.Errorf("meta.request_id = %q, want header %q", env.Meta.RequestID, id)
	}
}

func TestMethodNotAllowed_ReturnsEnvelope(t *testing.T) {
	rec, env := serveJSONErrors(t, http.MethodPost, "/ok")
	if rec.Code != http.StatusMethodNotAllowed || env.Error.Code != "http.method_not_allowed" {
		t.Fatalf("got %d %q, want 405 http.method_not_allowed", rec.Code, env.Error.Code)
	}
	if !strings.Contains(rec.Header().Get("Allow"), http.MethodGet) {
		t.Errorf("Allow = %q, want it to list GET", rec.Header().Get("Allow"))
	}
	if id := rec.Header().Get(middleware.HeaderRequestID); id == "" || env.Meta.RequestID != id {
		t.Errorf("meta.request_id = %q, want header %q", env.Meta.RequestID, id)
	}
}
