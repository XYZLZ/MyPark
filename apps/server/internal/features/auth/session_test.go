package auth_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"testing"
	"time"

	"github.com/XYZLZ/MyPark/apps/server/internal/features/auth"
)

func TestSuccessorExpiry_SlidingCappedByAbsolute(t *testing.T) {
	now := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	if got, want := auth.SuccessorExpiry(now, now.Add(90*24*time.Hour)), now.Add(30*24*time.Hour); !got.Equal(want) {
		t.Errorf("far absolute: got %v, want %v", got, want)
	}
	abs := now.Add(10 * 24 * time.Hour)
	if got := auth.SuccessorExpiry(now, abs); !got.Equal(abs) {
		t.Errorf("near absolute: got %v, want %v", got, abs)
	}
}

func TestNewRefreshToken_FormatAndHash(t *testing.T) {
	raw := bytes.Repeat([]byte{0xAB}, 32)
	token, hash, err := auth.NewRefreshToken(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if len(token) != 43 {
		t.Errorf("len(token) = %d, want 43", len(token))
	}
	dec, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || !bytes.Equal(dec, raw) {
		t.Errorf("token does not decode to the 32 random bytes: %v", err)
	}
	if sum := sha256.Sum256(raw); !bytes.Equal(hash, sum[:]) {
		t.Error("hash is not sha256 of the raw bytes")
	}
	if _, _, err := auth.NewRefreshToken(bytes.NewReader(raw[:10])); err == nil {
		t.Error("short random source must fail")
	}
}
