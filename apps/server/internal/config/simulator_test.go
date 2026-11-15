package config_test

import (
	"bytes"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"testing"

	"github.com/XYZLZ/MyPark/apps/server/internal/config"
)

func simEnv(extra map[string]string) func(string) string {
	env := map[string]string{
		"SIMULATOR_FLEET_SECRET":  config.DevSimulatorFleetSecret,
		"SIMULATOR_CONTROL_TOKEN": config.DevSimulatorControlToken,
	}
	for k, v := range extra {
		env[k] = v
	}
	return func(k string) string { return env[k] }
}

func TestLoadSimulator(t *testing.T) {
	c, err := config.LoadSimulator(simEnv(nil))
	if err != nil {
		t.Fatalf("LoadSimulator defaults: %v", err)
	}
	if c.Addr() != ":8090" || c.APIURL() != "http://api:8080" || c.Env() != "development" {
		t.Errorf("defaults: addr %q api %q env %q", c.Addr(), c.APIURL(), c.Env())
	}
	if len(c.FleetSecret().Reveal()) != 32 || c.ControlToken().Reveal() != config.DevSimulatorControlToken {
		t.Errorf("secrets not loaded")
	}

	cases := []struct {
		name string
		env  map[string]string
		want []string
	}{
		{"missing token", map[string]string{"SIMULATOR_CONTROL_TOKEN": ""}, []string{"SIMULATOR_CONTROL_TOKEN is required"}},
		{"short token", map[string]string{"SIMULATOR_CONTROL_TOKEN": strings.Repeat("x", 31)}, []string{"SIMULATOR_CONTROL_TOKEN must be at least 32 bytes"}},
		{"missing fleet secret", map[string]string{"SIMULATOR_FLEET_SECRET": ""}, []string{"SIMULATOR_FLEET_SECRET is required"}},
		{"short fleet secret", map[string]string{"SIMULATOR_FLEET_SECRET": b64(31, 3)}, []string{"SIMULATOR_FLEET_SECRET must decode to at least 32 bytes"}},
		{"padded token", map[string]string{"SIMULATOR_CONTROL_TOKEN": " " + strings.Repeat("p", 40) + "	"}, []string{"SIMULATOR_CONTROL_TOKEN must not have leading or trailing whitespace"}},
		{"bad api url", map[string]string{"SIMULATOR_API_URL": "not a url"}, []string{"SIMULATOR_API_URL"}},
		{"bad addr", map[string]string{"SIMULATOR_ADDR": "nope"}, []string{"SIMULATOR_ADDR"}},
		{"bad env", map[string]string{"APP_ENV": "prod"}, []string{"APP_ENV"}},
		{"production dev values", map[string]string{"APP_ENV": "production"}, []string{
			"SIMULATOR_FLEET_SECRET must not be the development default in production",
			"SIMULATOR_CONTROL_TOKEN must not be the development default in production",
		}},
	}
	for _, tc := range cases {
		_, err := config.LoadSimulator(simEnv(tc.env))
		if err == nil {
			t.Errorf("%s: no error", tc.name)
			continue
		}
		for _, w := range tc.want {
			if !strings.Contains(err.Error(), w) {
				t.Errorf("%s: error %q does not contain %q", tc.name, err, w)
			}
		}
		for k, v := range tc.env {
			secretVar := k == "SIMULATOR_FLEET_SECRET" || k == "SIMULATOR_CONTROL_TOKEN"
			if secretVar && len(v) >= 8 && strings.Contains(err.Error(), v) {
				t.Errorf("%s: error echoes the value", tc.name)
			}
		}
	}

	// Production with real secrets loads.
	if _, err := config.LoadSimulator(simEnv(map[string]string{
		"APP_ENV": "production", "SIMULATOR_FLEET_SECRET": b64(32, 9), "SIMULATOR_CONTROL_TOKEN": strings.Repeat("t", 40),
	})); err != nil {
		t.Errorf("production with real secrets: %v", err)
	}
}

func TestSimulatorConfig_Redacts(t *testing.T) {
	c, err := config.LoadSimulator(simEnv(nil))
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	slog.New(slog.NewJSONHandler(&buf, nil)).Info("cfg", "config", c)
	out := fmt.Sprintf("%v %+v %#v %s", c, c, c, c) + buf.String()
	for _, s := range []string{config.DevSimulatorControlToken, config.DevSimulatorFleetSecret, "s0oW01SL"} {
		if strings.Contains(out, s) {
			t.Errorf("a secret leaked: %s", out)
		}
	}
	if !strings.Contains(buf.String(), "http://api:8080") {
		t.Errorf("LogValue lacks the non-secret fields: %s", buf.String())
	}
}

// The compose defaults are the dev constants, so production's refusal covers
// them.
func TestSimulatorComposeDefaults(t *testing.T) {
	b, err := os.ReadFile("../../../../deploy/compose/docker-compose.yml")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"${SIMULATOR_FLEET_SECRET:-" + config.DevSimulatorFleetSecret + "}",
		"${SIMULATOR_CONTROL_TOKEN:-" + config.DevSimulatorControlToken + "}",
		"SIMULATOR_API_URL: http://api:8080",
		"SIMULATOR_INTERNAL_URL: http://simulator:8090",
	} {
		if !strings.Contains(string(b), want) {
			t.Errorf("compose file lacks %s", want)
		}
	}
}

func TestLoadSimulator_TrimsTheAPIURLSlash(t *testing.T) {
	c, err := config.LoadSimulator(simEnv(map[string]string{"SIMULATOR_API_URL": "http://api:8080/"}))
	if err != nil {
		t.Fatal(err)
	}
	if c.APIURL() != "http://api:8080" {
		t.Errorf("APIURL = %q, want no trailing slash", c.APIURL())
	}
}
