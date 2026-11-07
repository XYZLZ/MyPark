package auth

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/XYZLZ/MyPark/apps/server/internal/domain/apperr"
	"github.com/XYZLZ/MyPark/apps/server/internal/domain/errcodes"
	"github.com/XYZLZ/MyPark/apps/server/internal/domain/secret"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/audit"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/notifications"
)

// Forgot emails a reset code to an active user. For any other address,
// including pending drivers, admin invitees and suspended users, it does
// nothing and still returns nil, so the answer never reveals the account
// (spec 7.7, OC-38).
func (s *Service) Forgot(ctx context.Context, email string) error {
	email = NormalizeEmail(email)
	return s.d.Tx.InTx(ctx, func(ctx context.Context) error {
		u, found, err := s.d.Repo.LockUserByEmail(ctx, email)
		if err != nil || !found || u.Status != StatusActive {
			return err
		}
		code, codeID, err := s.issueCode(ctx, u, PurposeResetPassword, CodeTTL)
		if err != nil {
			return err
		}
		uid := u.ID
		err = s.d.Notify.Enqueue(ctx, notifications.Email{
			Template:  notifications.TplPasswordReset,
			Locale:    u.Locale,
			To:        email,
			UserID:    &uid,
			Data:      map[string]string{"code": code},
			DedupeKey: fmt.Sprintf("password_reset:%d", codeID),
		})
		if err != nil {
			return fmt.Errorf("auth: enqueue password reset: %w", err)
		}
		return s.recordAudit(ctx, audit.Entry{
			Action: audit.AuthPasswordResetRequested, Actor: audit.ActorAnonymous,
			EntityType: "user", EntityID: u.PublicID.String(),
		})
	})
}

// Reset sets a new password for the active user whose emailed code is code,
// ends every session of that user and tells them by email. Any failure,
// including an unknown address, an invitee or a suspended user, is the same
// auth.code_invalid (OC-11).
func (s *Service) Reset(ctx context.Context, email string, code, newPassword secret.String) error {
	email = NormalizeEmail(email)
	// Hash before the transaction so no connection waits on the argon2id
	// semaphore. It also keeps timing identical on the failing paths.
	hash, err := hashPassword(ctx, s.d.Hasher, newPassword)
	if err != nil {
		return err
	}
	done := false
	// Commit first: a wrong code still persists its attempt count.
	err = s.d.Tx.InTx(ctx, func(ctx context.Context) error {
		u, found, err := s.d.Repo.LockUserByEmail(ctx, email)
		if err != nil {
			return err
		}
		if !found || u.Status != StatusActive {
			// Same work shape against no row.
			_, err := s.checkCode(ctx, 0, uuid.Nil, PurposeResetPassword, code.Reveal())
			return err
		}
		ok, err := s.checkCode(ctx, u.ID, u.PublicID, PurposeResetPassword, code.Reveal())
		if err != nil || !ok {
			return err
		}
		if err := s.completeReset(ctx, u, email, hash); err != nil {
			return err
		}
		done = true
		return nil
	})
	if err != nil {
		return err
	}
	if !done {
		return apperr.Invalid(errcodes.AuthCodeInvalid)
	}
	return nil
}

// completeReset applies a verified reset inside the caller's transaction.
func (s *Service) completeReset(ctx context.Context, u UserRow, email, hash string) error {
	now := s.d.Clock.Now()
	if err := s.d.Repo.SetPassword(ctx, u.ID, hash, now); err != nil {
		return err
	}
	// The used code is already consumed; any other live code is now moot.
	for _, p := range []Purpose{PurposeResetPassword, PurposeVerifyEmail, PurposeAdminInvite} {
		if err := s.d.Repo.ConsumeLiveCodes(ctx, u.ID, p, now); err != nil {
			return err
		}
	}
	revoked, err := s.RevokeAllSessions(ctx, u.ID, ReasonPasswordReset)
	if err != nil {
		return err
	}
	uid := u.ID
	err = s.d.Notify.Enqueue(ctx, notifications.Email{
		Template:  notifications.TplPasswordChanged,
		Locale:    u.Locale,
		To:        email,
		UserID:    &uid,
		DedupeKey: fmt.Sprintf("password_changed:%d:%d", u.ID, now.UnixNano()),
	})
	if err != nil {
		return fmt.Errorf("auth: enqueue password changed: %w", err)
	}
	return s.recordAudit(ctx, audit.Entry{
		Action: audit.AuthPasswordReset, Actor: audit.ActorUser, ActorUserID: &uid,
		EntityType: "user", EntityID: u.PublicID.String(),
		After: map[string]any{"sessions_revoked": revoked},
	})
}

func (s *Service) recordAudit(ctx context.Context, e audit.Entry) error {
	if err := s.d.Audit.Record(ctx, e); err != nil {
		return fmt.Errorf("auth: audit %s: %w", e.Action, err)
	}
	return nil
}
