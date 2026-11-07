// Package users serves the signed-in user's own account: profile, legal
// re-acceptance and deletion with anonymization.
package users

import (
	"context"
	"errors"
	"fmt"

	"github.com/XYZLZ/MyPark/apps/server/internal/domain/apperr"
	"github.com/XYZLZ/MyPark/apps/server/internal/domain/authz"
	"github.com/XYZLZ/MyPark/apps/server/internal/domain/errcodes"
	"github.com/XYZLZ/MyPark/apps/server/internal/domain/secret"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/audit"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/auth"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/legal"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/notifications"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/vehicles"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/clock"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/database"
	"github.com/XYZLZ/MyPark/apps/server/internal/transport/http/httpx"
)

// Deps are the collaborators of Service. Other features appear only through
// their exported use-case interfaces and ports.
type Deps struct {
	Tx       database.TxRunnerPort
	Repo     Repo
	Accounts auth.Accounts
	Vehicles vehicles.OwnerEraser
	Notify   notifications.Notifier
	Legal    legal.Current
	Audit    audit.Recorder
	Clock    clock.Clock
}

// Service implements the account use cases.
type Service struct{ d Deps }

// NewService returns a Service over d.
func NewService(d Deps) *Service { return &Service{d: d} }

// PatchInput is a partial profile update. A nil string is left unchanged.
// Strings must already be trimmed and validated. A Phone that is null, or an
// empty string, clears the phone.
type PatchInput struct {
	FirstName, LastName, Locale *string
	Phone                       httpx.Nullable[string]
}

// Get returns the principal's own account.
func (s *Service) Get(ctx context.Context, p authz.Principal) (auth.UserView, error) {
	v, err := s.d.Repo.GetProfile(ctx, p.UserID)
	return v, mapErr(err)
}

// Update applies in to the principal's profile and audits the names of the
// changed fields, never their values. An update that changes nothing writes
// nothing and records nothing.
func (s *Service) Update(ctx context.Context, p authz.Principal, in PatchInput) (auth.UserView, error) {
	u := ProfileUpdate{FirstName: in.FirstName, LastName: in.LastName, Locale: in.Locale}
	var fields []string
	if in.FirstName != nil {
		fields = append(fields, "first_name")
	}
	if in.LastName != nil {
		fields = append(fields, "last_name")
	}
	if in.Phone.Set {
		u.SetPhone = true
		if !in.Phone.Null && in.Phone.Value != "" {
			phone := in.Phone.Value
			u.Phone = &phone
		}
		fields = append(fields, "phone")
	}
	if in.Locale != nil {
		fields = append(fields, "locale")
	}
	if len(fields) == 0 {
		return s.Get(ctx, p)
	}
	var view auth.UserView
	err := s.d.Tx.InTx(ctx, func(ctx context.Context) error {
		var err error
		view, err = s.d.Repo.UpdateProfile(ctx, p.UserID, u, s.d.Clock.Now())
		if err != nil {
			return mapErr(err)
		}
		return s.record(ctx, p, audit.UserProfileUpdated, map[string]any{"fields": fields})
	})
	if err != nil {
		return auth.UserView{}, err
	}
	return view, nil
}

// Delete anonymizes the principal's account (spec 7.8). The password is
// verified first, outside the transaction, so no pooled connection waits on
// the argon2id semaphore. Everything else commits or rolls back together.
func (s *Service) Delete(ctx context.Context, p authz.Principal, password secret.String) error {
	if p.Role != authz.RoleDriver {
		return apperr.Forbidden(errcodes.AuthForbidden)
	}
	if err := s.d.Accounts.VerifyPassword(ctx, p.UserID, password); err != nil {
		return err
	}
	return s.d.Tx.InTx(ctx, func(ctx context.Context) error {
		// The profile goes first: its update only matches a live user.
		if err := s.d.Repo.AnonymizeProfile(ctx, p.UserID, s.d.Clock.Now()); err != nil {
			return mapErr(err)
		}
		if err := s.d.Accounts.Erase(ctx, p.UserID); err != nil {
			return err
		}
		if err := s.d.Vehicles.EraseOwner(ctx, p.UserID); err != nil {
			return err
		}
		if err := s.d.Notify.ScrubPendingForUser(ctx, p.UserID); err != nil {
			return fmt.Errorf("users: scrub outbox: %w", err)
		}
		return s.record(ctx, p, audit.UserDeleted, nil)
	})
}

// AcceptLegal records that the principal accepted the current terms and
// privacy versions. It fails with legal.version_outdated unless both are
// current.
func (s *Service) AcceptLegal(ctx context.Context, p authz.Principal, terms, privacy string) error {
	if p.Role != authz.RoleDriver {
		return apperr.Forbidden(errcodes.AuthForbidden)
	}
	if err := s.d.Legal.ValidateAcceptance(ctx, terms, privacy); err != nil {
		return err
	}
	return s.d.Tx.InTx(ctx, func(ctx context.Context) error {
		if err := s.d.Repo.SetLegalAcceptance(ctx, p.UserID, terms, privacy, s.d.Clock.Now()); err != nil {
			return err
		}
		return s.record(ctx, p, audit.UserLegalAccepted,
			map[string]any{"terms_version": terms, "privacy_version": privacy})
	})
}

func (s *Service) record(ctx context.Context, p authz.Principal, action string, after map[string]any) error {
	uid := p.UserID
	err := s.d.Audit.Record(ctx, audit.Entry{
		Action: action, Actor: audit.ActorUser, ActorUserID: &uid,
		EntityType: "user", EntityID: p.UserPublicID.String(), After: after,
	})
	if err != nil {
		return fmt.Errorf("users: audit %s: %w", action, err)
	}
	return nil
}

// mapErr turns the storage sentinel into the API error. The principal's user
// is gone only when the account was deleted after the token was checked.
func mapErr(err error) error {
	if errors.Is(err, ErrNotFound) {
		return apperr.Unauthenticated(errcodes.AuthUnauthenticated)
	}
	return err
}
