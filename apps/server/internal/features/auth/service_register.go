package auth

import (
	"cmp"
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/XYZLZ/MyPark/apps/server/internal/domain/apperr"
	"github.com/XYZLZ/MyPark/apps/server/internal/domain/errcodes"
	"github.com/XYZLZ/MyPark/apps/server/internal/domain/secret"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/audit"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/notifications"
)

// defaultLocale is used when a registration names none.
const defaultLocale = "es-DO"

// RegisterInput is a registration request. Phone is nil for none.
type RegisterInput struct {
	Email          string
	Password       secret.String
	FirstName      string
	LastName       string
	Phone          *string
	Locale         string
	TermsVersion   string
	PrivacyVersion string
}

// Register creates, or refreshes, a pending driver and emails a code. Every
// outcome returns nil, so callers answer identically whether the address is
// new, pending, active or a pending admin invitee (spec 7.6, 7.7). Only
// legal.version_outdated, which does not depend on the address, is an error.
func (s *Service) Register(ctx context.Context, in RegisterInput) error {
	if err := s.d.Legal.ValidateAcceptance(ctx, in.TermsVersion, in.PrivacyVersion); err != nil {
		return err
	}
	// Hash on every path, before any transaction: timing is identical and no
	// connection is held while waiting on the argon2id semaphore.
	hash, err := hashPassword(ctx, s.d.Hasher, in.Password)
	if err != nil {
		return err
	}
	d := NewDriver{
		Email:          NormalizeEmail(in.Email),
		PasswordHash:   hash,
		FirstName:      in.FirstName,
		LastName:       in.LastName,
		Phone:          in.Phone,
		Locale:         cmp.Or(in.Locale, defaultLocale),
		TermsVersion:   in.TermsVersion,
		PrivacyVersion: in.PrivacyVersion,
	}
	// A concurrent registration of the same address makes the insert fail
	// once; the retry then sees the other row and takes the pending path.
	for range 2 {
		err = s.d.Tx.InTx(ctx, func(ctx context.Context) error { return s.register(ctx, d) })
		if !errors.Is(err, ErrEmailTaken) {
			return err
		}
	}
	return err
}

func (s *Service) register(ctx context.Context, d NewDriver) error {
	d.Now = s.d.Clock.Now()
	u, found, err := s.d.Repo.LockUserByEmail(ctx, d.Email)
	if err != nil {
		return err
	}
	switch {
	case !found:
		return s.registerNew(ctx, d)
	case u.Role == RoleDriver && u.Status == StatusPending:
		return s.replacePending(ctx, u, d)
	case u.Status == StatusPending:
		// A pending admin invitee (OC-38): never overwritten, never emailed.
		return nil
	default:
		return s.notifyAccountExists(ctx, u, d)
	}
}

func (s *Service) registerNew(ctx context.Context, d NewDriver) error {
	u, err := s.d.Repo.InsertPendingDriver(ctx, d)
	if err != nil {
		return err
	}
	return s.issueAndAnnounce(ctx, u, d, audit.AuthRegistered)
}

func (s *Service) replacePending(ctx context.Context, u UserRow, d NewDriver) error {
	if err := s.d.Repo.OverwritePendingDriver(ctx, u.ID, d); err != nil {
		return err
	}
	u.Locale = d.Locale
	return s.issueAndAnnounce(ctx, u, d, audit.AuthRegistrationReplaced)
}

// issueAndAnnounce issues the verification code, enqueues it and audits.
func (s *Service) issueAndAnnounce(ctx context.Context, u UserRow, d NewDriver, action string) error {
	code, codeID, err := s.issueCode(ctx, u, PurposeVerifyEmail, CodeTTL)
	if err != nil {
		return err
	}
	if err := s.enqueueVerifyEmail(ctx, d.Email, u, code, codeID); err != nil {
		return err
	}
	err = s.d.Audit.Record(ctx, audit.Entry{
		Action: action, Actor: audit.ActorAnonymous,
		EntityType: "user", EntityID: u.PublicID.String(),
		After: map[string]any{"locale": d.Locale},
	})
	if err != nil {
		return fmt.Errorf("auth: audit %s: %w", action, err)
	}
	return nil
}

// notifyAccountExists tells the owner of an active or suspended account that
// someone tried to register it. One message per hour at most.
func (s *Service) notifyAccountExists(ctx context.Context, u UserRow, d NewDriver) error {
	uid := u.ID
	err := s.d.Notify.Enqueue(ctx, notifications.Email{
		Template:  notifications.TplAccountExists,
		Locale:    u.Locale,
		To:        d.Email,
		UserID:    &uid,
		DedupeKey: fmt.Sprintf("account_exists:%d:%s", u.ID, d.Now.UTC().Format("2006010215")),
	})
	if err != nil {
		return fmt.Errorf("auth: enqueue account exists: %w", err)
	}
	return nil
}

// Verify activates the pending driver whose emailed code is code. Any
// failure, including an unknown address or an admin invitee, is the same
// auth.code_invalid.
func (s *Service) Verify(ctx context.Context, email string, code secret.String) error {
	email = NormalizeEmail(email)
	verified := false
	// Commit first: a wrong code still persists its attempt count.
	err := s.d.Tx.InTx(ctx, func(ctx context.Context) error {
		u, found, err := s.d.Repo.LockPendingDriverByEmail(ctx, email)
		if err != nil {
			return err
		}
		if !found {
			// Same work shape against no row (OC-11).
			_, err := s.checkCode(ctx, 0, uuid.Nil, PurposeVerifyEmail, code.Reveal())
			return err
		}
		ok, err := s.checkCode(ctx, u.ID, u.PublicID, PurposeVerifyEmail, code.Reveal())
		if err != nil || !ok {
			return err
		}
		if err := s.d.Repo.ActivateUser(ctx, u.ID, s.d.Clock.Now()); err != nil {
			return err
		}
		uid := u.ID
		if err := s.d.Audit.Record(ctx, audit.Entry{
			Action: audit.AuthEmailVerified, Actor: audit.ActorUser, ActorUserID: &uid,
			EntityType: "user", EntityID: u.PublicID.String(),
		}); err != nil {
			return fmt.Errorf("auth: audit %s: %w", audit.AuthEmailVerified, err)
		}
		verified = true
		return nil
	})
	if err != nil {
		return err
	}
	if !verified {
		return apperr.Invalid(errcodes.AuthCodeInvalid)
	}
	return nil
}

// Resend issues a fresh code to a pending driver. For any other address it
// does nothing and still returns nil.
func (s *Service) Resend(ctx context.Context, email string) error {
	email = NormalizeEmail(email)
	return s.d.Tx.InTx(ctx, func(ctx context.Context) error {
		u, found, err := s.d.Repo.LockPendingDriverByEmail(ctx, email)
		if err != nil || !found {
			return err
		}
		code, codeID, err := s.issueCode(ctx, u, PurposeVerifyEmail, CodeTTL)
		if err != nil {
			return err
		}
		return s.enqueueVerifyEmail(ctx, email, u, code, codeID)
	})
}
