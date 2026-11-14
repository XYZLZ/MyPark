// Package secretbox seals small secrets at rest (device HMAC secrets) with
// NaCl secretbox (XSalsa20-Poly1305) under a rotatable keyring. Every Seal
// picks a fresh random nonce and tags the result with the key id, so rows
// sealed under an old key stay readable after rotation.
package secretbox

import (
	"crypto/rand"
	"errors"
	"fmt"

	"golang.org/x/crypto/nacl/secretbox"
)

const nonceSize = 24

var (
	// ErrUnknownKey means the key id is not in the keyring.
	ErrUnknownKey = errors.New("secretbox: unknown key id")
	// ErrDecrypt means the ciphertext is malformed, tampered with or was
	// sealed under different key material. It never carries the plaintext.
	ErrDecrypt = errors.New("secretbox: decryption failed")
)

// Key is a 32-byte secretbox key and its identifier.
type Key struct {
	ID     string
	Secret [32]byte
}

// Sealer encrypts under the current key and decrypts under any keyring key.
// It is safe for concurrent use: it holds no mutable state.
type Sealer struct {
	current Key
	keys    map[string]Key
}

// NewSealer builds a keyring whose current key seals new values and whose old
// keys only open values sealed earlier. Ids must be non-empty and unique.
func NewSealer(current Key, old ...Key) (*Sealer, error) {
	keys := make(map[string]Key, 1+len(old))
	for _, k := range append([]Key{current}, old...) {
		if k.ID == "" {
			return nil, errors.New("secretbox: key id must not be empty")
		}
		if _, dup := keys[k.ID]; dup {
			return nil, fmt.Errorf("secretbox: duplicate key id %q", k.ID)
		}
		keys[k.ID] = k
	}
	return &Sealer{current: current, keys: keys}, nil
}

// CurrentKeyID returns the id new values are sealed under.
func (s *Sealer) CurrentKeyID() string { return s.current.ID }

// Seal encrypts plain under the current key. The result is a random 24-byte
// nonce followed by the secretbox output; keyID names the key to Open with.
func (s *Sealer) Seal(plain []byte) (ciphertext []byte, keyID string, err error) {
	var nonce [nonceSize]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return nil, "", fmt.Errorf("secretbox: nonce: %w", err)
	}
	return secretbox.Seal(nonce[:], plain, &nonce, &s.current.Secret), s.current.ID, nil
}

// Open decrypts a value produced by Seal under the key named keyID.
func (s *Sealer) Open(ciphertext []byte, keyID string) ([]byte, error) {
	k, ok := s.keys[keyID]
	if !ok {
		return nil, ErrUnknownKey
	}
	if len(ciphertext) < nonceSize+secretbox.Overhead {
		return nil, ErrDecrypt
	}
	var nonce [nonceSize]byte
	copy(nonce[:], ciphertext)
	plain, ok := secretbox.Open(nil, ciphertext[nonceSize:], &nonce, &k.Secret)
	if !ok {
		return nil, ErrDecrypt
	}
	return plain, nil
}
