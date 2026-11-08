package jwt_test

import (
	"crypto/ed25519"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	gojwt "github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/XYZLZ/MyPark/apps/server/internal/domain/authz"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/jwt"
	"github.com/XYZLZ/MyPark/apps/server/internal/testsupport"
)

var t0 = time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)

type fixture struct {
	clk  *testsupport.FakeClock
	priv ed25519.PrivateKey
	seed []byte
	s    *jwt.Signer
	v    *jwt.Verifier
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	seed := make([]byte, ed25519.SeedSize)
	for i := range seed {
		seed[i] = byte(i + 1)
	}
	priv := ed25519.NewKeyFromSeed(seed)
	clk := testsupport.NewFakeClock(t0)
	s, err := jwt.NewSigner("dev-1", seed, clk, jwt.AccessTTL)
	if err != nil {
		t.Fatalf("NewSigner: %v", err)
	}
	v := jwt.NewVerifier(map[string]ed25519.PublicKey{"dev-1": priv.Public().(ed25519.PublicKey)}, clk)
	return fixture{clk: clk, priv: priv, seed: seed, s: s, v: v}
}

// forge signs arbitrary claims with the fixture key under kid.
func (f fixture) forge(t *testing.T, kid string, claims gojwt.MapClaims) string {
	t.Helper()
	tok := gojwt.NewWithClaims(gojwt.SigningMethodEdDSA, claims)
	tok.Header["kid"] = kid
	out, err := tok.SignedString(f.priv)
	if err != nil {
		t.Fatalf("forge: %v", err)
	}
	return out
}

func (f fixture) validClaims() gojwt.MapClaims {
	return gojwt.MapClaims{
		"iss":  jwt.Issuer,
		"aud":  jwt.Audience,
		"sub":  uuid.NewString(),
		"sid":  uuid.NewString(),
		"role": "driver",
		"iat":  t0.Unix(),
		"exp":  t0.Add(time.Minute).Unix(),
	}
}

func TestSignVerify_RoundTrip(t *testing.T) {
	f := newFixture(t)
	sub, sid := uuid.New(), uuid.New()
	tok, exp, err := f.s.Sign(sub, sid, authz.RoleAdmin)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if want := t0.Add(15 * time.Minute); !exp.Equal(want) {
		t.Errorf("expiresAt = %v; want %v", exp, want)
	}
	c, err := f.v.Verify(tok)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if c.Subject != sub || c.SessionID != sid || c.Role != authz.RoleAdmin {
		t.Errorf("claims = %+v", c)
	}
	if !c.ExpiresAt.Equal(exp) || !c.IssuedAt.Equal(t0) {
		t.Errorf("times = iat %v exp %v", c.IssuedAt, c.ExpiresAt)
	}
	parsed, _, err := gojwt.NewParser().ParseUnverified(tok, gojwt.MapClaims{})
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Header["kid"] != "dev-1" || parsed.Header["alg"] != "EdDSA" {
		t.Errorf("header = %v", parsed.Header)
	}
}

func TestVerify_RejectsAlgNone(t *testing.T) {
	f := newFixture(t)
	tok := gojwt.NewWithClaims(gojwt.SigningMethodNone, f.validClaims())
	tok.Header["kid"] = "dev-1"
	s, err := tok.SignedString(gojwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.v.Verify(s); !errors.Is(err, jwt.ErrInvalid) {
		t.Fatalf("err = %v; want ErrInvalid", err)
	}
}

func TestVerify_RejectsHS256WithPublicKeyBytes(t *testing.T) {
	f := newFixture(t)
	tok := gojwt.NewWithClaims(gojwt.SigningMethodHS256, f.validClaims())
	tok.Header["kid"] = "dev-1"
	s, err := tok.SignedString([]byte(f.priv.Public().(ed25519.PublicKey)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.v.Verify(s); !errors.Is(err, jwt.ErrInvalid) {
		t.Fatalf("err = %v; want ErrInvalid", err)
	}
}

func TestVerify_RejectsWrongIssuerOrAudience(t *testing.T) {
	f := newFixture(t)
	for _, tc := range []struct {
		name, key string
		val       any
	}{
		{"issuer", "iss", "evil"},
		{"audience", "aud", "other"},
		{"missing issuer", "iss", nil},
		{"missing audience", "aud", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := f.validClaims()
			if tc.val == nil {
				delete(c, tc.key)
			} else {
				c[tc.key] = tc.val
			}
			if _, err := f.v.Verify(f.forge(t, "dev-1", c)); !errors.Is(err, jwt.ErrInvalid) {
				t.Fatalf("err = %v; want ErrInvalid", err)
			}
		})
	}
}

func TestVerify_RejectsExpired(t *testing.T) {
	f := newFixture(t)
	tok, _, err := f.s.Sign(uuid.New(), uuid.New(), authz.RoleDriver)
	if err != nil {
		t.Fatal(err)
	}
	f.clk.Advance(15*time.Minute + 29*time.Second)
	if _, err := f.v.Verify(tok); err != nil {
		t.Fatalf("within leeway: %v", err)
	}
	f.clk.Advance(2 * time.Second) // 15m31s
	if _, err := f.v.Verify(tok); !errors.Is(err, jwt.ErrInvalid) {
		t.Fatalf("err = %v; want ErrInvalid", err)
	}
}

func TestVerify_RejectsMissingExp(t *testing.T) {
	f := newFixture(t)
	c := f.validClaims()
	delete(c, "exp")
	if _, err := f.v.Verify(f.forge(t, "dev-1", c)); !errors.Is(err, jwt.ErrInvalid) {
		t.Fatalf("err = %v; want ErrInvalid", err)
	}
}

func TestVerify_RejectsNotBeforeFuture(t *testing.T) {
	f := newFixture(t)
	c := f.validClaims()
	c["nbf"] = t0.Add(5 * time.Minute).Unix()
	if _, err := f.v.Verify(f.forge(t, "dev-1", c)); !errors.Is(err, jwt.ErrInvalid) {
		t.Fatalf("err = %v; want ErrInvalid", err)
	}
}

func TestVerify_RejectsUnknownKid(t *testing.T) {
	f := newFixture(t)
	for _, kid := range []string{"nope", ""} {
		if _, err := f.v.Verify(f.forge(t, kid, f.validClaims())); !errors.Is(err, jwt.ErrInvalid) {
			t.Fatalf("kid %q: err = %v; want ErrInvalid", kid, err)
		}
	}
}

func TestVerify_RejectsWrongSignature(t *testing.T) {
	f := newFixture(t)
	other := ed25519.NewKeyFromSeed(make([]byte, ed25519.SeedSize))
	tok := gojwt.NewWithClaims(gojwt.SigningMethodEdDSA, f.validClaims())
	tok.Header["kid"] = "dev-1"
	s, _ := tok.SignedString(other)
	if _, err := f.v.Verify(s); !errors.Is(err, jwt.ErrInvalid) {
		t.Fatalf("err = %v; want ErrInvalid", err)
	}
}

func TestVerify_RejectsMissingSidOrBadRole(t *testing.T) {
	f := newFixture(t)
	for _, tc := range []struct {
		name string
		mut  func(gojwt.MapClaims)
	}{
		{"missing sid", func(c gojwt.MapClaims) { delete(c, "sid") }},
		{"bad sid", func(c gojwt.MapClaims) { c["sid"] = "not-a-uuid" }},
		{"missing sub", func(c gojwt.MapClaims) { delete(c, "sub") }},
		{"bad sub", func(c gojwt.MapClaims) { c["sub"] = "x" }},
		{"missing role", func(c gojwt.MapClaims) { delete(c, "role") }},
		{"unknown role", func(c gojwt.MapClaims) { c["role"] = "root" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := f.validClaims()
			tc.mut(c)
			if _, err := f.v.Verify(f.forge(t, "dev-1", c)); !errors.Is(err, jwt.ErrInvalid) {
				t.Fatalf("err = %v; want ErrInvalid", err)
			}
		})
	}
}

func TestVerify_AcceptsRotatedOutKey(t *testing.T) {
	f := newFixture(t)
	oldSeed := make([]byte, ed25519.SeedSize)
	oldSeed[0] = 9
	oldPriv := ed25519.NewKeyFromSeed(oldSeed)
	oldSigner, err := jwt.NewSigner("old-1", oldSeed, f.clk, jwt.AccessTTL)
	if err != nil {
		t.Fatal(err)
	}
	tok, _, err := oldSigner.Sign(uuid.New(), uuid.New(), authz.RoleDriver)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.v.Verify(tok); !errors.Is(err, jwt.ErrInvalid) {
		t.Fatalf("without old key: err = %v; want ErrInvalid", err)
	}
	v := jwt.NewVerifier(map[string]ed25519.PublicKey{
		"dev-1": f.priv.Public().(ed25519.PublicKey),
		"old-1": oldPriv.Public().(ed25519.PublicKey),
	}, f.clk)
	if _, err := v.Verify(tok); err != nil {
		t.Fatalf("with old key: %v", err)
	}
}

func TestVerify_ErrorsDoNotLeakToken(t *testing.T) {
	f := newFixture(t)
	_, err := f.v.Verify("garbage.token.value")
	if !errors.Is(err, jwt.ErrInvalid) || strings.Contains(err.Error(), "garbage") {
		t.Fatalf("err = %v", err)
	}
}

func TestNewSigner_Validation(t *testing.T) {
	clk := testsupport.NewFakeClock(t0)
	if _, err := jwt.NewSigner("k", make([]byte, 31), clk, time.Minute); err == nil {
		t.Error("short seed accepted")
	}
	if _, err := jwt.NewSigner("", make([]byte, 32), clk, time.Minute); err == nil {
		t.Error("empty kid accepted")
	}
	if _, err := jwt.NewSigner("k", make([]byte, 32), clk, 0); err == nil {
		t.Error("zero ttl accepted")
	}
}

func TestSigner_FormattingIsRedacted(t *testing.T) {
	f := newFixture(t)
	for _, verb := range []string{"%v", "%+v", "%#v", "%s"} {
		for _, val := range []any{f.s, *f.s} {
			out := fmt.Sprintf(verb, val)
			if strings.Contains(out, fmt.Sprintf("%x", f.seed)) || strings.Contains(out, fmt.Sprintf("%v", f.seed)) || strings.Contains(out, fmt.Sprintf("%v", []byte(f.priv))) {
				t.Errorf("%s leaks key: %s", verb, out)
			}
			if !strings.Contains(out, "[redacted]") {
				t.Errorf("%s = %q; want redacted", verb, out)
			}
		}
	}
}
