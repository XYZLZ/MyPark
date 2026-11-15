package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/XYZLZ/MyPark/apps/server/internal/config"
	"github.com/XYZLZ/MyPark/apps/server/internal/testsupport"
	"github.com/XYZLZ/MyPark/apps/server/internal/testsupport/fakeclock"
)

// simEnv is a valid simulator environment on an ephemeral port.
func simEnv(apiURL string) map[string]string {
	return map[string]string{
		"SIMULATOR_ADDR":          "127.0.0.1:0",
		"SIMULATOR_API_URL":       apiURL,
		"SIMULATOR_FLEET_SECRET":  config.DevSimulatorFleetSecret,
		"SIMULATOR_CONTROL_TOKEN": config.DevSimulatorControlToken,
	}
}

// fakeAPI answers /readyz with 200 while up, 503 otherwise.
func fakeAPI(t *testing.T, up *atomic.Bool) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/readyz" && up.Load() {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(srv.Close)
	return srv
}

type envelope struct {
	Data  map[string]any `json:"data"`
	Error *struct {
		Code string `json:"code"`
	} `json:"error"`
}

func get(t *testing.T, url string, header ...string) (int, envelope) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	for i := 0; i+1 < len(header); i += 2 {
		req.Header.Set(header[i], header[i+1])
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	var body envelope
	_ = json.NewDecoder(resp.Body).Decode(&body)
	return resp.StatusCode, body
}

func TestSimulator_HealthzServes(t *testing.T) {
	var up atomic.Bool
	base := testsupport.StartServer(t, run, simEnv(fakeAPI(t, &up).URL), "simulator listening")
	status, body := get(t, base+"/healthz")
	if status != http.StatusOK || body.Data["status"] != "ok" || body.Data["service"] != "simulator" {
		t.Fatalf("/healthz = %d %v, want 200 ok service=simulator", status, body)
	}
}

func TestSimulator_ControlRequiresTheToken(t *testing.T) {
	var up atomic.Bool
	base := testsupport.StartServer(t, run, simEnv(fakeAPI(t, &up).URL), "simulator listening")
	if status, body := get(t, base+"/control/status"); status != http.StatusUnauthorized || body.Error == nil || body.Error.Code != "simulator.unauthorized" {
		t.Errorf("no token = %d %+v, want 401 simulator.unauthorized", status, body)
	}
	if status, body := get(t, base+"/control/status", "X-Simulator-Token", config.DevSimulatorControlToken); status != http.StatusOK || body.Data["running"] != false {
		t.Errorf("right token = %d %+v, want 200 and an idle status", status, body)
	}
}

func TestRun_ReadyzTracksAPI(t *testing.T) {
	var up atomic.Bool
	up.Store(true)
	fc := fakeclock.New(time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC))
	runFake := func(ctx context.Context, getenv func(string) string, w io.Writer) error {
		return runWith(ctx, getenv, w, fc)
	}
	base := testsupport.StartServer(t, runFake, simEnv(fakeAPI(t, &up).URL), "simulator listening")

	deadline := time.Now().Add(10 * time.Second)
	for {
		status, _ := get(t, base+"/readyz")
		if status == http.StatusOK {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("/readyz = %d with the API up, want 200", status)
		}
		time.Sleep(5 * time.Millisecond)
	}

	up.Store(false)
	fc.Advance(31 * time.Second)
	status, body := get(t, base+"/readyz")
	if status != http.StatusServiceUnavailable || body.Error == nil || body.Error.Code != "health.not_ready" {
		t.Fatalf("/readyz after the API was down 31 s = %d %+v, want 503 health.not_ready", status, body)
	}
}

func TestRun_ReadyzBeforeTheFirstAnswer(t *testing.T) {
	var up atomic.Bool // the API never answers 200
	base := testsupport.StartServer(t, run, simEnv(fakeAPI(t, &up).URL), "simulator listening")
	if status, _ := get(t, base+"/readyz"); status != http.StatusServiceUnavailable {
		t.Errorf("/readyz with the API down = %d, want 503", status)
	}
}

func TestSimulator_InvalidConfig(t *testing.T) {
	for name, tc := range map[string]struct{ key, value string }{
		"addr":  {"SIMULATOR_ADDR", "nope"},
		"token": {"SIMULATOR_CONTROL_TOKEN", "short"},
	} {
		env := simEnv("http://api.test")
		env[tc.key] = tc.value
		err := run(t.Context(), func(k string) string { return env[k] }, io.Discard)
		if err == nil || !strings.Contains(err.Error(), tc.key) {
			t.Errorf("%s: run error = %v, want one naming %s", name, err, tc.key)
		}
		if err != nil && strings.Contains(err.Error(), "short") {
			t.Errorf("%s: the error echoes the value", name)
		}
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
