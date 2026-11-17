package reservations_test

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/XYZLZ/MyPark/apps/server/internal/features/reservations"
)

type qrGolden struct {
	KeyHex   string `json:"key_hex"`
	PublicID string `json:"public_id"`
	NonceHex string `json:"nonce_hex"`
	Token    string `json:"token"`
}

func loadGolden(t *testing.T) (key []byte, id uuid.UUID, nonce [16]byte, token string) {
	t.Helper()
	raw, err := os.ReadFile("testdata/qr_golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var g qrGolden
	if err := json.Unmarshal(raw, &g); err != nil {
		t.Fatal(err)
	}
	key, _ = hex.DecodeString(g.KeyHex)
	id = uuid.MustParse(g.PublicID)
	n, _ := hex.DecodeString(g.NonceHex)
	copy(nonce[:], n)
	return key, id, nonce, g.Token
}

// The token was computed outside Go (node crypto, HMAC-SHA256 over
// public_id || nonce, base64url without padding) and frozen.
func TestQR_GoldenVector(t *testing.T) {
	key, id, nonce, want := loadGolden(t)
	got := reservations.EncodeQR(key, id, nonce)
	if got != want {
		t.Errorf("token = %s, want %s", got, want)
	}
	if len(got) != 86 {
		t.Errorf("len = %d, want 86", len(got))
	}
	gotID, gotNonce, ok := reservations.DecodeQR(key, want)
	if !ok || gotID != id || gotNonce != nonce {
		t.Errorf("decode golden = %v %x %v", gotID, gotNonce, ok)
	}
}

func TestQR_RoundTripAndTamper(t *testing.T) {
	key, id, nonce, token := loadGolden(t)
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(raw) != 64 {
		t.Fatalf("golden decodes to %d bytes: %v", len(raw), err)
	}

	for i := range raw {
		b := append([]byte(nil), raw...)
		b[i] ^= 0x01
		if _, _, ok := reservations.DecodeQR(key, base64.RawURLEncoding.EncodeToString(b)); ok {
			t.Errorf("flipping byte %d still decodes", i)
		}
	}

	otherKey := append([]byte(nil), key...)
	otherKey[0] ^= 0xff
	cases := map[string]struct {
		key   []byte
		token string
	}{
		"wrong key":      {otherKey, token},
		"85 chars":       {key, token[:85]},
		"87 chars":       {key, token + "A"},
		"padding":        {key, token[:84] + "=="},
		"plus":           {key, "+" + token[1:]},
		"slash":          {key, "/" + token[1:]},
		"trailing LF":    {key, token[:85] + "\n"},
		"trailing LF 87": {key, token + "\n"},
		"embedded CR":    {key, token[:40] + "\r" + token[41:]},
		"leading space":  {key, " " + token[1:]},
		"empty":          {key, ""},
		"non canonical":  {key, token[:85] + "B"},
	}
	for name, c := range cases {
		if _, _, ok := reservations.DecodeQR(c.key, c.token); ok {
			t.Errorf("%s: decoded, want rejected", name)
		}
	}

	if gotID, gotNonce, ok := reservations.DecodeQR(key, reservations.EncodeQR(key, id, nonce)); !ok || gotID != id || gotNonce != nonce {
		t.Errorf("round trip = %v %x %v", gotID, gotNonce, ok)
	}
	if strings.ContainsAny(token, "=+/\n") {
		t.Errorf("token has characters outside base64url: %q", token)
	}
}
