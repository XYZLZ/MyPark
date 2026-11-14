package devicesig_test

import (
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/XYZLZ/MyPark/apps/server/internal/devicesig"
)

type golden struct {
	SecretHex  string `json:"secret_hex"`
	Method     string `json:"method"`
	URI        string `json:"uri"`
	Timestamp  string `json:"timestamp"`
	Body       string `json:"body"`
	BodySHA256 string `json:"body_sha256"`
	Signature  string `json:"signature"`
}

func loadGolden(t *testing.T) golden {
	t.Helper()
	raw, err := os.ReadFile("testdata/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var g golden
	if err := json.Unmarshal(raw, &g); err != nil {
		t.Fatal(err)
	}
	return g
}

func TestCanonical_GoldenVector(t *testing.T) {
	g := loadGolden(t)
	secret, err := hex.DecodeString(g.SecretHex)
	if err != nil || len(secret) != 32 {
		t.Fatalf("secret_hex: %v (len %d)", err, len(secret))
	}
	canonical := devicesig.Canonical(g.Method, g.URI, g.Timestamp, []byte(g.Body))
	want := g.Method + "\n" + g.URI + "\n" + g.Timestamp + "\n" + g.BodySHA256
	if string(canonical) != want {
		t.Fatalf("canonical = %q, want %q", canonical, want)
	}
	if got := devicesig.Sign(secret, canonical); got != g.Signature {
		t.Errorf("signature = %s, want %s", got, g.Signature)
	}
	if !devicesig.Verify(secret, canonical, g.Signature) {
		t.Error("Verify rejects the golden signature")
	}
}

func TestVerify_Rejects(t *testing.T) {
	g := loadGolden(t)
	secret, _ := hex.DecodeString(g.SecretHex)
	canonical := devicesig.Canonical(g.Method, g.URI, g.Timestamp, []byte(g.Body))
	flipped := []byte(g.Signature)
	flipped[0] ^= 1
	for name, sig := range map[string]string{
		"uppercase": strings.ToUpper(g.Signature),
		"flipped":   string(flipped),
		"short":     g.Signature[:63],
		"empty":     "",
		"not hex":   strings.Repeat("z", 64),
	} {
		if devicesig.Verify(secret, canonical, sig) {
			t.Errorf("%s: Verify accepted %q", name, sig)
		}
	}
	other := append([]byte{}, secret...)
	other[0] ^= 1
	if devicesig.Verify(other, canonical, g.Signature) {
		t.Error("Verify accepted a signature made with another secret")
	}
}

func TestParseTimestamp(t *testing.T) {
	got, ok := devicesig.ParseTimestamp("2026-11-24T14:03:11Z")
	if !ok || !got.Equal(time.Date(2026, 11, 24, 14, 3, 11, 0, time.UTC)) {
		t.Errorf("ParseTimestamp = %v, %v", got, ok)
	}
	for _, s := range []string{
		"2026-11-24T14:03:11.123Z",
		"2026-11-24T10:03:11-04:00",
		"2026-11-24T14:03:11+00:00",
		"2026-11-24 14:03:11Z",
		"2026-11-24T14:03:11z",
		"2026-11-24T4:03:11Z",
		"2026-02-30T14:03:11Z",
		"2026-11-24T24:00:00Z",
		"",
	} {
		if _, ok := devicesig.ParseTimestamp(s); ok {
			t.Errorf("ParseTimestamp(%q) accepted", s)
		}
	}
}

func TestWellFormedSignature(t *testing.T) {
	ok := strings.Repeat("0a", 32)
	if !devicesig.WellFormedSignature(ok) {
		t.Errorf("rejected %q", ok)
	}
	for _, s := range []string{ok[:63], ok + "0", strings.ToUpper(ok), strings.Repeat("g", 64), ""} {
		if devicesig.WellFormedSignature(s) {
			t.Errorf("accepted %q", s)
		}
	}
}
