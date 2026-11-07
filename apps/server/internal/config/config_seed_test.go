package config_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/XYZLZ/MyPark/apps/server/internal/config"
)

func seedEnv(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestLoadSeedAdmin_UnsetSkips_ProductionRefusesDevPassword(t *testing.T) {
	// Unset email: skip, no error, in any environment.
	for _, appEnv := range []string{"", "development", "production"} {
		_, ok, err := config.LoadSeedAdmin(seedEnv(map[string]string{"APP_ENV": appEnv}))
		if ok || err != nil {
			t.Errorf("APP_ENV=%q unset: ok=%v err=%v, want skip", appEnv, ok, err)
		}
	}
	full := func(appEnv, pw string) map[string]string {
		return map[string]string{
			"APP_ENV": appEnv, "SEED_ADMIN_EMAIL": " Admin@MyPark.local ", "SEED_ADMIN_FIRST_NAME": " MyPark ",
			"SEED_ADMIN_LAST_NAME": "Admin", "SEED_ADMIN_PASSWORD": pw,
		}
	}
	s, ok, err := config.LoadSeedAdmin(seedEnv(full("development", config.DevSeedAdminPassword)))
	if err != nil || !ok {
		t.Fatalf("development with the dev password: ok=%v err=%v", ok, err)
	}
	if s.Email() != "admin@mypark.local" || s.FirstName() != "MyPark" || s.LastName() != "Admin" || s.Password().Reveal() != config.DevSeedAdminPassword {
		t.Errorf("accessors = %q %q %q", s.Email(), s.FirstName(), s.LastName())
	}
	_, ok, err = config.LoadSeedAdmin(seedEnv(full("production", config.DevSeedAdminPassword)))
	if ok || err == nil || !strings.Contains(err.Error(), "SEED_ADMIN_PASSWORD") {
		t.Fatalf("production with the dev password: ok=%v err=%v, want a refusal naming the variable", ok, err)
	}
	if strings.Contains(err.Error(), config.DevSeedAdminPassword) {
		t.Errorf("error echoes the password: %v", err)
	}
	if _, ok, err := config.LoadSeedAdmin(seedEnv(full("production", "A-real-prod-secret-91"))); err != nil || !ok {
		t.Errorf("production with its own password: ok=%v err=%v", ok, err)
	}
}

func TestLoadSeedAdmin_ValidationNamesVariablesWithoutValues(t *testing.T) {
	base := func() map[string]string {
		return map[string]string{
			"SEED_ADMIN_EMAIL": "admin@mypark.local", "SEED_ADMIN_FIRST_NAME": "MyPark",
			"SEED_ADMIN_LAST_NAME": "Admin", "SEED_ADMIN_PASSWORD": "Some-long-secret-77",
		}
	}
	for name, tc := range map[string]struct {
		key, val, want string
	}{
		"bad email":      {"SEED_ADMIN_EMAIL", "not-an-email", "SEED_ADMIN_EMAIL"},
		"missing first":  {"SEED_ADMIN_FIRST_NAME", "", "SEED_ADMIN_FIRST_NAME"},
		"blank last":     {"SEED_ADMIN_LAST_NAME", "   ", "SEED_ADMIN_LAST_NAME"},
		"long first":     {"SEED_ADMIN_FIRST_NAME", strings.Repeat("a", 61), "SEED_ADMIN_FIRST_NAME"},
		"long last":      {"SEED_ADMIN_LAST_NAME", strings.Repeat("a", 81), "SEED_ADMIN_LAST_NAME"},
		"missing secret": {"SEED_ADMIN_PASSWORD", "", "SEED_ADMIN_PASSWORD"},
	} {
		m := base()
		m[tc.key] = tc.val
		_, ok, err := config.LoadSeedAdmin(seedEnv(m))
		if ok || err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: ok=%v err=%v", name, ok, err)
			continue
		}
		if strings.Contains(err.Error(), "Some-long-secret-77") || strings.Contains(err.Error(), "not-an-email") {
			t.Errorf("%s: error echoes a value: %v", name, err)
		}
	}
}

func TestSeedAdmin_Redacted(t *testing.T) {
	s, ok, err := config.LoadSeedAdmin(seedEnv(map[string]string{
		"SEED_ADMIN_EMAIL": "admin@mypark.local", "SEED_ADMIN_FIRST_NAME": "MyPark",
		"SEED_ADMIN_LAST_NAME": "Admin", "SEED_ADMIN_PASSWORD": config.DevSeedAdminPassword,
	}))
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
	for _, verb := range []string{"%v", "%+v", "%#v", "%s"} {
		for _, val := range []any{s, &s} {
			out := fmt.Sprintf(verb, val)
			if strings.Contains(out, config.DevSeedAdminPassword) || strings.Contains(out, "admin@mypark.local") {
				t.Errorf("%s leaks: %s", verb, out)
			}
		}
	}
}
