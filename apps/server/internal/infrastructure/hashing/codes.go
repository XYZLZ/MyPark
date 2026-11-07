package hashing

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"log/slog"

	"github.com/google/uuid"
)

// CodeHasher derives the stored form of short-lived one-time codes: an
// HMAC-SHA256 keyed with a server-side pepper, so a leaked table of hashes
// cannot be brute-forced offline without the pepper.
type CodeHasher struct {
	pepper []byte
}

// MinPepperLen is the shortest accepted pepper, in bytes.
const MinPepperLen = 32

// NewCodeHasher returns a CodeHasher keyed with pepper. It rejects a pepper
// shorter than MinPepperLen bytes.
func NewCodeHasher(pepper []byte) (CodeHasher, error) {
	if len(pepper) < MinPepperLen {
		return CodeHasher{}, errors.New("hashing: pepper must be at least 32 bytes")
	}
	return CodeHasher{pepper: append([]byte(nil), pepper...)}, nil
}

// String, GoString and LogValue keep the pepper out of logs and fmt output.
func (CodeHasher) String() string       { return "[redacted]" }
func (CodeHasher) GoString() string     { return "[redacted]" }
func (CodeHasher) LogValue() slog.Value { return slog.StringValue("[redacted]") }

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
