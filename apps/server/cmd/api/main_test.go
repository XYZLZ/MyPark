package main

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/XYZLZ/MyPark/apps/server/internal/features/health"
	"github.com/XYZLZ/MyPark/apps/server/internal/testsupport"
	"github.com/XYZLZ/MyPark/apps/server/internal/transport/http/middleware"
)

type okPinger struct{}

func (okPinger) Ping(context.Context) error { return nil }

type okMigrations struct{}

func (okMigrations) Status(context.Context) (int64, int64, error) { return 1, 1, nil }

func doRequest(t *testing.T, h *health.Handler, method, path string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	rec := httptest.NewRecorder()
	newHandler(log, h, nil).ServeHTTP(rec, httptest.NewRequest(method, path, nil))
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body %q: %v", rec.Body.String(), err)
	}
	return rec, body
}

func readyHealth() *health.Handler {
	return health.NewHandler("api", "test", okPinger{}, okMigrations{})
}

// assertEnvelope checks status, error code, content type, and that
// meta.request_id matches the X-Request-Id response header set by the full chain.
func assertEnvelope(t *testing.T, rec *httptest.ResponseRecorder, body map[string]any, status int, code string) {
	t.Helper()
	if rec.Code != status {
		t.Fatalf("status = %d, want %d (body %v)", rec.Code, status, body)
	}
	errObj, _ := body["error"].(map[string]any)
	if errObj["code"] != code {
		t.Errorf("error.code = %v, want %s", errObj["code"], code)
	}
	id := rec.Header().Get(middleware.HeaderRequestID)
	meta, _ := body["meta"].(map[string]any)
	if id == "" || meta["request_id"] != id {
		t.Errorf("meta.request_id = %v, want X-Request-Id %q", meta["request_id"], id)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
}

func TestUnknownRoute_ReturnsEnvelope(t *testing.T) {
	rec, body := doRequest(t, readyHealth(), http.MethodGet, "/nope")
	assertEnvelope(t, rec, body, http.StatusNotFound, "http.not_found")
}

func TestMethodNotAllowed_ReturnsEnvelope(t *testing.T) {
	rec, body := doRequest(t, readyHealth(), http.MethodPost, "/healthz")
	assertEnvelope(t, rec, body, http.StatusMethodNotAllowed, "http.method_not_allowed")
	if allow := rec.Header().Get("Allow"); !strings.Contains(allow, http.MethodGet) {
		t.Errorf("Allow = %q, want it to list GET", allow)
	}
}

func TestHealthz_ThroughFullChain(t *testing.T) {
	rec, body := doRequest(t, readyHealth(), http.MethodGet, "/healthz")
	data, _ := body["data"].(map[string]any)
	if rec.Code != http.StatusOK || data["status"] != "ok" {
		t.Fatalf("status=%d body=%v, want 200 ok", rec.Code, body)
	}
	if meta, _ := body["meta"].(map[string]any); meta["request_id"] != rec.Header().Get(middleware.HeaderRequestID) {
		t.Errorf("meta.request_id = %v, want X-Request-Id header", meta["request_id"])
	}
	if rec.Header().Get(middleware.HeaderRequestID) == "" {
		t.Error("missing X-Request-Id on success response")
	}
}

// startRun starts run with env and returns the base URL it listens on. run
// is stopped and must return nil when the test ends.
func startRun(t *testing.T, env map[string]string) string {
	t.Helper()
	return testsupport.StartServer(t, run, env, "api listening")
}

func getReadyz(t *testing.T, baseURL string) (int, map[string]any) {
	t.Helper()
	resp, err := http.Get(baseURL + "/readyz")
	if err != nil {
		t.Fatalf("GET /readyz: %v", err)
	}
	defer resp.Body.Close()
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode /readyz body: %v", err)
	}
	return resp.StatusCode, body
}

// TestRun_ReadyzReady_Integration checks run wires the real pool and
// migration status: a reachable, migrated database makes /readyz 200.
func TestRun_ReadyzReady_Integration(t *testing.T) {
	base := startRun(t, map[string]string{"HTTP_ADDR": "127.0.0.1:0", "DATABASE_URL": testsupport.NewDBURL(t)})
	status, body := getReadyz(t, base)
	data, _ := body["data"].(map[string]any)
	if status != http.StatusOK || data["status"] != "ready" {
		t.Fatalf("/readyz = %d %v, want 200 ready", status, body)
	}
}

// TestRun_ReadyzDBDown checks an unreachable database does not stop the API
// from booting; /readyz reports it as 503 with database=fail instead.
func TestRun_ReadyzDBDown(t *testing.T) {
	base := startRun(t, map[string]string{
		"HTTP_ADDR":    "127.0.0.1:0",
		"DATABASE_URL": "postgres://mypark:x@127.0.0.1:1/mypark?connect_timeout=1",
	})
	status, body := getReadyz(t, base)
	errObj, _ := body["error"].(map[string]any)
	details, _ := errObj["details"].(map[string]any)
	if status != http.StatusServiceUnavailable || errObj["code"] != "health.not_ready" || details["database"] != "fail" {
		t.Fatalf("/readyz = %d %v, want 503 health.not_ready database=fail", status, body)
	}
}

func TestRun_InvalidConfig(t *testing.T) {
	err := run(t.Context(), func(string) string { return "" }, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "DATABASE_URL") {
		t.Fatalf("run error = %v, want one naming DATABASE_URL", err)
	}
}

func TestRun_ListenError(t *testing.T) {
	// Occupy a port so run fails at bind time, after config validation.
	busy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer busy.Close()
	env := map[string]string{"HTTP_ADDR": busy.Addr().String(), "DATABASE_URL": "postgres://x"}
	err = run(t.Context(), func(k string) string { return env[k] }, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "listen") {
		t.Fatalf("run error = %v, want a listen error", err)
	}
}

func TestRun_ShutsDownOnCancel(t *testing.T) {
	env := map[string]string{
		"HTTP_ADDR":    "127.0.0.1:0",
		"DATABASE_URL": "postgres://mypark:s3cret@localhost:5432/mypark",
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var out testsupport.SyncBuffer
	done := make(chan error, 1)
	go func() { done <- run(ctx, func(k string) string { return env[k] }, &out) }()

	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(out.String(), "api listening") {
		if time.Now().After(deadline) {
			t.Fatalf("server did not start; output: %s", out.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("run returned %v, want nil after graceful shutdown", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("run did not return after cancel")
	}
	logs := out.String()
	if strings.Contains(logs, "s3cret") {
		t.Errorf("startup log leaks the database password: %s", logs)
	}
	if !strings.Contains(logs, "xxxxx") {
		t.Errorf("startup log does not include the redacted config: %s", logs)
	}
}

func TestAccessLog_RouteThroughFullChain(t *testing.T) {
	var logs testsupport.SyncBuffer
	log := slog.New(slog.NewJSONHandler(&logs, nil))
	h := newHandler(log, readyHealth(), nil)
	for path, route := range map[string]string{"/healthz": "GET /healthz", "/nope": "unmatched"} {
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, path, nil))
		if want := `"route":"` + route + `"`; !strings.Contains(logs.String(), want) {
			t.Errorf("%s: access log missing %s: %s", path, want, logs.String())
		}
	}
}

// TestAPI_HealthcheckFlag checks -healthcheck probes /readyz on the local
// HTTP_ADDR and exits nil only on a 2xx answer. It must not need
// DATABASE_URL: the probe only talks to the running API.
func TestAPI_HealthcheckFlag(t *testing.T) {
	for _, tc := range []struct {
		status  int
		wantErr bool
	}{{http.StatusOK, false}, {http.StatusServiceUnavailable, true}} {
		var gotPath string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotPath = r.URL.Path
			w.WriteHeader(tc.status)
		}))
		env := map[string]string{"HTTP_ADDR": strings.TrimPrefix(srv.URL, "http://")}
		err := execute(t.Context(), []string{"-healthcheck"}, func(k string) string { return env[k] }, io.Discard)
		srv.Close()
		if (err != nil) != tc.wantErr {
			t.Errorf("status %d: execute error = %v, wantErr %v", tc.status, err, tc.wantErr)
		}
		if gotPath != "/readyz" {
			t.Errorf("status %d: probed %q, want /readyz", tc.status, gotPath)
		}
	}
}

func TestAPI_UnknownFlag(t *testing.T) {
	err := execute(t.Context(), []string{"-nope"}, func(string) string { return "" }, io.Discard)
	if err == nil {
		t.Fatal("execute with an unknown flag returned nil, want an error")
	}
}
