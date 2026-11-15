package config_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/XYZLZ/MyPark/apps/server/internal/config"
)

func fleetEnv(extra map[string]string) func(string) string {
	env := map[string]string{
		"SEED_SIMULATOR_FLEET":   "true",
		"SECRETBOX_KEY":          config.DevSecretboxKey,
		"SECRETBOX_KEY_ID":       "k1",
		"CARD_UID_PEPPER":        config.DevCardUIDPepper,
		"SIMULATOR_FLEET_SECRET": config.DevSimulatorFleetSecret,
	}
	for k, v := range extra {
		env[k] = v
	}
	return func(k string) string { return env[k] }
}

func TestLoadSeedFleet_DisabledByDefaultIgnoresOtherVariables(t *testing.T) {
	for _, v := range []string{"", "false", "0"} {
		f, err := config.LoadSeedFleet(func(k string) string {
			return map[string]string{"SEED_SIMULATOR_FLEET": v, "SIMULATOR_FLEET_SECRET": "!!"}[k]
		})
		if err != nil || f.Enabled() {
			t.Errorf("SEED_SIMULATOR_FLEET=%q: enabled=%v err=%v, want disabled and no error", v, f.Enabled(), err)
		}
	}
}

func TestLoadSeedFleet_EnabledExposesAccessors(t *testing.T) {
	f, err := config.LoadSeedFleet(fleetEnv(nil))
	if err != nil || !f.Enabled() {
		t.Fatalf("LoadSeedFleet: enabled=%v err=%v", f.Enabled(), err)
	}
	cur, old := f.SecretboxKeys()
	if cur.ID != "k1" || len(old) != 0 {
		t.Errorf("SecretboxKeys = %q, %d old keys", cur.ID, len(old))
	}
	if len(f.CardUIDPepper()) != 32 || len(f.FleetSecret().Reveal()) != 32 {
		t.Errorf("pepper %d bytes, fleet secret %d bytes, want 32 each", len(f.CardUIDPepper()), len(f.FleetSecret().Reveal()))
	}
}

func TestLoadSeedFleet_Rejects(t *testing.T) {
	cases := []struct {
		name, want string
		env        map[string]string
	}{
		{"invalid flag", "SEED_SIMULATOR_FLEET", map[string]string{"SEED_SIMULATOR_FLEET": "maybe"}},
		{"missing fleet secret", "SIMULATOR_FLEET_SECRET is required", map[string]string{"SIMULATOR_FLEET_SECRET": ""}},
		{"fleet secret not base64", "SIMULATOR_FLEET_SECRET must be base64", map[string]string{"SIMULATOR_FLEET_SECRET": "!!!"}},
		{"fleet secret 31 bytes", "SIMULATOR_FLEET_SECRET must decode to at least 32 bytes", map[string]string{"SIMULATOR_FLEET_SECRET": b64(31, 7)}},
		{"missing pepper", "CARD_UID_PEPPER is required", map[string]string{"CARD_UID_PEPPER": ""}},
		{"short pepper", "CARD_UID_PEPPER must decode to at least 32 bytes", map[string]string{"CARD_UID_PEPPER": b64(16, 7)}},
		{"missing secretbox key", "SECRETBOX_KEY", map[string]string{"SECRETBOX_KEY": ""}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := config.LoadSeedFleet(fleetEnv(tc.env))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want one containing %q", err, tc.want)
			}
			if strings.Contains(err.Error(), b64(16, 7)) || strings.Contains(err.Error(), b64(31, 7)) {
				t.Errorf("error echoes a value: %v", err)
			}
		})
	}
}

func TestLoadSeedFleet_ProductionRefusesDevFleetSecret(t *testing.T) {
	prod := map[string]string{
		"APP_ENV":                "production",
		"SECRETBOX_KEY":          b64(32, 1),
		"CARD_UID_PEPPER":        b64(32, 2),
		"SIMULATOR_FLEET_SECRET": config.DevSimulatorFleetSecret,
	}
	_, err := config.LoadSeedFleet(fleetEnv(prod))
	if err == nil || !strings.Contains(err.Error(), "SIMULATOR_FLEET_SECRET must not be the development default in production") {
		t.Fatalf("err = %v, want the production refusal", err)
	}
	prod["SIMULATOR_FLEET_SECRET"] = b64(32, 3)
	if _, err := config.LoadSeedFleet(fleetEnv(prod)); err != nil {
		t.Errorf("production with real values: %v", err)
	}
	// Dev pepper in production is refused too.
	prod["CARD_UID_PEPPER"] = config.DevCardUIDPepper
	if _, err := config.LoadSeedFleet(fleetEnv(prod)); err == nil || !strings.Contains(err.Error(), "CARD_UID_PEPPER") {
		t.Errorf("dev pepper in production: err = %v", err)
	}
}

func TestSeedFleet_RedactsEverywhere(t *testing.T) {
	f, err := config.LoadSeedFleet(fleetEnv(nil))
	if err != nil {
		t.Fatal(err)
	}
	for _, out := range []string{fmt.Sprintf("%v", f), fmt.Sprintf("%+v", f), fmt.Sprintf("%#v", f), f.String()} {
		if strings.Contains(out, config.DevSimulatorFleetSecret) || strings.Contains(out, "k1") {
			t.Errorf("formatted SeedFleet leaks: %q", out)
		}
	}
}
