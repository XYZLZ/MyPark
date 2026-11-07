package config_test

import (
	"crypto/ed25519"
	"encoding/base64"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/XYZLZ/MyPark/apps/server/internal/config"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/hashing"
)

func TestLoad_AuthDefaultsRequired(t *testing.T) {
	_, err := config.Load(rawEnv(map[string]string{"DATABASE_URL": "postgres://x"}))
	if err == nil {
		t.Fatal("want error, got nil")
	}
	for _, name := range []string{"JWT_SIGNING_KEY", "JWT_KEY_ID", "AUTH_CODE_PEPPER"} {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("error %q does not name %s", err, name)
		}
	}
}

func TestLoad_AuthAccessors(t *testing.T) {
	c, err := config.Load(env(map[string]string{"DATABASE_URL": "postgres://x"}))
	if err != nil {
		t.Fatal(err)
	}
	seed, _ := base64.StdEncoding.DecodeString(config.DevJWTSigningKey)
	if string(c.JWTSigningSeed()) != string(seed) || c.JWTKeyID() != "dev-1" {
		t.Error("signing seed or key id mismatch")
	}
	pub := ed25519.NewKeyFromSeed(seed).Public().(ed25519.PublicKey)
	keys := c.JWTVerifyKeys()
	if len(keys) != 1 || !keys["dev-1"].Equal(pub) {
		t.Errorf("verify keys = %v; want the signing public key under dev-1", keys)
	}
	pepper, _ := base64.StdEncoding.DecodeString(config.DevAuthCodePepper)
	if string(c.AuthCodePepper()) != string(pepper) {
		t.Error("pepper mismatch")
	}
	if len(c.TrustedProxyCIDRs()) != 0 || c.MaxVehiclesPerUser() != 5 {
		t.Errorf("defaults: cidrs %v, max vehicles %d", c.TrustedProxyCIDRs(), c.MaxVehiclesPerUser())
	}
	// Accessors return copies.
	c.JWTSigningSeed()[0] ^= 0xff
	c.AuthCodePepper()[0] ^= 0xff
	if string(c.JWTSigningSeed()) != string(seed) || string(c.AuthCodePepper()) != string(pepper) {
		t.Error("accessors expose internal slices")
	}
}

func TestLoad_ExtraVerifyKeys(t *testing.T) {
	old := ed25519.NewKeyFromSeed(make([]byte, 32)).Public().(ed25519.PublicKey)
	enc := base64.StdEncoding.EncodeToString(old)
	c, err := config.Load(env(map[string]string{"DATABASE_URL": "postgres://x", "JWT_VERIFY_KEYS": "old-1:" + enc}))
	if err != nil {
		t.Fatal(err)
	}
	if k := c.JWTVerifyKeys(); len(k) != 2 || !k["old-1"].Equal(old) {
		t.Errorf("verify keys = %v", k)
	}
	for _, bad := range []string{"old-1", "old-1:!!!", "old-1:AAAA", ":" + enc, "dev-1:" + enc} {
		_, err := config.Load(env(map[string]string{"DATABASE_URL": "postgres://x", "JWT_VERIFY_KEYS": bad}))
		if err == nil || !strings.Contains(err.Error(), "JWT_VERIFY_KEYS") {
			t.Errorf("JWT_VERIFY_KEYS=%q: err = %v", bad, err)
		}
	}
}

func TestLoad_RejectsShortSeedAndPepper(t *testing.T) {
	short := base64.StdEncoding.EncodeToString(make([]byte, 16))
	long := base64.StdEncoding.EncodeToString(make([]byte, 33))
	for _, tc := range []struct{ name, val string }{
		{"JWT_SIGNING_KEY", short}, {"JWT_SIGNING_KEY", long}, {"JWT_SIGNING_KEY", "!!notbase64"},
		{"AUTH_CODE_PEPPER", short}, {"AUTH_CODE_PEPPER", "!!notbase64"},
	} {
		_, err := config.Load(env(map[string]string{"DATABASE_URL": "postgres://x", tc.name: tc.val}))
		if err == nil || !strings.Contains(err.Error(), tc.name) {
			t.Errorf("%s=%q: err = %v", tc.name, tc.val, err)
			continue
		}
		if strings.Contains(err.Error(), tc.val) {
			t.Errorf("error echoes %s value: %v", tc.name, err)
		}
	}
	// A pepper longer than 32 bytes is fine.
	if _, err := config.Load(env(map[string]string{"DATABASE_URL": "postgres://x", "AUTH_CODE_PEPPER": long})); err != nil {
		t.Errorf("33-byte pepper rejected: %v", err)
	}
}

func TestLoad_PepperAcceptedByCodeHasher(t *testing.T) {
	c, err := config.Load(env(map[string]string{"DATABASE_URL": "postgres://x"}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := hashing.NewCodeHasher(c.AuthCodePepper()); err != nil {
		t.Fatalf("config pepper rejected by the hasher: %v", err)
	}
}

func TestLoad_ProductionRefusesDevKeys(t *testing.T) {
	other := base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef"))
	dev := map[string]string{"JWT_SIGNING_KEY": config.DevJWTSigningKey, "AUTH_CODE_PEPPER": config.DevAuthCodePepper}
	for name, devValue := range dev {
		m := map[string]string{
			"DATABASE_URL": "postgres://x", "APP_ENV": "production",
			"JWT_SIGNING_KEY": other, "AUTH_CODE_PEPPER": other,
		}
		m[name] = devValue
		_, err := config.Load(env(m))
		if err == nil || !strings.Contains(err.Error(), name) {
			t.Fatalf("%s: err = %v; want one naming it", name, err)
		}
		if strings.Contains(err.Error(), devValue) {
			t.Errorf("error echoes the dev value: %v", err)
		}
	}
	// The same dev keys are fine outside production.
	if _, err := config.Load(env(map[string]string{"DATABASE_URL": "postgres://x", "APP_ENV": "test"})); err != nil {
		t.Errorf("dev keys rejected in test env: %v", err)
	}
}

func TestLoad_MaxVehiclesRange(t *testing.T) {
	for _, v := range []string{"0", "21", "x", "-1"} {
		_, err := config.Load(env(map[string]string{"DATABASE_URL": "postgres://x", "MAX_VEHICLES_PER_USER": v}))
		if err == nil || !strings.Contains(err.Error(), "MAX_VEHICLES_PER_USER") {
			t.Errorf("%q: err = %v", v, err)
		}
	}
	for v, want := range map[string]int{"": 5, "1": 1, "20": 20} {
		c, err := config.Load(env(map[string]string{"DATABASE_URL": "postgres://x", "MAX_VEHICLES_PER_USER": v}))
		if err != nil || c.MaxVehiclesPerUser() != want {
			t.Errorf("%q: got %d, %v; want %d", v, c.MaxVehiclesPerUser(), err, want)
		}
	}
}

func TestLoad_TrustedProxyCIDRs(t *testing.T) {
	c, err := config.Load(env(map[string]string{"DATABASE_URL": "postgres://x", "TRUSTED_PROXY_CIDRS": "172.16.0.0/12, 10.0.0.0/8"}))
	if err != nil {
		t.Fatal(err)
	}
	got := c.TrustedProxyCIDRs()
	if len(got) != 2 || got[0].String() != "172.16.0.0/12" || got[1].String() != "10.0.0.0/8" {
		t.Errorf("cidrs = %v", got)
	}
	for _, bad := range []string{"nope", "10.0.0.0", "10.0.0.0/33"} {
		_, err := config.Load(env(map[string]string{"DATABASE_URL": "postgres://x", "TRUSTED_PROXY_CIDRS": bad}))
		if err == nil || !strings.Contains(err.Error(), "TRUSTED_PROXY_CIDRS") {
			t.Errorf("%q: err = %v", bad, err)
		}
	}
}

func TestConfig_StringAndGoStringRedacted(t *testing.T) {
	c, err := config.Load(env(map[string]string{"DATABASE_URL": "postgres://mypark:s3cretpw@db:5432/mypark"}))
	if err != nil {
		t.Fatal(err)
	}
	seed, _ := base64.StdEncoding.DecodeString(config.DevJWTSigningKey)
	pepper, _ := base64.StdEncoding.DecodeString(config.DevAuthCodePepper)
	leaks := []string{
		"s3cretpw", config.DevJWTSigningKey, config.DevAuthCodePepper, config.DevSeedAdminPassword,
		fmt.Sprint(seed), fmt.Sprint(pepper), fmt.Sprintf("%x", seed), fmt.Sprintf("%x", pepper),
	}
	for _, verb := range []string{"%v", "%+v", "%#v", "%s"} {
		for _, val := range []any{c, &c} {
			out := fmt.Sprintf(verb, val)
			for _, l := range leaks {
				if strings.Contains(out, l) {
					t.Errorf("%s leaks %q: %s", verb, l, out)
				}
			}
			if !strings.Contains(out, "xxxxx") {
				t.Errorf("%s = %q; want the redacted database url", verb, out)
			}
		}
	}
}

func TestDevPasswordIsNotCommon(t *testing.T) {
	if len(config.DevSeedAdminPassword) < 10 || hashing.IsCommonPassword(config.DevSeedAdminPassword) {
		t.Fatal("DevSeedAdminPassword must be >= 10 chars and not on the common list")
	}
}

func TestComposeDefaultsMatchDevConstants(t *testing.T) {
	b, err := os.ReadFile("../../../../deploy/compose/docker-compose.yml")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"${JWT_SIGNING_KEY:-" + config.DevJWTSigningKey + "}",
		"${AUTH_CODE_PEPPER:-" + config.DevAuthCodePepper + "}",
	} {
		if !strings.Contains(string(b), want) {
			t.Errorf("compose file lacks %s", want)
		}
	}
	if strings.Contains(string(b), "deploy/compose/.env") {
		t.Error("compose header still mentions deploy/compose/.env")
	}
}
