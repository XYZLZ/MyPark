package config_test

import (
	"encoding/base64"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/XYZLZ/MyPark/apps/server/internal/config"
)

// b64 returns the base64 of n copies of fill.
func b64(n int, fill byte) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = fill
	}
	return base64.StdEncoding.EncodeToString(b)
}

func TestLoad_SecretboxAndPepperRequired(t *testing.T) {
	_, err := config.Load(rawEnv(map[string]string{
		"DATABASE_URL": "postgres://x", "JWT_SIGNING_KEY": config.DevJWTSigningKey,
		"JWT_KEY_ID": "dev-1", "AUTH_CODE_PEPPER": config.DevAuthCodePepper,
	}))
	if err == nil {
		t.Fatal("want error, got nil")
	}
	for _, name := range []string{"SECRETBOX_KEY", "SECRETBOX_KEY_ID", "CARD_UID_PEPPER"} {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("error %q does not name %s", err, name)
		}
	}
}

func TestLoad_SecretboxKeyMustBe32Bytes(t *testing.T) {
	bad := b64(31, 7)
	_, err := config.Load(env(map[string]string{"DATABASE_URL": "postgres://x", "SECRETBOX_KEY": bad}))
	if err == nil || !strings.Contains(err.Error(), "SECRETBOX_KEY") {
		t.Fatalf("err = %v; want one naming SECRETBOX_KEY", err)
	}
	if strings.Contains(err.Error(), bad) {
		t.Errorf("error echoes the key: %v", err)
	}
}

func TestLoad_CardUIDPepperMinLength(t *testing.T) {
	_, err := config.Load(env(map[string]string{"DATABASE_URL": "postgres://x", "CARD_UID_PEPPER": b64(31, 7)}))
	if err == nil || !strings.Contains(err.Error(), "CARD_UID_PEPPER") {
		t.Fatalf("err = %v; want one naming CARD_UID_PEPPER", err)
	}
}

func TestLoad_OldKeysParsed(t *testing.T) {
	old := b64(32, 5)
	c, err := config.Load(env(map[string]string{
		"DATABASE_URL": "postgres://x", "SECRETBOX_OLD_KEYS": "k0:" + old,
	}))
	if err != nil {
		t.Fatal(err)
	}
	cur, olds := c.SecretboxKeys()
	if cur.ID != "dev-1" || len(olds) != 1 || olds[0].ID != "k0" || olds[0].Secret[0] != 5 {
		t.Errorf("keys = %q, %+v", cur.ID, olds)
	}
	curBytes, _ := base64.StdEncoding.DecodeString(config.DevSecretboxKey)
	if string(cur.Secret[:]) != string(curBytes) {
		t.Error("current key secret mismatch")
	}

	_, err = config.Load(env(map[string]string{
		"DATABASE_URL": "postgres://x", "SECRETBOX_OLD_KEYS": "dev-1:" + old,
	}))
	if err == nil || !strings.Contains(err.Error(), "SECRETBOX_OLD_KEYS") {
		t.Errorf("old key repeating the current id: err = %v", err)
	}
	for _, bad := range []string{"nocolon", ":" + old, "k0:!!notbase64", "k0:" + b64(31, 1), "k0:" + old + ",k0:" + old} {
		_, err = config.Load(env(map[string]string{"DATABASE_URL": "postgres://x", "SECRETBOX_OLD_KEYS": bad}))
		if err == nil || !strings.Contains(err.Error(), "SECRETBOX_OLD_KEYS") {
			t.Errorf("SECRETBOX_OLD_KEYS=%q: err = %v; want one naming it", bad, err)
		}
	}
}

func TestLoad_ProductionRefusesM2DevSecrets(t *testing.T) {
	other := b64(32, 9)
	for name, dev := range map[string]string{
		"SECRETBOX_KEY": config.DevSecretboxKey, "CARD_UID_PEPPER": config.DevCardUIDPepper,
		"SECRETBOX_OLD_KEYS": "k0:" + config.DevSecretboxKey,
	} {
		for label, value := range map[string]string{"exact": dev, "alternate encoding": altPad(t, strings.TrimPrefix(dev, "k0:"))} {
			if strings.HasPrefix(dev, "k0:") && label == "alternate encoding" {
				value = "k0:" + value
			}
			m := map[string]string{
				"DATABASE_URL": "postgres://x", "APP_ENV": "production",
				"JWT_SIGNING_KEY": other, "AUTH_CODE_PEPPER": other,
				"SECRETBOX_KEY": other, "CARD_UID_PEPPER": other,
			}
			m[name] = value
			_, err := config.Load(env(m))
			if err == nil || !strings.Contains(err.Error(), name) {
				t.Fatalf("%s (%s): err = %v; want one naming it", name, label, err)
			}
			if strings.Contains(err.Error(), strings.TrimPrefix(dev, "k0:")) {
				t.Errorf("%s: error echoes the dev value: %v", name, err)
			}
		}
	}
	if _, err := config.Load(env(map[string]string{"DATABASE_URL": "postgres://x", "APP_ENV": "test"})); err != nil {
		t.Errorf("dev secrets rejected outside production: %v", err)
	}
}

func TestParseSecretboxKeys(t *testing.T) {
	cur, old, err := config.ParseSecretboxKeys("k2", b64(32, 2), "k1:"+b64(32, 1), true)
	if err != nil || cur.ID != "k2" || len(old) != 1 || old[0].ID != "k1" {
		t.Fatalf("got %+v %+v %v", cur, old, err)
	}
	if _, _, err := config.ParseSecretboxKeys("", b64(32, 2), "", false); err == nil {
		t.Error("empty key id accepted")
	}
	if _, _, err := config.ParseSecretboxKeys("k", config.DevSecretboxKey, "", true); err == nil {
		t.Error("dev key accepted in production")
	}
}

func TestLoad_M2Defaults(t *testing.T) {
	c, err := config.Load(env(map[string]string{"DATABASE_URL": "postgres://x"}))
	if err != nil {
		t.Fatal(err)
	}
	if c.SSEMaxPerIP() != 20 || c.SSEMaxStreams() != 2000 || c.DeviceRateLimitPerS() != 100 {
		t.Errorf("defaults = %d %d %d; want 20 2000 100", c.SSEMaxPerIP(), c.SSEMaxStreams(), c.DeviceRateLimitPerS())
	}
	pepper, _ := base64.StdEncoding.DecodeString(config.DevCardUIDPepper)
	if string(c.CardUIDPepper()) != string(pepper) {
		t.Error("card uid pepper mismatch")
	}
}

func TestLoad_M2Overrides(t *testing.T) {
	c, err := config.Load(env(map[string]string{
		"DATABASE_URL": "postgres://x", "SSE_MAX_PER_IP": "5", "SSE_MAX_STREAMS": "50", "DEVICE_RATE_LIMIT_PER_S": "10000",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if c.SSEMaxPerIP() != 5 || c.SSEMaxStreams() != 50 || c.DeviceRateLimitPerS() != 10000 {
		t.Errorf("overrides = %d %d %d", c.SSEMaxPerIP(), c.SSEMaxStreams(), c.DeviceRateLimitPerS())
	}
}

func TestLoad_M2LimitsValidated(t *testing.T) {
	for name, value := range map[string]string{
		"DEVICE_RATE_LIMIT_PER_S": "0", "SSE_MAX_PER_IP": "abc", "SSE_MAX_STREAMS": "0",
	} {
		_, err := config.Load(env(map[string]string{"DATABASE_URL": "postgres://x", name: value}))
		if err == nil || !strings.Contains(err.Error(), name) {
			t.Errorf("%s=%s: err = %v; want one naming it", name, value, err)
		}
	}
	_, err := config.Load(env(map[string]string{"DATABASE_URL": "postgres://x", "DEVICE_RATE_LIMIT_PER_S": "10001"}))
	if err == nil || !strings.Contains(err.Error(), "DEVICE_RATE_LIMIT_PER_S") {
		t.Errorf("10001: err = %v", err)
	}
}

func TestConfig_StringRedactsM2Secrets(t *testing.T) {
	c, err := config.Load(env(map[string]string{"DATABASE_URL": "postgres://x", "SECRETBOX_OLD_KEYS": "k0:" + b64(32, 5)}))
	if err != nil {
		t.Fatal(err)
	}
	for label, out := range map[string]string{"%v": fmt.Sprint(c), "%+v": fmt.Sprintf("%+v", c), "%#v": fmt.Sprintf("%#v", c), "LogValue": c.LogValue().String()} {
		for _, leak := range []string{config.DevSecretboxKey, config.DevCardUIDPepper, b64(32, 5)} {
			if strings.Contains(out, leak) {
				t.Errorf("%s leaks %s", label, leak)
			}
		}
	}
}

func TestComposeDefaultsMatchM2DevConstants(t *testing.T) {
	b, err := os.ReadFile("../../../../deploy/compose/docker-compose.yml")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"${SECRETBOX_KEY:-" + config.DevSecretboxKey + "}",
		"${SECRETBOX_KEY_ID:-dev-1}",
		"${SECRETBOX_OLD_KEYS:-}",
		"${CARD_UID_PEPPER:-" + config.DevCardUIDPepper + "}",
	} {
		if !strings.Contains(string(b), want) {
			t.Errorf("compose file lacks %s", want)
		}
	}
}
