package auth

import (
	"context"
	"fmt"

	"github.com/XYZLZ/MyPark/apps/server/internal/domain/apperr"
	"github.com/XYZLZ/MyPark/apps/server/internal/domain/authz"
	"github.com/XYZLZ/MyPark/apps/server/internal/domain/errcodes"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/legal"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/clock"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/jwt"
)

// Authenticator turns a bearer access token into an authz.Principal.
type Authenticator struct {
	verifier *jwt.Verifier
	repo     Repo
	legal    legal.Current
	clock    clock.Clock
}

// NewAuthenticator returns an Authenticator over v and repo.
func NewAuthenticator(v *jwt.Verifier, repo Repo, l legal.Current, c clock.Clock) *Authenticator {
	return &Authenticator{verifier: v, repo: repo, legal: l, clock: c}
}

// Authenticate checks a bearer token and the state of its session on every
// request (spec 7.3, OC-4), so a revoked session, a suspended or deleted
// user and a role change take effect at once, not when the token expires. It
// runs exactly one query. The database is authoritative: the role in the
// token is only compared with it.
func (a *Authenticator) Authenticate(ctx context.Context, bearer string) (authz.Principal, error) {
	claims, err := a.verifier.Verify(bearer)
	if err != nil {
		return authz.Principal{}, apperr.Unauthenticated(errcodes.AuthUnauthenticated)
	}
	row, found, err := a.repo.GetSessionPrincipal(ctx, claims.SessionID, claims.Subject, a.clock.Now())
	if err != nil {
		return authz.Principal{}, err
	}
	switch {
	case !found:
		return authz.Principal{}, apperr.Unauthenticated(errcodes.AuthUnauthenticated)
	case row.Revoked || row.Expired:
		return authz.Principal{}, apperr.Unauthenticated(errcodes.AuthSessionRevoked)
	}
	switch row.Status {
	case StatusActive:
	case StatusSuspended:
		return authz.Principal{}, apperr.Forbidden(errcodes.AuthAccountSuspended)
	default: // deleted, pending_verification, or anything unknown
		return authz.Principal{}, apperr.Unauthenticated(errcodes.AuthSessionRevoked)
	}
	role, ok := authz.ParseRole(row.Role)
	if !ok || role != claims.Role {
		return authz.Principal{}, apperr.Unauthenticated(errcodes.AuthSessionRevoked)
	}
	pending := false
	if role == authz.RoleDriver {
		if pending, err = a.legalPending(ctx, row); err != nil {
			return authz.Principal{}, err
		}
	}
	return authz.Principal{
		UserID: row.UserID, UserPublicID: claims.Subject, Role: role,
		SessionID: row.SessionID, SessionPublicID: claims.SessionID, LegalPending: pending,
	}, nil
}

// legalPending reports whether a driver has not accepted the current legal
// documents. Admins are exempt (OC-15), so only drivers get here.
func (a *Authenticator) legalPending(ctx context.Context, row SessionPrincipal) (bool, error) {
	cur, err := a.legal.Versions(ctx)
	if err != nil {
		return false, fmt.Errorf("auth: current legal versions: %w", err)
	}
	return legalOutdated(row.TermsVersion, row.PrivacyVersion, cur), nil
}

// legalOutdated reports whether the accepted versions differ from the current ones.
func legalOutdated(terms, privacy *string, cur legal.Versions) bool {
	return terms == nil || *terms != cur.Terms.Version || privacy == nil || *privacy != cur.Privacy.Version
}
