package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
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

// syncBuffer is a goroutine-safe writer for capturing run's log output.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func doRequest(t *testing.T, h *health.Handler, method, path string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	rec := httptest.NewRecorder()
	newHandler(log, h).ServeHTTP(rec, httptest.NewRequest(method, path, nil))
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
// request_id matches the X-Request-Id response header set by the full chain.
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
	if id == "" || errObj["request_id"] != id {
		t.Errorf("error.request_id = %v, want X-Request-Id %q", errObj["request_id"], id)
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
	if rec.Code != http.StatusOK || body["status"] != "ok" {
		t.Fatalf("status=%d body=%v, want 200 ok", rec.Code, body)
	}
	if rec.Header().Get(middleware.HeaderRequestID) == "" {
		t.Error("missing X-Request-Id on success response")
	}
}

// startRun starts run with env and returns the base URL it listens on. run
// is stopped and must return nil when the test ends.
func startRun(t *testing.T, env map[string]string) string {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	var out syncBuffer
	done := make(chan error, 1)
	go func() { done <- run(ctx, func(k string) string { return env[k] }, &out) }()
	t.Cleanup(func() {
		cancel()
		if err := <-done; err != nil {
			t.Errorf("run returned %v, want nil after shutdown", err)
		}
	})

	deadline := time.Now().Add(5 * time.Second)
	for {
		for line := range strings.Lines(out.String()) {
			var entry struct {
				Msg  string `json:"msg"`
				Addr string `json:"addr"`
			}
			if json.Unmarshal([]byte(line), &entry) == nil && entry.Msg == "api listening" {
				return "http://" + entry.Addr
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("server did not start; output: %s", out.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
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
	if status != http.StatusOK || body["status"] != "ready" {
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
	var out syncBuffer
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
	var logs syncBuffer
	log := slog.New(slog.NewJSONHandler(&logs, nil))
	h := newHandler(log, readyHealth())
	for path, route := range map[string]string{"/healthz": "GET /healthz", "/nope": "unmatched"} {
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, path, nil))
		if want := `"route":"` + route + `"`; !strings.Contains(logs.String(), want) {
			t.Errorf("%s: access log missing %s: %s", path, want, logs.String())
		}
	}
}
