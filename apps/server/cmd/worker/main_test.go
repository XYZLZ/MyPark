package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/XYZLZ/MyPark/apps/server/internal/testsupport"
)

// dummyDatabaseURL is never dialed by /healthz: the pool connects lazily.
const dummyDatabaseURL = "postgres://mypark_app:x@127.0.0.1:1/mypark?connect_timeout=1"

func TestWorker_HealthzServes(t *testing.T) {
	base := testsupport.StartServer(t, run, map[string]string{
		"WORKER_HEALTH_ADDR": "127.0.0.1:0",
		"DATABASE_URL":       dummyDatabaseURL,
	}, "worker listening")

	resp, err := http.Get(base + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	defer resp.Body.Close()
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode /healthz body: %v", err)
	}
	if resp.StatusCode != http.StatusOK || body["status"] != "ok" || body["service"] != "worker" {
		t.Fatalf("/healthz = %d %v, want 200 ok service=worker", resp.StatusCode, body)
	}
}

// TestWorker_ReadyzDBDown checks the worker reports an unreachable database
// on /readyz instead of failing to boot.
func TestWorker_ReadyzDBDown(t *testing.T) {
	base := testsupport.StartServer(t, run, map[string]string{
		"WORKER_HEALTH_ADDR": "127.0.0.1:0",
		"DATABASE_URL":       dummyDatabaseURL,
	}, "worker listening")

	resp, err := http.Get(base + "/readyz")
	if err != nil {
		t.Fatalf("GET /readyz: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("/readyz status = %d, want 503", resp.StatusCode)
	}
}

func TestWorker_MissingDatabaseURL(t *testing.T) {
	err := run(t.Context(), func(string) string { return "" }, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "DATABASE_URL is required") {
		t.Fatalf("run error = %v, want one naming DATABASE_URL", err)
	}
}

func TestWorker_InvalidHealthAddr(t *testing.T) {
	env := map[string]string{"WORKER_HEALTH_ADDR": "nope", "DATABASE_URL": dummyDatabaseURL}
	err := run(t.Context(), func(k string) string { return env[k] }, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "WORKER_HEALTH_ADDR") {
		t.Fatalf("run error = %v, want one naming WORKER_HEALTH_ADDR", err)
	}
}

func TestWorker_HealthcheckFlag(t *testing.T) {
	for _, tc := range []struct {
		status  int
		wantErr bool
	}{{http.StatusOK, false}, {http.StatusServiceUnavailable, true}} {
		var gotPath string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotPath = r.URL.Path
			w.WriteHeader(tc.status)
		}))
		env := map[string]string{"WORKER_HEALTH_ADDR": strings.TrimPrefix(srv.URL, "http://")}
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
