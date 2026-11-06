package response_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/XYZLZ/MyPark/apps/server/internal/domain/errcodes"
	"github.com/XYZLZ/MyPark/apps/server/internal/transport/http/response"
)

type envelope struct {
	Error struct {
		Code      string         `json:"code"`
		Message   string         `json:"message"`
		Details   map[string]any `json:"details"`
		RequestID string         `json:"request_id"`
	} `json:"error"`
}

func TestError_WritesEnvelope(t *testing.T) {
	orig := response.RequestIDFunc
	t.Cleanup(func() { response.RequestIDFunc = orig })
	response.RequestIDFunc = func(context.Context) string { return "req-1" }

	rec := httptest.NewRecorder()
	response.Error(rec, httptest.NewRequest(http.MethodGet, "/x", nil), http.StatusServiceUnavailable,
		errcodes.HealthNotReady, "db down", map[string]any{"database": "fail"})

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want %q", ct, "application/json")
	}
	var body envelope
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body %q: %v", rec.Body.String(), err)
	}
	if body.Error.Code != "health.not_ready" {
		t.Errorf("code = %q, want %q", body.Error.Code, "health.not_ready")
	}
	if body.Error.Message != "db down" {
		t.Errorf("message = %q, want %q", body.Error.Message, "db down")
	}
	if body.Error.RequestID != "req-1" {
		t.Errorf("request_id = %q, want %q", body.Error.RequestID, "req-1")
	}
	if got := body.Error.Details["database"]; got != "fail" {
		t.Errorf("details.database = %v, want %q", got, "fail")
	}
}

func TestError_DefaultRequestIDAndNilDetails(t *testing.T) {
	rec := httptest.NewRecorder()
	response.Error(rec, httptest.NewRequest(http.MethodGet, "/x", nil), http.StatusNotFound,
		errcodes.HTTPNotFound, "not found", nil)

	var raw map[string]map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode body %q: %v", rec.Body.String(), err)
	}
	e := raw["error"]
	for _, k := range []string{"code", "message", "details", "request_id"} {
		if _, ok := e[k]; !ok {
			t.Errorf("envelope missing key %q: %s", k, rec.Body.String())
		}
	}
	if e["request_id"] != "" {
		t.Errorf("request_id = %v, want empty string", e["request_id"])
	}
	if d, ok := e["details"].(map[string]any); !ok || len(d) != 0 {
		t.Errorf("details = %v, want empty object", e["details"])
	}
}

func TestJSON_WritesBody(t *testing.T) {
	rec := httptest.NewRecorder()
	response.JSON(rec, http.StatusOK, map[string]string{"status": "ok"})

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want %q", ct, "application/json")
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["status"] != "ok" {
		t.Errorf("status field = %q, want %q", body["status"], "ok")
	}
}
