package ratelimit

import (
	"crypto/hmac"
	"crypto/sha256"
	"errors"
	"slices"
)

// MinPepperLen is the shortest pepper NewKeyHasher accepts, in bytes.
const MinPepperLen = 32

// KeyHasher hashes rate-limit bucket keys as HMAC-SHA256(pepper, rule.Prefix
// + key). Emails and IPs never reach the rate_limit_counters table or the
// logs in clear, and without the pepper a hash cannot be reversed by
// enumerating the IPv4 space. The zero value has no pepper and panics.
type KeyHasher struct {
	pepper []byte
}

// NewKeyHasher returns a KeyHasher over a copy of pepper (RATE_LIMIT_PEPPER),
// which must be at least MinPepperLen bytes.
func NewKeyHasher(pepper []byte) (KeyHasher, error) {
	if len(pepper) < MinPepperLen {
		return KeyHasher{}, errors.New("ratelimit: key pepper is too short")
	}
	return KeyHasher{pepper: slices.Clone(pepper)}, nil
}

// Hash returns the keyed hash of key under rule.
func (h KeyHasher) Hash(rule Rule, key string) []byte {
	if len(h.pepper) == 0 {
		panic("ratelimit: KeyHasher used without a pepper")
	}
	mac := hmac.New(sha256.New, h.pepper)
	mac.Write([]byte(rule.Prefix + key))
	return mac.Sum(nil)
}
