package hashing

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"

	"github.com/google/uuid"
)

// CodeHasher derives the stored form of short-lived one-time codes: an
// HMAC-SHA256 keyed with a server-side pepper, so a leaked table of hashes
// cannot be brute-forced offline without the pepper.
type CodeHasher struct {
	pepper []byte
}

// NewCodeHasher returns a CodeHasher keyed with pepper.
func NewCodeHasher(pepper []byte) CodeHasher {
	return CodeHasher{pepper: append([]byte(nil), pepper...)}
}

// Hash returns HMAC-SHA256(pepper, purpose ":" userPublicID ":" code). Binding
// the purpose and the user stops a code from being replayed for another flow
// or account.
func (c CodeHasher) Hash(purpose string, userPublicID uuid.UUID, code string) []byte {
	m := hmac.New(sha256.New, c.pepper)
	m.Write([]byte(purpose + ":" + userPublicID.String() + ":" + code))
	return m.Sum(nil)
}

// Equal compares two digests in constant time.
func Equal(a, b []byte) bool {
	return subtle.ConstantTimeCompare(a, b) == 1
}
