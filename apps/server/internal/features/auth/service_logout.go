package auth

import (
	"context"
	"fmt"

	"github.com/XYZLZ/MyPark/apps/server/internal/domain/authz"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/audit"
)

// Logout revokes the session the principal authenticated with. It is
// idempotent in the database: a session that is already revoked keeps its
// reason. A second call with the same token never gets here, because the
// per-request session check rejects it first.
func (s *Service) Logout(ctx context.Context, p authz.Principal) error {
	return s.d.Tx.InTx(ctx, func(ctx context.Context) error {
		revoked, err := s.d.Repo.RevokeSession(ctx, p.SessionID, ReasonLogout, s.d.Clock.Now())
		if err != nil {
			return err
		}
		n := 0
		if revoked {
			n = 1
		}
		return s.auditRevocation(ctx, p, audit.AuthLogout, "session", p.SessionPublicID.String(), n)
	})
}

// LogoutAll revokes every live session of the principal's user and returns
// how many it revoked.
func (s *Service) LogoutAll(ctx context.Context, p authz.Principal) (int, error) {
	var revoked int
	err := s.d.Tx.InTx(ctx, func(ctx context.Context) error {
		var err error
		if revoked, err = s.RevokeAllSessions(ctx, p.UserID, ReasonLogoutAll); err != nil {
			return err
		}
		return s.auditRevocation(ctx, p, audit.AuthLogoutAll, "user", p.UserPublicID.String(), revoked)
	})
	if err != nil {
		return 0, err
	}
	return revoked, nil
}

// RevokeAllSessions revokes every live session of a user with reason, one of
// the Reason constants, and returns how many it revoked. Sessions that are
// already revoked keep their own reason. It does not audit: password reset
// and account deletion record their own action, and they call this inside
// their transaction.
func (s *Service) RevokeAllSessions(ctx context.Context, userID int64, reason string) (int, error) {
	return s.d.Repo.RevokeUserSessions(ctx, userID, reason, s.d.Clock.Now())
}

func (s *Service) auditRevocation(ctx context.Context, p authz.Principal, action, entityType, entityID string, revoked int) error {
	uid := p.UserID
	err := s.d.Audit.Record(ctx, audit.Entry{
		Action: action, Actor: audit.ActorUser, ActorUserID: &uid,
		EntityType: entityType, EntityID: entityID,
		After: map[string]any{"sessions_revoked": revoked},
	})
	if err != nil {
		return fmt.Errorf("auth: audit %s: %w", action, err)
	}
	return nil
}
