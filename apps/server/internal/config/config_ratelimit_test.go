package config_test

import (
	"encoding/base64"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/XYZLZ/MyPark/apps/server/internal/config"
)

// RATE_LIMIT_PEPPER keys the hashed rate-limit keys that reach the logs and
// the rate_limit_counters table, so a logged IPv4 hash cannot be reversed.
func TestLoad_RateLimitPepper(t *testing.T) {
	c, err := config.Load(env(map[string]string{"DATABASE_URL": "postgres://x"}))
	if err != nil {
		t.Fatal(err)
	}
	want, _ := base64.StdEncoding.DecodeString(config.DevRateLimitPepper)
	if string(c.RateLimitPepper()) != string(want) {
		t.Error("rate limit pepper mismatch")
	}

	m := map[string]string{"DATABASE_URL": "postgres://x", "JWT_SIGNING_KEY": config.DevJWTSigningKey, "JWT_KEY_ID": "dev-1",
		"AUTH_CODE_PEPPER": config.DevAuthCodePepper, "SECRETBOX_KEY": config.DevSecretboxKey, "SECRETBOX_KEY_ID": "dev-1",
		"CARD_UID_PEPPER": config.DevCardUIDPepper}
	if _, err := config.Load(rawEnv(m)); err == nil || !strings.Contains(err.Error(), "RATE_LIMIT_PEPPER is required") {
		t.Errorf("missing: err = %v; want RATE_LIMIT_PEPPER is required", err)
	}
	short := b64(31, 7)
	_, err = config.Load(env(map[string]string{"DATABASE_URL": "postgres://x", "RATE_LIMIT_PEPPER": short}))
	if err == nil || !strings.Contains(err.Error(), "RATE_LIMIT_PEPPER") || strings.Contains(err.Error(), short) {
		t.Errorf("short: err = %v; want one naming RATE_LIMIT_PEPPER without the value", err)
	}

	other := b64(32, 9)
	prod := map[string]string{
		"DATABASE_URL": "postgres://x", "APP_ENV": "production", "JWT_SIGNING_KEY": other, "AUTH_CODE_PEPPER": other,
		"SECRETBOX_KEY": other, "CARD_UID_PEPPER": other, "RATE_LIMIT_PEPPER": config.DevRateLimitPepper,
	}
	_, err = config.Load(env(prod))
	if err == nil || !strings.Contains(err.Error(), "RATE_LIMIT_PEPPER must not be the development default") {
		t.Errorf("production with the dev pepper: err = %v", err)
	}
	prod["RATE_LIMIT_PEPPER"] = other
	if _, err := config.Load(env(prod)); err != nil {
		t.Errorf("production with a real pepper: %v", err)
	}

	for label, out := range map[string]string{"%v": fmt.Sprint(c), "%#v": fmt.Sprintf("%#v", c), "LogValue": c.LogValue().String()} {
		if strings.Contains(out, config.DevRateLimitPepper) {
			t.Errorf("%s leaks the rate limit pepper", label)
		}
	}
	if !strings.Contains(c.LogValue().String(), "rate_limit_pepper=[redacted]") {
		t.Errorf("LogValue = %s, want rate_limit_pepper redacted", c.LogValue())
	}
}

func TestComposeDefaultsMatchRateLimitPepper(t *testing.T) {
	b, err := os.ReadFile("../../../../deploy/compose/docker-compose.yml")
	if err != nil {
		t.Fatal(err)
	}
	if want := "${RATE_LIMIT_PEPPER:-" + config.DevRateLimitPepper + "}"; !strings.Contains(string(b), want) {
		t.Errorf("compose file lacks %s", want)
	}
}
