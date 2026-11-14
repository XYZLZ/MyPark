package secretbox_test

import (
	"bytes"
	"errors"
	"testing"

	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/secretbox"
)

func key(id string, fill byte) secretbox.Key {
	k := secretbox.Key{ID: id}
	for i := range k.Secret {
		k.Secret[i] = fill
	}
	return k
}

func mustSealer(t *testing.T, current secretbox.Key, old ...secretbox.Key) *secretbox.Sealer {
	t.Helper()
	s, err := secretbox.NewSealer(current, old...)
	if err != nil {
		t.Fatalf("NewSealer: %v", err)
	}
	return s
}

func TestSealer_RoundTrip(t *testing.T) {
	s := mustSealer(t, key("k1", 1))
	ct, kid, err := s.Seal([]byte("abc"))
	if err != nil {
		t.Fatal(err)
	}
	if kid != "k1" || s.CurrentKeyID() != "k1" {
		t.Errorf("key id = %q / %q, want k1", kid, s.CurrentKeyID())
	}
	if want := 24 + 3 + 16; len(ct) != want {
		t.Errorf("len(ciphertext) = %d, want %d", len(ct), want)
	}
	if bytes.Contains(ct, []byte("abc")) {
		t.Error("ciphertext contains the plaintext")
	}
	got, err := s.Open(ct, kid)
	if err != nil || string(got) != "abc" {
		t.Fatalf("Open = %q, %v; want abc", got, err)
	}
}

func TestSealer_NoncesDiffer(t *testing.T) {
	s := mustSealer(t, key("k1", 1))
	a, _, _ := s.Seal([]byte("same"))
	b, _, _ := s.Seal([]byte("same"))
	if bytes.Equal(a, b) {
		t.Error("two seals of the same plaintext are identical: nonce reused")
	}
}

func TestSealer_OpensWithOldKey(t *testing.T) {
	k1, k2 := key("k1", 1), key("k2", 2)
	ct, kid, err := mustSealer(t, k1).Seal([]byte("rotate me"))
	if err != nil {
		t.Fatal(err)
	}
	rotated := mustSealer(t, k2, k1)
	got, err := rotated.Open(ct, kid)
	if err != nil || string(got) != "rotate me" {
		t.Fatalf("Open with old key = %q, %v", got, err)
	}
	if _, newKID, _ := rotated.Seal([]byte("x")); newKID != "k2" {
		t.Errorf("new seals use %q, want k2", newKID)
	}
}

func TestSealer_UnknownKeyAndTamper(t *testing.T) {
	s := mustSealer(t, key("k1", 1))
	ct, kid, _ := s.Seal([]byte("abc"))
	if _, err := s.Open(ct, "nope"); !errors.Is(err, secretbox.ErrUnknownKey) {
		t.Errorf("unknown key: err = %v, want ErrUnknownKey", err)
	}
	for _, pos := range []int{0, len(ct) - 1} {
		bad := bytes.Clone(ct)
		bad[pos] ^= 1
		if _, err := s.Open(bad, kid); !errors.Is(err, secretbox.ErrDecrypt) {
			t.Errorf("flipped byte %d: err = %v, want ErrDecrypt", pos, err)
		}
	}
	if _, err := s.Open(ct[:10], kid); !errors.Is(err, secretbox.ErrDecrypt) {
		t.Errorf("short ciphertext: err = %v, want ErrDecrypt", err)
	}
	other := mustSealer(t, key("k1", 9))
	if _, err := other.Open(ct, kid); !errors.Is(err, secretbox.ErrDecrypt) {
		t.Errorf("wrong key material: err = %v, want ErrDecrypt", err)
	}
}

func TestNewSealer_RejectsBadIDs(t *testing.T) {
	if _, err := secretbox.NewSealer(key("", 1)); err == nil {
		t.Error("empty current id accepted")
	}
	if _, err := secretbox.NewSealer(key("k1", 1), key("", 2)); err == nil {
		t.Error("empty old id accepted")
	}
	if _, err := secretbox.NewSealer(key("k1", 1), key("k1", 2)); err == nil {
		t.Error("old id repeating the current id accepted")
	}
	if _, err := secretbox.NewSealer(key("k1", 1), key("k0", 2), key("k0", 3)); err == nil {
		t.Error("duplicate old ids accepted")
	}
}
