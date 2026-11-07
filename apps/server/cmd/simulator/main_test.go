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

func TestSimulator_HealthzServes(t *testing.T) {
	base := testsupport.StartServer(t, run, map[string]string{"SIMULATOR_ADDR": "127.0.0.1:0"}, "simulator listening")

	resp, err := http.Get(base + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	defer resp.Body.Close()
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode /healthz body: %v", err)
	}
	data, _ := body["data"].(map[string]any)
	if resp.StatusCode != http.StatusOK || data["status"] != "ok" || data["service"] != "simulator" {
		t.Fatalf("/healthz = %d %v, want 200 ok service=simulator", resp.StatusCode, body)
	}
}

// TestSimulator_NoReadyz checks the simulator, which has no dependencies,
// exposes only the liveness probe.
func TestSimulator_NoReadyz(t *testing.T) {
	base := testsupport.StartServer(t, run, map[string]string{"SIMULATOR_ADDR": "127.0.0.1:0"}, "simulator listening")

	resp, err := http.Get(base + "/readyz")
	if err != nil {
		t.Fatalf("GET /readyz: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("/readyz status = %d, want 404", resp.StatusCode)
	}
}

func TestSimulator_InvalidAddr(t *testing.T) {
	env := map[string]string{"SIMULATOR_ADDR": "nope"}
	err := run(t.Context(), func(k string) string { return env[k] }, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "SIMULATOR_ADDR") {
		t.Fatalf("run error = %v, want one naming SIMULATOR_ADDR", err)
	}
}

func TestSimulator_HealthcheckFlag(t *testing.T) {
	for _, tc := range []struct {
		status  int
		wantErr bool
	}{{http.StatusOK, false}, {http.StatusServiceUnavailable, true}} {
		var gotPath string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotPath = r.URL.Path
			w.WriteHeader(tc.status)
		}))
		env := map[string]string{"SIMULATOR_ADDR": strings.TrimPrefix(srv.URL, "http://")}
		err := execute(t.Context(), []string{"-healthcheck"}, func(k string) string { return env[k] }, io.Discard)
		srv.Close()
		if (err != nil) != tc.wantErr {
			t.Errorf("status %d: execute error = %v, wantErr %v", tc.status, err, tc.wantErr)
		}
		if gotPath != "/healthz" {
			t.Errorf("status %d: probed %q, want /healthz", tc.status, gotPath)
		}
	}
}
