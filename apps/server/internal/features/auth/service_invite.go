package auth

import (
	"cmp"
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/XYZLZ/MyPark/apps/server/internal/domain/apperr"
	"github.com/XYZLZ/MyPark/apps/server/internal/domain/authz"
	"github.com/XYZLZ/MyPark/apps/server/internal/domain/errcodes"
	"github.com/XYZLZ/MyPark/apps/server/internal/domain/secret"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/audit"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/notifications"
)

// InviteInput is an admin invitation. Locale defaults to es-DO.
type InviteInput struct {
	Email, FirstName, LastName, Locale string
}

// InviteResult is the invited account. Created is false when an existing
// invitee got a fresh code.
type InviteResult struct {
	User    UserView
	Created bool
}

// CreateInvitedAdmin implements Accounts.
func (a accounts) CreateInvitedAdmin(ctx context.Context, actor authz.Principal, in InviteInput) (InviteResult, error) {
	s := a.s
	if !actor.Is(authz.RoleAdmin) {
		return InviteResult{}, apperr.Forbidden(errcodes.AuthForbidden)
	}
	in.Email = NormalizeEmail(in.Email)
	in.Locale = cmp.Or(in.Locale, defaultLocale)
	var res InviteResult
	var err error
	// A concurrent invite of the same address makes the insert fail once; the
	// retry then sees the other row and takes the reissue path.
	for range 2 {
		res = InviteResult{}
		err = s.d.Tx.InTx(ctx, func(ctx context.Context) error {
			var err error
			res, err = s.inviteAdmin(ctx, actor, NewAdmin{
				Email: in.Email, FirstName: in.FirstName, LastName: in.LastName, Locale: in.Locale,
				Now: s.d.Clock.Now(),
			})
			return err
		})
		if !errors.Is(err, ErrEmailTaken) {
			break
		}
	}
	if errors.Is(err, ErrEmailTaken) {
		return InviteResult{}, apperr.Conflict(errcodes.UserEmailTaken)
	}
	return res, err
}

func (s *Service) inviteAdmin(ctx context.Context, actor authz.Principal, a NewAdmin) (InviteResult, error) {
	u, found, err := s.d.Repo.LockUserByEmail(ctx, a.Email)
	if err != nil {
		return InviteResult{}, err
	}
	var (
		action string
		meta   map[string]any
		res    InviteResult
	)
	switch {
	case !found:
		if u, err = s.d.Repo.InsertInvitedAdmin(ctx, a); err != nil {
			return InviteResult{}, err
		}
		action, meta, res.Created = audit.AdminCreated, map[string]any{"role": RoleAdmin}, true
	case u.Role == RoleAdmin && u.Status == StatusPending:
		// OC-39: the invitee already exists; only the code is renewed.
		action = audit.AdminInvitationReissued
	case u.Role == RoleDriver && u.Status == StatusPending:
		// OC-39: an unverified registration never proved ownership of the
		// address, so it cannot block an invite.
		ok, err := s.d.Repo.TakeOverPendingDriver(ctx, u.ID, a)
		if err != nil {
			return InviteResult{}, err
		}
		if !ok {
			return InviteResult{}, ErrEmailTaken
		}
		now := s.d.Clock.Now()
		for _, p := range []Purpose{PurposeVerifyEmail, PurposeResetPassword} {
			if err := s.d.Repo.ConsumeLiveCodes(ctx, u.ID, p, now); err != nil {
				return InviteResult{}, err
			}
		}
		u.Role, u.Locale = RoleAdmin, a.Locale
		action = audit.AdminCreated
		meta, res.Created = map[string]any{"role": RoleAdmin, audit.MetaReplacedPendingDriver: true}, true
	default:
		return InviteResult{}, apperr.Conflict(errcodes.UserEmailTaken)
	}
	// The stored row is the source of truth for the greeting: a re-issue
	// leaves the invitee's details as they were.
	if res.User, err = s.d.Repo.GetUserView(ctx, u.ID); err != nil {
		return InviteResult{}, err
	}
	if err := s.sendInvite(ctx, u, res.User); err != nil {
		return InviteResult{}, err
	}
	actorID := actor.UserID
	err = s.d.Audit.Record(ctx, audit.Entry{
		Action: action, Actor: audit.ActorAdmin, ActorUserID: &actorID,
		EntityType: "user", EntityID: u.PublicID.String(), After: meta,
	})
	if err != nil {
		return InviteResult{}, fmt.Errorf("auth: audit %s: %w", action, err)
	}
	return res, nil
}

// sendInvite issues the 48 h admin_invite code, consuming the live one, and
// enqueues the email in the invitee's locale.
func (s *Service) sendInvite(ctx context.Context, u UserRow, v UserView) error {
	code, codeID, err := s.issueCode(ctx, u, PurposeAdminInvite, InviteTTL)
	if err != nil {
		return err
	}
	uid := u.ID
	err = s.d.Notify.Enqueue(ctx, notifications.Email{
		Template:  notifications.TplAdminInvite,
		Locale:    u.Locale,
		To:        v.Email,
		UserID:    &uid,
		Data:      map[string]string{"code": code, "first_name": v.FirstName},
		DedupeKey: fmt.Sprintf("admin_invite:%d", codeID),
	})
	if err != nil {
		return fmt.Errorf("auth: enqueue admin invite: %w", err)
	}
	return nil
}

// AcceptInvitation sets the password of the pending admin invitee whose
// emailed code is code and activates the account (OC-19). It accepts only
// role='admin' rows in pending_verification; any other row, an unknown
// address and a wrong code are all the same auth.code_invalid (OC-38).
func (s *Service) AcceptInvitation(ctx context.Context, email string, code, password secret.String) error {
	email = NormalizeEmail(email)
	// Hash before the transaction so no connection waits on the argon2id
	// semaphore. It also keeps timing identical on the failing paths.
	hash, err := hashPassword(ctx, s.d.Hasher, password)
	if err != nil {
		return err
	}
	accepted := false
	// Commit first: a wrong code still persists its attempt count.
	err = s.d.Tx.InTx(ctx, func(ctx context.Context) error {
		u, found, err := s.d.Repo.LockPendingAdminByEmail(ctx, email)
		if err != nil {
			return err
		}
		if !found {
			// Same work shape against no row.
			_, err := s.checkCode(ctx, 0, uuid.Nil, PurposeAdminInvite, code.Reveal())
			return err
		}
		ok, err := s.checkCode(ctx, u.ID, u.PublicID, PurposeAdminInvite, code.Reveal())
		if err != nil || !ok {
			return err
		}
		now := s.d.Clock.Now()
		activated, err := s.d.Repo.ActivateInvitedAdmin(ctx, u.ID, hash, now)
		if err != nil {
			return err
		}
		if !activated {
			return errors.New("auth: locked invitee is no longer a pending admin")
		}
		uid := u.ID
		if err := s.recordAudit(ctx, audit.Entry{
			Action: audit.AdminInvitationAccepted, Actor: audit.ActorAdmin, ActorUserID: &uid,
			EntityType: "user", EntityID: u.PublicID.String(),
		}); err != nil {
			return err
		}
		accepted = true
		return nil
	})
	if err != nil {
		return err
	}
	if !accepted {
		return apperr.Invalid(errcodes.AuthCodeInvalid)
	}
	return nil
}
