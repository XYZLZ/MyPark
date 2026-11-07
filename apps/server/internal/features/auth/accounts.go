package auth

import (
	"context"
	"errors"

	"github.com/XYZLZ/MyPark/apps/server/internal/domain/apperr"
	"github.com/XYZLZ/MyPark/apps/server/internal/domain/errcodes"
	"github.com/XYZLZ/MyPark/apps/server/internal/domain/secret"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/database"
)

// Accounts is the use-case interface other features call for the parts of an
// account that auth owns: the password, the sessions and the login columns.
type Accounts interface {
	// VerifyPassword returns apperr.Forbidden(auth.password_incorrect) unless
	// pw is the current password of the user. It runs one argon2id
	// verification, so callers invoke it before opening a transaction.
	VerifyPassword(ctx context.Context, userID int64, pw secret.String) error
	// RevokeAllSessions revokes every live session of the user with reason
	// and returns how many it revoked.
	RevokeAllSessions(ctx context.Context, userID int64, reason string) (int, error)
	// Erase anonymizes the auth-owned columns of the user (email, password
	// hash, status, deleted_at), revokes its sessions with account_deleted
	// and consumes its live codes. It must run inside a transaction.
	Erase(ctx context.Context, userID int64) error
}

// ErrUserGone is returned by Accounts.Erase when the user does not exist or
// is already deleted.
var ErrUserGone = errors.New("auth: user not found or already deleted")

type accounts struct{ s *Service }

// NewAccounts returns the Accounts view of s.
func NewAccounts(s *Service) Accounts { return accounts{s: s} }

func (a accounts) VerifyPassword(ctx context.Context, userID int64, pw secret.String) error {
	d := a.s.d
	hash, found, err := d.Repo.GetPasswordHash(ctx, userID)
	if err != nil {
		return err
	}
	if !found {
		// Same work shape as a wrong password, against the dummy hash.
		hash = d.Hasher.DummyHash()
	}
	ok, _, err := d.Hasher.Verify(ctx, hash, pw.Reveal())
	if err != nil {
		return err
	}
	if !found || !ok {
		return apperr.Forbidden(errcodes.AuthPasswordIncorrect)
	}
	return nil
}

func (a accounts) RevokeAllSessions(ctx context.Context, userID int64, reason string) (int, error) {
	return a.s.RevokeAllSessions(ctx, userID, reason)
}

func (a accounts) Erase(ctx context.Context, userID int64) error {
	if _, err := database.MustTx(ctx); err != nil {
		return err
	}
	d := a.s.d
	now := d.Clock.Now()
	n, err := d.Repo.EraseUser(ctx, userID, now)
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrUserGone
	}
	if _, err := a.s.RevokeAllSessions(ctx, userID, ReasonAccountDeleted); err != nil {
		return err
	}
	for _, p := range []Purpose{PurposeResetPassword, PurposeVerifyEmail, PurposeAdminInvite} {
		if err := d.Repo.ConsumeLiveCodes(ctx, userID, p, now); err != nil {
			return err
		}
	}
	return nil
}
