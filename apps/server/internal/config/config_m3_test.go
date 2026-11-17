package config_test

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/XYZLZ/MyPark/apps/server/internal/config"
)

func devQRKey(t *testing.T) []byte {
	t.Helper()
	b, err := base64.StdEncoding.DecodeString(config.DevQRHMACKey)
	if err != nil || len(b) != 32 {
		t.Fatalf("DevQRHMACKey is not 32 bytes of base64: %v", err)
	}
	return b
}

func TestLoad_M3Defaults(t *testing.T) {
	c, err := config.Load(env(map[string]string{"DATABASE_URL": "postgres://x"}))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !bytes.Equal(c.QRHMACKey(), devQRKey(t)) {
		t.Error("QRHMACKey is not the development default")
	}
	if c.PaymentChargeTimeout() != 5*time.Second || c.SimGatewayLatency() != 300*time.Millisecond ||
		c.SimGatewayTimeoutDelay() != 30*time.Second || c.PaymentGateway() != "simulated" {
		t.Errorf("defaults wrong: %v", c)
	}
}

func TestLoad_M3Overrides(t *testing.T) {
	key := b64(32, 9)
	c, err := config.Load(env(map[string]string{
		"DATABASE_URL": "postgres://x", "QR_HMAC_KEY": key, "PAYMENT_GATEWAY": "simulated",
		"PAYMENT_CHARGE_TIMEOUT": "10s", "SIM_GATEWAY_LATENCY": "0s", "SIM_GATEWAY_TIMEOUT_DELAY": "2m",
	}))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := base64.StdEncoding.EncodeToString(c.QRHMACKey()); got != key {
		t.Error("QRHMACKey override not applied")
	}
	if c.PaymentChargeTimeout() != 10*time.Second || c.SimGatewayLatency() != 0 || c.SimGatewayTimeoutDelay() != 2*time.Minute {
		t.Errorf("overrides wrong: %v", c)
	}
}

func TestLoad_M3Validation(t *testing.T) {
	short := b64(31, 7)
	tests := []struct {
		name, key, val string
	}{
		{"short QR key", "QR_HMAC_KEY", short},
		{"QR key not base64", "QR_HMAC_KEY", "%%%"},
		{"timeout too long", "PAYMENT_CHARGE_TIMEOUT", "11s"},
		{"timeout too short", "PAYMENT_CHARGE_TIMEOUT", "500ms"},
		{"timeout not a duration", "PAYMENT_CHARGE_TIMEOUT", "fast"},
		{"latency too long", "SIM_GATEWAY_LATENCY", "6s"},
		{"latency negative", "SIM_GATEWAY_LATENCY", "-1s"},
		{"timeout delay too short", "SIM_GATEWAY_TIMEOUT_DELAY", "999ms"},
		{"timeout delay too long", "SIM_GATEWAY_TIMEOUT_DELAY", "121s"},
		{"unknown gateway", "PAYMENT_GATEWAY", "stripe"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := config.Load(env(map[string]string{"DATABASE_URL": "postgres://x", tt.key: tt.val}))
			if err == nil || !strings.Contains(err.Error(), tt.key) {
				t.Fatalf("err = %v; want one naming %s", err, tt.key)
			}
			if tt.key == "QR_HMAC_KEY" && strings.Contains(err.Error(), tt.val) {
				t.Errorf("error echoes the key: %v", err)
			}
		})
	}
}

// prodEnv is a production environment with real (non-development) secrets.
func prodEnv(extra map[string]string) map[string]string {
	other := b64(32, 3)
	m := map[string]string{
		"DATABASE_URL": "postgres://x", "APP_ENV": "production",
		"JWT_SIGNING_KEY": other, "AUTH_CODE_PEPPER": other, "SECRETBOX_KEY": other,
		"CARD_UID_PEPPER": other, "RATE_LIMIT_PEPPER": other,
		"QR_HMAC_KEY": other, "PAYMENT_GATEWAY": "simulated",
	}
	for k, v := range extra {
		m[k] = v
	}
	return m
}

func TestLoad_M3Production(t *testing.T) {
	if _, err := config.Load(env(prodEnv(nil))); err != nil {
		t.Fatalf("production with real values: %v", err)
	}
	_, err := config.Load(env(prodEnv(map[string]string{"QR_HMAC_KEY": config.DevQRHMACKey})))
	if err == nil || !strings.Contains(err.Error(), "QR_HMAC_KEY must not be the development default in production") {
		t.Errorf("dev QR key in production: err = %v", err)
	}
	_, err = config.Load(env(prodEnv(map[string]string{"QR_HMAC_KEY": ""})))
	if err == nil || !strings.Contains(err.Error(), "QR_HMAC_KEY") {
		t.Errorf("unset QR key in production: err = %v", err)
	}
	_, err = config.Load(env(prodEnv(map[string]string{"PAYMENT_GATEWAY": ""})))
	if err == nil || !strings.Contains(err.Error(), "PAYMENT_GATEWAY") {
		t.Errorf("unset gateway in production: err = %v", err)
	}
}

func TestLoadWorker_M3(t *testing.T) {
	c, err := config.LoadWorker(workerEnv(nil))
	if err != nil {
		t.Fatalf("LoadWorker: %v", err)
	}
	if !bytes.Equal(c.QRHMACKey(), devQRKey(t)) || c.PaymentChargeTimeout() != 5*time.Second ||
		c.SimGatewayLatency() != 300*time.Millisecond || c.SimGatewayTimeoutDelay() != 30*time.Second ||
		c.PaymentGateway() != "simulated" {
		t.Errorf("worker defaults wrong: %v", c)
	}

	for _, tt := range []struct{ key, val string }{
		{"QR_HMAC_KEY", b64(31, 7)},
		{"PAYMENT_CHARGE_TIMEOUT", "11s"},
		{"SIM_GATEWAY_LATENCY", "6s"},
		{"SIM_GATEWAY_TIMEOUT_DELAY", "500ms"},
		{"PAYMENT_GATEWAY", "stripe"},
	} {
		if _, err := config.LoadWorker(workerEnv(map[string]string{tt.key: tt.val})); err == nil || !strings.Contains(err.Error(), tt.key) {
			t.Errorf("%s=%q: err = %v", tt.key, tt.val, err)
		}
	}

	prod := map[string]string{"APP_ENV": "production", "QR_HMAC_KEY": b64(32, 3), "PAYMENT_GATEWAY": "simulated"}
	if _, err := config.LoadWorker(workerEnv(prod)); err != nil {
		t.Errorf("worker production with real values: %v", err)
	}
	_, err = config.LoadWorker(workerEnv(map[string]string{"APP_ENV": "production", "QR_HMAC_KEY": config.DevQRHMACKey, "PAYMENT_GATEWAY": "simulated"}))
	if err == nil || !strings.Contains(err.Error(), "QR_HMAC_KEY must not be the development default in production") {
		t.Errorf("worker dev QR key in production: err = %v", err)
	}
	if _, err = config.LoadWorker(workerEnv(map[string]string{"APP_ENV": "production", "QR_HMAC_KEY": b64(32, 3)})); err == nil || !strings.Contains(err.Error(), "PAYMENT_GATEWAY") {
		t.Errorf("worker unset gateway in production: err = %v", err)
	}
}

func TestConfig_LogValueRedactsQRKey(t *testing.T) {
	key := b64(32, 9)
	c, err := config.Load(env(map[string]string{"DATABASE_URL": "postgres://x", "QR_HMAC_KEY": key}))
	if err != nil {
		t.Fatal(err)
	}
	w, err := config.LoadWorker(workerEnv(map[string]string{"QR_HMAC_KEY": key}))
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	slog.New(slog.NewJSONHandler(&buf, nil)).Info("cfg", "config", c, "worker", w)
	for name, out := range map[string]string{
		"Sprint": fmt.Sprint(c), "%#v": fmt.Sprintf("%#v", c), "slog": buf.String(),
		"worker Sprint": fmt.Sprint(w), "worker %#v": fmt.Sprintf("%#v", w),
	} {
		if strings.Contains(out, key) {
			t.Errorf("%s output contains the QR key: %s", name, out)
		}
	}
	if !strings.Contains(fmt.Sprint(c), "qr_hmac_key=[redacted]") {
		t.Errorf("Config does not print qr_hmac_key: [redacted]: %v", c)
	}
}
