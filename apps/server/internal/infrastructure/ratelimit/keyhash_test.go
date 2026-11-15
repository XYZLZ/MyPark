package ratelimit_test

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"testing"

	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/ratelimit"
)

// A logged or stored key hash must not be reversible: an unkeyed SHA-256 of
// an IPv4 address falls to a 2^32 search in seconds. The hash is an HMAC
// under the server's RATE_LIMIT_PEPPER.
func TestKeyHasher(t *testing.T) {
	pepperA := bytes.Repeat([]byte{1}, 32)
	pepperB := bytes.Repeat([]byte{2}, 32)
	a, err := ratelimit.NewKeyHasher(pepperA)
	if err != nil {
		t.Fatal(err)
	}
	b, err := ratelimit.NewKeyHasher(pepperB)
	if err != nil {
		t.Fatal(err)
	}
	const key = "203.0.113.9"
	got := a.Hash(ratelimit.DeviceIP, key)

	mac := hmac.New(sha256.New, pepperA)
	mac.Write([]byte(ratelimit.DeviceIP.Prefix + key))
	if !bytes.Equal(got, mac.Sum(nil)) {
		t.Errorf("Hash = %x, want HMAC-SHA256(pepper, prefix+key)", got)
	}
	plain := sha256.Sum256([]byte(ratelimit.DeviceIP.Prefix + key))
	if bytes.Equal(got, plain[:]) {
		t.Error("Hash is the plain SHA-256")
	}
	if bytes.Equal(got, b.Hash(ratelimit.DeviceIP, key)) {
		t.Error("two peppers gave the same hash")
	}
	if bytes.Equal(got, a.Hash(ratelimit.PublicReadIP, key)) {
		t.Error("two rules share a hash for the same key")
	}

	pepperA[0] = 9 // the hasher keeps its own copy
	if !bytes.Equal(got, a.Hash(ratelimit.DeviceIP, key)) {
		t.Error("changing the caller's slice changed the hash")
	}
	if _, err := ratelimit.NewKeyHasher(make([]byte, 31)); err == nil {
		t.Error("a 31-byte pepper was accepted")
	}
}

// The zero KeyHasher has no pepper: hashing with it would be unkeyed, so it
// panics instead of logging a reversible hash.
func TestKeyHasher_ZeroValuePanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("zero KeyHasher hashed without a pepper")
		}
	}()
	var h ratelimit.KeyHasher
	h.Hash(ratelimit.DeviceIP, "203.0.113.9")
}
