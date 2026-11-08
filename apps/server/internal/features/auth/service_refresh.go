package auth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net/netip"
	"time"

	"github.com/XYZLZ/MyPark/apps/server/internal/domain/apperr"
	"github.com/XYZLZ/MyPark/apps/server/internal/domain/authz"
	"github.com/XYZLZ/MyPark/apps/server/internal/domain/errcodes"
	"github.com/XYZLZ/MyPark/apps/server/internal/domain/secret"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/audit"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/legal"
)

// refreshed is what the refresh transaction hands back: the decision and,
// for RefreshOK, the data to sign and answer with.
type refreshed struct {
	outcome RefreshOutcome
	row     RefreshRow
	token   string
	expires time.Time
	now     time.Time
}

// Refresh rotates a refresh token (spec 7.4). The token is single use: the
// second presentation of a rotated token revokes the whole session (OC-5).
// The transaction commits before the failure is returned (spec 4.5, OC-6), so
// the revocation and its audit row survive the 401. Concurrent presentations
// of one token serialize on the row lock; the loser sees the rotation and is
// treated as reuse. The client IP and request id reach the audit row through
// ctx, so the ip parameter is not read here.
func (s *Service) Refresh(ctx context.Context, token secret.String, _ netip.Addr) (TokenPair, error) {
	if s.d.Signer == nil {
		return TokenPair{}, errors.New("auth: refresh: no token signer configured")
	}
	hash, ok := hashRefreshToken(token.Reveal())
	if !ok {
		return TokenPair{}, apperr.Unauthenticated(errcodes.AuthRefreshInvalid)
	}
	// Read the legal versions before the rotation: once it commits the
	// presented token is spent, so nothing after it may fail on a transient
	// error and force a logout. The lookup is cached.
	cur, err := s.d.Legal.Versions(ctx)
	if err != nil {
		return TokenPair{}, fmt.Errorf("auth: current legal versions: %w", err)
	}
	var res refreshed
	err = s.d.Tx.InTx(ctx, func(ctx context.Context) error {
		var err error
		res, err = s.rotate(ctx, hash)
		return err
	})
	if err != nil {
		return TokenPair{}, err
	}
	switch res.outcome {
	case RefreshOK:
	case RefreshInvalid:
		return TokenPair{}, apperr.Unauthenticated(errcodes.AuthRefreshInvalid)
	default:
		return TokenPair{}, apperr.Unauthenticated(errcodes.AuthSessionRevoked)
	}
	return s.refreshPair(res, cur)
}

// hashRefreshToken decodes a presented token and returns its SHA-256. It
// reports false for anything that is not 32 bytes of unpadded base64url, so
// garbage never reaches the database.
func hashRefreshToken(token string) ([]byte, bool) {
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(raw) != refreshTokenBytes {
		return nil, false
	}
	sum := sha256.Sum256(raw)
	return sum[:], true
}

// rotate runs inside the refresh transaction. It returns an error only for
// infrastructure failures: every refusal is an outcome, so the transaction
// still commits whatever the refusal wrote.
func (s *Service) rotate(ctx context.Context, hash []byte) (refreshed, error) {
	row, found, err := s.d.Repo.LockRefreshToken(ctx, hash)
	if err != nil {
		return refreshed{}, err
	}
	row.Snapshot.Found = found
	now := s.d.Clock.Now()
	res := refreshed{outcome: DecideRefresh(row.Snapshot, now), row: row, now: now}
	switch res.outcome {
	case RefreshReuse:
		if _, err := s.d.Repo.RevokeSession(ctx, row.SessionID, ReasonReuseDetected, now); err != nil {
			return refreshed{}, err
		}
		if err := s.auditReuse(ctx, row); err != nil {
			return refreshed{}, err
		}
	case RefreshUserInactive:
		// Only a deleted account has a revocation reason. A suspended or
		// pending one is refused without a write, and a later reactivation
		// keeps the session.
		if row.Snapshot.UserStatus == StatusDeleted {
			if _, err := s.d.Repo.RevokeSession(ctx, row.SessionID, ReasonAccountDeleted, now); err != nil {
				return refreshed{}, err
			}
		}
	case RefreshOK:
		token, newHash, err := NewRefreshToken(s.d.Rand)
		if err != nil {
			return refreshed{}, err
		}
		res.token = token
		res.expires = SuccessorExpiry(now, row.Snapshot.SessionAbsoluteExpiresAt)
		if err := s.d.Repo.RotateRefreshToken(ctx, row.TokenID, now); err != nil {
			return refreshed{}, err
		}
		if err := s.d.Repo.InsertRefreshToken(ctx, row.SessionID, newHash, res.expires, now); err != nil {
			return refreshed{}, err
		}
		if err := s.d.Repo.TouchSession(ctx, row.SessionID, now); err != nil {
			return refreshed{}, err
		}
	}
	return res, nil
}

func (s *Service) auditReuse(ctx context.Context, row RefreshRow) error {
	err := s.d.Audit.Record(ctx, audit.Entry{
		Action: audit.AuthRefreshReuseDetected, Actor: audit.ActorSystem,
		EntityType: "session", EntityID: row.SessionPublicID.String(),
		After: map[string]any{"user_id": row.User.PublicID.String()},
	})
	if err != nil {
		return fmt.Errorf("auth: audit %s: %w", audit.AuthRefreshReuseDetected, err)
	}
	return nil
}

// refreshPair signs the access token and builds the answer after the
// transaction committed, from the legal versions read before it.
func (s *Service) refreshPair(res refreshed, cur legal.Versions) (TokenPair, error) {
	u := res.row.User
	pending := false
	if u.Role == RoleDriver {
		pending = legalOutdated(res.row.View.TermsVersion, res.row.View.PrivacyVersion, cur)
	}
	role, _ := authz.ParseRole(u.Role)
	access, accessExp, err := s.d.Signer.Sign(u.PublicID, res.row.SessionPublicID, role)
	if err != nil {
		return TokenPair{}, fmt.Errorf("auth: sign access token: %w", err)
	}
	return TokenPair{
		AccessToken:             access,
		AccessExpiresIn:         int(accessExp.Sub(res.now.Truncate(time.Second)) / time.Second),
		RefreshToken:            secret.String(res.token),
		RefreshExpiresIn:        int(res.expires.Sub(res.now) / time.Second),
		LegalAcceptanceRequired: pending,
		User:                    res.row.View,
	}, nil
}
