package auth

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"strings"
	"time"

	"github.com/XYZLZ/MyPark/apps/server/internal/domain/apperr"
	"github.com/XYZLZ/MyPark/apps/server/internal/domain/authz"
	"github.com/XYZLZ/MyPark/apps/server/internal/domain/errcodes"
	"github.com/XYZLZ/MyPark/apps/server/internal/domain/secret"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/audit"
)

// Further users.status values.
const (
	StatusActive    = "active"
	StatusSuspended = "suspended"
)

// maxUserAgent bounds the stored user agent.
const maxUserAgent = 512

// LoginInput is a login request. Email may be unnormalized.
type LoginInput struct {
	Email      string
	Password   secret.String
	ClientKind ClientKind
	IP         netip.Addr
	UserAgent  string
}

// Login checks the credentials and opens a session. The password is verified
// before any transaction, and exactly once on every path, against a dummy
// hash when the account is unknown, so neither timing nor the response
// reveals which emails exist (spec 7.7). Not-verified and suspended are
// answered only after the password matched. The rate limit is the caller's
// job and runs before this.
func (s *Service) Login(ctx context.Context, in LoginInput) (TokenPair, error) {
	if s.d.Signer == nil {
		return TokenPair{}, errors.New("auth: login: no token signer configured")
	}
	u, found, err := s.d.Repo.FindUserForLogin(ctx, NormalizeEmail(in.Email))
	if err != nil {
		return TokenPair{}, err
	}
	known := found && u.HasPassword
	phc := s.d.Hasher.DummyHash()
	if known {
		phc = u.PasswordHash
	}
	ok, needsRehash, err := s.d.Hasher.Verify(ctx, phc, in.Password.Reveal())
	if err != nil {
		return TokenPair{}, fmt.Errorf("auth: verify password: %w", err)
	}
	if !known {
		return TokenPair{}, apperr.Unauthenticated(errcodes.AuthInvalidCredentials)
	}
	if !ok {
		if err := s.recordFailedLogin(ctx, u, "password"); err != nil {
			return TokenPair{}, err
		}
		return TokenPair{}, apperr.Unauthenticated(errcodes.AuthInvalidCredentials)
	}
	switch u.Status {
	case StatusPending:
		if err := s.recordFailedLogin(ctx, u, "not_verified"); err != nil {
			return TokenPair{}, err
		}
		return TokenPair{}, apperr.Forbidden(errcodes.AuthEmailNotVerified)
	case StatusSuspended:
		if err := s.recordFailedLogin(ctx, u, "suspended"); err != nil {
			return TokenPair{}, err
		}
		return TokenPair{}, apperr.Forbidden(errcodes.AuthAccountSuspended)
	case StatusActive:
	default:
		return TokenPair{}, apperr.Unauthenticated(errcodes.AuthInvalidCredentials)
	}

	// Everything slow happens before the transaction: the upgraded hash and
	// the legal lookup.
	newHash := ""
	if needsRehash {
		// Best effort: a failure here must not fail a valid login.
		if h, err := s.d.Hasher.Hash(ctx, in.Password.Reveal()); err == nil {
			newHash = h
		}
	}
	legalPending, err := s.legalPending(ctx, u)
	if err != nil {
		return TokenPair{}, err
	}

	var pair TokenPair
	err = s.d.Tx.InTx(ctx, func(ctx context.Context) error {
		var err error
		pair, err = s.openSession(ctx, u, in, newHash)
		return err
	})
	if err != nil {
		return TokenPair{}, err
	}
	pair.LegalAcceptanceRequired = legalPending
	return pair, nil
}

// openSession stores the session and its first refresh token, audits and
// signs the access token. It runs inside the login transaction.
func (s *Service) openSession(ctx context.Context, u LoginUser, in LoginInput, newHash string) (TokenPair, error) {
	now := s.d.Clock.Now()
	if newHash != "" {
		if err := s.d.Repo.RehashPassword(ctx, u.ID, u.PasswordHash, newHash, now); err != nil {
			return TokenPair{}, err
		}
	}
	absolute := now.Add(SessionAbsoluteTTL)
	sess, err := s.d.Repo.InsertSession(ctx, NewSession{
		UserID: u.ID, ClientKind: in.ClientKind, Now: now, Absolute: absolute,
		IP: in.IP, UserAgent: truncate(in.UserAgent, maxUserAgent),
	})
	if err != nil {
		return TokenPair{}, err
	}
	token, hash, err := NewRefreshToken(s.d.Rand)
	if err != nil {
		return TokenPair{}, err
	}
	expires := SuccessorExpiry(now, absolute)
	if err := s.d.Repo.InsertRefreshToken(ctx, sess.ID, hash, expires, now); err != nil {
		return TokenPair{}, err
	}
	uid := u.ID
	err = s.d.Audit.Record(ctx, audit.Entry{
		Action: audit.AuthLoginSucceeded, Actor: audit.ActorUser, ActorUserID: &uid,
		EntityType: "session", EntityID: sess.PublicID.String(),
		After: map[string]any{"client_kind": string(in.ClientKind)},
	})
	if err != nil {
		return TokenPair{}, fmt.Errorf("auth: audit %s: %w", audit.AuthLoginSucceeded, err)
	}
	role, _ := authz.ParseRole(u.Role)
	access, accessExp, err := s.d.Signer.Sign(u.PublicID, sess.PublicID, role)
	if err != nil {
		return TokenPair{}, fmt.Errorf("auth: sign access token: %w", err)
	}
	return TokenPair{
		AccessToken:      access,
		AccessExpiresIn:  int(accessExp.Sub(now.Truncate(time.Second)) / time.Second),
		RefreshToken:     secret.String(token),
		RefreshExpiresIn: int(expires.Sub(now) / time.Second),
		User:             u.View,
	}, nil
}

// recordFailedLogin audits a refused login (reason password, not_verified or
// suspended) for a known account. The audit
// row commits on its own, because the request then fails with 401.
func (s *Service) recordFailedLogin(ctx context.Context, u LoginUser, reason string) error {
	return s.d.Tx.InTx(ctx, func(ctx context.Context) error {
		err := s.d.Audit.Record(ctx, audit.Entry{
			Action: audit.AuthLoginFailed, Actor: audit.ActorAnonymous,
			EntityType: "user", EntityID: u.PublicID.String(),
			After: map[string]any{"reason": reason},
		})
		if err != nil {
			return fmt.Errorf("auth: audit %s: %w", audit.AuthLoginFailed, err)
		}
		return nil
	})
}

// legalPending reports whether a driver has not accepted the current legal
// documents. Admins are exempt (OC-15).
func (s *Service) legalPending(ctx context.Context, u LoginUser) (bool, error) {
	if u.Role != RoleDriver {
		return false, nil
	}
	cur, err := s.d.Legal.Versions(ctx)
	if err != nil {
		return false, err
	}
	return legalOutdated(u.View.TermsVersion, u.View.PrivacyVersion, cur), nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return strings.ToValidUTF8(s[:n], "")
}
