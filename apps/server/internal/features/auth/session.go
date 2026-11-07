package auth

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"time"

	"github.com/google/uuid"

	"github.com/XYZLZ/MyPark/apps/server/internal/domain/authz"
	"github.com/XYZLZ/MyPark/apps/server/internal/domain/secret"
)

const (
	// RefreshTTL is the lifetime of a refresh token. It slides on rotation
	// (OC-31).
	RefreshTTL = 30 * 24 * time.Hour
	// SessionAbsoluteTTL caps a session however often it is refreshed
	// (OC-31).
	SessionAbsoluteTTL = 90 * 24 * time.Hour

	refreshTokenBytes = 32
)

// SuccessorExpiry returns when a refresh token issued at now expires: the
// sliding TTL, never past the absolute end of the session.
func SuccessorExpiry(now, absolute time.Time) time.Time {
	return minTime(now.Add(RefreshTTL), absolute)
}

func minTime(a, b time.Time) time.Time {
	if b.Before(a) {
		return b
	}
	return a
}

// NewRefreshToken reads 32 random bytes from r and returns the token for the
// client (base64url, no padding) with the SHA-256 that is stored. The token
// itself is never stored.
func NewRefreshToken(r io.Reader) (token string, hash []byte, err error) {
	raw := make([]byte, refreshTokenBytes)
	if _, err := io.ReadFull(r, raw); err != nil {
		return "", nil, fmt.Errorf("auth: read refresh token entropy: %w", err)
	}
	sum := sha256.Sum256(raw)
	return base64.RawURLEncoding.EncodeToString(raw), sum[:], nil
}

// ClientKind is the kind of client a session belongs to.
type ClientKind string

// Client kinds, as stored in auth_sessions.client_kind.
const (
	ClientWeb    ClientKind = "web"
	ClientMobile ClientKind = "mobile"
)

// TokenPair is the result of a login or refresh. RefreshToken is a
// secret.String so it cannot reach a log; the HTTP layer reveals it into a
// plain response field.
type TokenPair struct {
	AccessToken             string
	AccessExpiresIn         int
	RefreshToken            secret.String
	RefreshExpiresIn        int
	LegalAcceptanceRequired bool
	User                    UserView
}

// UserView is a user as the API shows it. It is shared with the users
// feature through ToUserJSON.
type UserView struct {
	PublicID          uuid.UUID
	Email             string
	FirstName         string
	LastName          string
	Phone             *string
	Role              authz.Role
	Status            string
	Locale            string
	EmailVerifiedAt   *time.Time
	TermsVersion      *string
	PrivacyVersion    *string
	TermsAcceptedAt   *time.Time
	PrivacyAcceptedAt *time.Time
	CreatedAt         time.Time
}

type userLegalJSON struct {
	TermsVersion      *string    `json:"terms_version"`
	TermsAcceptedAt   *time.Time `json:"terms_accepted_at"`
	PrivacyVersion    *string    `json:"privacy_version"`
	PrivacyAcceptedAt *time.Time `json:"privacy_accepted_at"`
}

type userJSON struct {
	ID              uuid.UUID     `json:"id"`
	Email           string        `json:"email"`
	FirstName       string        `json:"first_name"`
	LastName        string        `json:"last_name"`
	Phone           *string       `json:"phone"`
	Role            authz.Role    `json:"role"`
	Status          string        `json:"status"`
	Locale          string        `json:"locale"`
	EmailVerifiedAt *time.Time    `json:"email_verified_at"`
	Legal           userLegalJSON `json:"legal"`
	CreatedAt       time.Time     `json:"created_at"`
}

// ToUserJSON returns v in the exact User shape of the API (spec 6.2).
func ToUserJSON(v UserView) any {
	return userJSON{
		ID: v.PublicID, Email: v.Email, FirstName: v.FirstName, LastName: v.LastName,
		Phone: v.Phone, Role: v.Role, Status: v.Status, Locale: v.Locale,
		EmailVerifiedAt: utcPtr(v.EmailVerifiedAt),
		Legal: userLegalJSON{
			TermsVersion: v.TermsVersion, TermsAcceptedAt: utcPtr(v.TermsAcceptedAt),
			PrivacyVersion: v.PrivacyVersion, PrivacyAcceptedAt: utcPtr(v.PrivacyAcceptedAt),
		},
		CreatedAt: v.CreatedAt.UTC(),
	}
}

func utcPtr(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC()
	return &u
}

// tokenResponse is the wire form of TokenPair. RefreshToken is a plain
// string: secret.String would encode as "[redacted]". It is filled here, at
// the response boundary, and nowhere else.
type tokenResponse struct {
	AccessToken             string `json:"access_token"`
	TokenType               string `json:"token_type"`
	ExpiresIn               int    `json:"expires_in"`
	RefreshToken            string `json:"refresh_token"`
	RefreshExpiresIn        int    `json:"refresh_expires_in"`
	LegalAcceptanceRequired bool   `json:"legal_acceptance_required"`
	User                    any    `json:"user"`
}

func newTokenResponse(p TokenPair) tokenResponse {
	return tokenResponse{
		AccessToken: p.AccessToken, TokenType: "Bearer", ExpiresIn: p.AccessExpiresIn,
		RefreshToken: p.RefreshToken.Reveal(), RefreshExpiresIn: p.RefreshExpiresIn,
		LegalAcceptanceRequired: p.LegalAcceptanceRequired, User: ToUserJSON(p.User),
	}
}
