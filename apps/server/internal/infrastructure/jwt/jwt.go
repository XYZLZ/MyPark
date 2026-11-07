// Package jwt signs and verifies the short-lived access tokens: JWS compact
// tokens signed with Ed25519 (alg EdDSA). The verifier accepts EdDSA only,
// picks the key by the kid header and requires issuer, audience and expiry.
package jwt

import (
	"crypto/ed25519"
	"errors"
	"log/slog"
	"time"

	gojwt "github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/XYZLZ/MyPark/apps/server/internal/domain/authz"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/clock"
)

const (
	// Issuer is the iss claim of every token.
	Issuer = "mypark-api"
	// Audience is the aud claim of every token.
	Audience = "mypark"
	// Leeway is the clock skew tolerated when checking exp and nbf.
	Leeway = 30 * time.Second
	// AccessTTL is the lifetime of an access token.
	AccessTTL = 15 * time.Minute

	redacted = "[redacted]"
)

// ErrInvalid is the single error returned for every verification failure, so
// callers cannot (and need not) distinguish why a token was rejected.
var ErrInvalid = errors.New("invalid token")

// Claims are the verified access token claims.
type Claims struct {
	Subject   uuid.UUID
	SessionID uuid.UUID
	Role      authz.Role
	IssuedAt  time.Time
	ExpiresAt time.Time
}

type wireClaims struct {
	gojwt.RegisteredClaims
	SessionID string `json:"sid"`
	Role      string `json:"role"`
}

// Signer issues access tokens with one Ed25519 key. Formatting it never
// prints the key.
type Signer struct {
	kid string
	key ed25519.PrivateKey
	clk clock.Clock
	ttl time.Duration
}

// NewSigner builds a Signer from the 32-byte Ed25519 seed. kid is written to
// the token header so verifiers can pick the matching public key.
func NewSigner(kid string, seed []byte, c clock.Clock, ttl time.Duration) (*Signer, error) {
	if kid == "" {
		return nil, errors.New("jwt: key id must not be empty")
	}
	if len(seed) != ed25519.SeedSize {
		return nil, errors.New("jwt: signing seed must be 32 bytes")
	}
	if ttl <= 0 {
		return nil, errors.New("jwt: ttl must be positive")
	}
	return &Signer{kid: kid, key: ed25519.NewKeyFromSeed(seed), clk: c, ttl: ttl}, nil
}

// Sign issues an access token for subject in session with role and returns
// it with its expiry.
func (s *Signer) Sign(subject, session uuid.UUID, role authz.Role) (string, time.Time, error) {
	now := s.clk.Now().UTC()
	exp := now.Add(s.ttl)
	tok := gojwt.NewWithClaims(gojwt.SigningMethodEdDSA, wireClaims{
		RegisteredClaims: gojwt.RegisteredClaims{
			Issuer:    Issuer,
			Audience:  gojwt.ClaimStrings{Audience},
			Subject:   subject.String(),
			IssuedAt:  gojwt.NewNumericDate(now),
			NotBefore: gojwt.NewNumericDate(now),
			ExpiresAt: gojwt.NewNumericDate(exp),
		},
		SessionID: session.String(),
		Role:      string(role),
	})
	tok.Header["kid"] = s.kid
	out, err := tok.SignedString(s.key)
	if err != nil {
		return "", time.Time{}, errors.New("jwt: sign token")
	}
	return out, exp.Truncate(time.Second), nil
}

// String, GoString and LogValue keep the private key out of logs and
// fmt output.
func (Signer) String() string       { return redacted }
func (Signer) GoString() string     { return redacted }
func (Signer) LogValue() slog.Value { return slog.StringValue(redacted) }

// Verifier validates access tokens against a set of public keys by kid.
type Verifier struct {
	keys map[string]ed25519.PublicKey
	clk  clock.Clock
}

// NewVerifier returns a Verifier trusting keys (kid to public key). The map
// is copied; include retired keys until their tokens have expired.
func NewVerifier(keys map[string]ed25519.PublicKey, c clock.Clock) *Verifier {
	cp := make(map[string]ed25519.PublicKey, len(keys))
	for k, v := range keys {
		cp[k] = v
	}
	return &Verifier{keys: cp, clk: c}
}

// Verify checks signature, algorithm, issuer, audience, expiry and not-before,
// and returns the claims. Every failure returns ErrInvalid.
func (v *Verifier) Verify(token string) (Claims, error) {
	var wc wireClaims
	parser := gojwt.NewParser(
		gojwt.WithValidMethods([]string{gojwt.SigningMethodEdDSA.Alg()}),
		gojwt.WithIssuer(Issuer),
		gojwt.WithAudience(Audience),
		gojwt.WithExpirationRequired(),
		gojwt.WithLeeway(Leeway),
		gojwt.WithTimeFunc(v.clk.Now),
	)
	_, err := parser.ParseWithClaims(token, &wc, func(t *gojwt.Token) (any, error) {
		kid, _ := t.Header["kid"].(string)
		key, ok := v.keys[kid]
		if kid == "" || !ok {
			return nil, ErrInvalid
		}
		return key, nil
	})
	if err != nil {
		return Claims{}, ErrInvalid
	}
	sub, err := uuid.Parse(wc.Subject)
	if err != nil {
		return Claims{}, ErrInvalid
	}
	sid, err := uuid.Parse(wc.SessionID)
	if err != nil {
		return Claims{}, ErrInvalid
	}
	role, ok := authz.ParseRole(wc.Role)
	if !ok || wc.IssuedAt == nil || wc.ExpiresAt == nil {
		return Claims{}, ErrInvalid
	}
	return Claims{
		Subject:   sub,
		SessionID: sid,
		Role:      role,
		IssuedAt:  wc.IssuedAt.Time,
		ExpiresAt: wc.ExpiresAt.Time,
	}, nil
}
