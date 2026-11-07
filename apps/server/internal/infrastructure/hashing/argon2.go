// Package hashing provides password hashing (argon2id), one-time code
// hashing (peppered HMAC) and the common-password rule.
package hashing

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Params are the argon2id cost parameters. They are stored inside every hash
// so they can be raised later without invalidating existing hashes.
type Params struct {
	Memory  uint32 // KiB
	Time    uint32
	Threads uint8
	SaltLen uint32
	KeyLen  uint32
}

// DefaultParams: m=64 MiB, t=3, p=2.
var DefaultParams = Params{Memory: 64 * 1024, Time: 3, Threads: 2, SaltLen: 16, KeyLen: 32}

// Upper bounds accepted when parsing a stored hash, so a corrupted or hostile
// PHC string cannot make Verify allocate unbounded memory or time.
const (
	maxMemoryKiB = 1024 * 1024
	maxTime      = 16
	maxThreads   = 64
	maxKeyLen    = 1024
)

// ErrInvalidHash is returned when a stored hash is not a valid argon2id PHC string.
var ErrInvalidHash = errors.New("hashing: invalid argon2id hash")

// PasswordHasher hashes and verifies passwords.
type PasswordHasher interface {
	Hash(ctx context.Context, password string) (string, error)
	// Verify reports whether password matches phc, and whether phc was made
	// with different parameters than the hasher's current ones.
	Verify(ctx context.Context, phc, password string) (ok, needsRehash bool, err error)
	// DummyHash returns a valid hash to verify against when the user does not
	// exist, so timing does not reveal account existence.
	DummyHash() string
}

var b64 = base64.RawStdEncoding

// Argon2id implements PasswordHasher. At most maxConcurrent derivations run
// at once (OC-14); further callers wait until a slot frees or ctx ends.
// Callers must not hold a database transaction while hashing.
type Argon2id struct {
	p     Params
	sem   chan struct{}
	dummy string
	trace func(delta int32) // test hook: +1 on derivation start, -1 on end
}

// NewArgon2id builds a hasher and precomputes its dummy hash.
func NewArgon2id(p Params, maxConcurrent int64) *Argon2id {
	a := &Argon2id{p: p, sem: make(chan struct{}, max(maxConcurrent, 1))}
	salt := make([]byte, p.SaltLen)
	_, _ = rand.Read(salt)
	key := argon2.IDKey([]byte("dummy-password-never-valid"), salt, p.Time, p.Memory, p.Threads, p.KeyLen)
	a.dummy = encode(p, salt, key)
	return a
}

// DummyHash returns the precomputed dummy PHC string.
func (a *Argon2id) DummyHash() string { return a.dummy }

// derive runs argon2id inside a concurrency slot.
func (a *Argon2id) derive(ctx context.Context, password string, salt []byte, p Params, keyLen uint32) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	select {
	case a.sem <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	defer func() { <-a.sem }()
	if a.trace != nil {
		a.trace(1)
		defer a.trace(-1)
	}
	return argon2.IDKey([]byte(password), salt, p.Time, p.Memory, p.Threads, keyLen), nil
}

// Hash returns a PHC string for password using a fresh random salt.
func (a *Argon2id) Hash(ctx context.Context, password string) (string, error) {
	salt := make([]byte, a.p.SaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("hashing: salt: %w", err)
	}
	key, err := a.derive(ctx, password, salt, a.p, a.p.KeyLen)
	if err != nil {
		return "", err
	}
	return encode(a.p, salt, key), nil
}

// Verify checks password against phc using the parameters stored in phc.
// A wrong password is (false, false, nil); a malformed phc is an error.
func (a *Argon2id) Verify(ctx context.Context, phc, password string) (ok, needsRehash bool, err error) {
	p, salt, want, err := decode(phc)
	if err != nil {
		return false, false, err
	}
	got, err := a.derive(ctx, password, salt, p, p.KeyLen)
	if err != nil {
		return false, false, err
	}
	if subtle.ConstantTimeCompare(got, want) != 1 {
		return false, false, nil
	}
	return true, p != a.p, nil
}

func encode(p Params, salt, key []byte) string {
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, p.Memory, p.Time, p.Threads, b64.EncodeToString(salt), b64.EncodeToString(key))
}

func decode(phc string) (Params, []byte, []byte, error) {
	parts := strings.Split(phc, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" ||
		parts[2] != "v="+strconv.Itoa(argon2.Version) {
		return Params{}, nil, nil, ErrInvalidHash
	}
	fields := strings.Split(parts[3], ",")
	if len(fields) != 3 {
		return Params{}, nil, nil, ErrInvalidHash
	}
	var vals [3]uint64
	for i, prefix := range []string{"m=", "t=", "p="} {
		v, ok := strings.CutPrefix(fields[i], prefix)
		if !ok {
			return Params{}, nil, nil, ErrInvalidHash
		}
		n, err := strconv.ParseUint(v, 10, 32)
		if err != nil {
			return Params{}, nil, nil, ErrInvalidHash
		}
		vals[i] = n
	}
	m, t, th := vals[0], vals[1], vals[2]
	if m == 0 || m > maxMemoryKiB || t == 0 || t > maxTime || th == 0 || th > maxThreads {
		return Params{}, nil, nil, ErrInvalidHash
	}
	salt, err := b64.DecodeString(parts[4])
	if err != nil || len(salt) == 0 {
		return Params{}, nil, nil, ErrInvalidHash
	}
	key, err := b64.DecodeString(parts[5])
	if err != nil || len(key) == 0 || len(key) > maxKeyLen {
		return Params{}, nil, nil, ErrInvalidHash
	}
	return Params{
		Memory: uint32(m), Time: uint32(t), Threads: uint8(th),
		SaltLen: uint32(len(salt)), KeyLen: uint32(len(key)),
	}, salt, key, nil
}
