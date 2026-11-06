package health_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/XYZLZ/MyPark/apps/server/internal/features/health"
)

type fakePinger struct{ err error }

func (f fakePinger) Ping(context.Context) error { return f.err }

type fakeMigrations struct {
	current, expected int64
	err               error
	calls             *int
}

func (f fakeMigrations) Status(context.Context) (int64, int64, error) {
	if f.calls != nil {
		*f.calls++
	}
	return f.current, f.expected, f.err
}

func serve(t *testing.T, p health.Pinger, m health.MigrationStatus, path string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	mux := http.NewServeMux()
	health.NewHandler("api", "test", p, m).Register(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body %q: %v", rec.Body.String(), err)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	return rec, body
}

// notReadyDetails asserts a 503 health.not_ready envelope whose details carry
// both check keys, and returns the details.
func notReadyDetails(t *testing.T, rec *httptest.ResponseRecorder, body map[string]any) map[string]any {
	t.Helper()
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
	errObj, _ := body["error"].(map[string]any)
	if errObj["code"] != "health.not_ready" {
		t.Fatalf("error.code = %v, want health.not_ready", errObj["code"])
	}
	details, _ := errObj["details"].(map[string]any)
	for _, key := range []string{"database", "migrations"} {
		if _, ok := details[key]; !ok {
			t.Errorf("details missing %q: %v", key, details)
		}
	}
	return details
}

func TestHealthz_OK(t *testing.T) {
	// Liveness must not depend on the database.
	rec, body := serve(t, fakePinger{err: errors.New("down")}, fakeMigrations{err: errors.New("down")}, "/healthz")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	want := map[string]any{"status": "ok", "service": "api", "version": "test"}
	if len(body) != len(want) {
		t.Fatalf("body = %v, want %v", body, want)
	}
	for k, v := range want {
		if body[k] != v {
			t.Errorf("body[%q] = %v, want %v", k, body[k], v)
		}
	}
}

func TestReadyz_Ready(t *testing.T) {
	rec, body := serve(t, fakePinger{}, fakeMigrations{current: 3, expected: 3}, "/readyz")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %v)", rec.Code, body)
	}
	if body["status"] != "ready" {
		t.Errorf("status = %v, want ready", body["status"])
	}
	checks, _ := body["checks"].(map[string]any)
	if checks["database"] != "ok" || checks["migrations"] != "ok" {
		t.Errorf("checks = %v, want database=ok migrations=ok", checks)
	}
}

func TestReadyz_DBDown(t *testing.T) {
	calls := 0
	rec, body := serve(t, fakePinger{err: errors.New("dial tcp: refused")},
		fakeMigrations{current: 3, expected: 3, calls: &calls}, "/readyz")
	details := notReadyDetails(t, rec, body)
	if details["database"] != "fail" {
		t.Errorf("details.database = %v, want fail", details["database"])
	}
	// Documented behavior: with the database down, Status is not called and
	// migrations are reported as "fail".
	if details["migrations"] != "fail" {
		t.Errorf("details.migrations = %v, want fail", details["migrations"])
	}
	if calls != 0 {
		t.Errorf("Status called %d times, want 0 when Ping fails", calls)
	}
}

func TestReadyz_MigrationsBehind(t *testing.T) {
	rec, body := serve(t, fakePinger{}, fakeMigrations{current: 2, expected: 3}, "/readyz")
	details := notReadyDetails(t, rec, body)
	if details["database"] != "ok" || details["migrations"] != "behind" {
		t.Errorf("details = %v, want database=ok migrations=behind", details)
	}
}

func TestReadyz_MigrationStatusError(t *testing.T) {
	rec, body := serve(t, fakePinger{}, fakeMigrations{err: errors.New("no table")}, "/readyz")
	details := notReadyDetails(t, rec, body)
	if details["database"] != "ok" || details["migrations"] != "fail" {
		t.Errorf("details = %v, want database=ok migrations=fail", details)
	}
}
