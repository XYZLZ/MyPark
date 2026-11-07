package auth

import "time"

// RefreshSnapshot is what the refresh decision needs from the locked token,
// its session and its user. Found is false when no token has the presented
// hash.
type RefreshSnapshot struct {
	Found                    bool
	TokenRotatedAt           *time.Time
	SessionRevokedAt         *time.Time
	TokenExpiresAt           time.Time
	SessionAbsoluteExpiresAt time.Time
	UserStatus               string
}

// RefreshOutcome is the result of DecideRefresh.
type RefreshOutcome int

// Refresh outcomes. RefreshInvalid answers 401 auth.refresh_invalid. Every
// other failure answers 401 auth.session_revoked.
const (
	RefreshOK RefreshOutcome = iota
	RefreshInvalid
	RefreshRevoked
	RefreshReuse
	RefreshUserInactive
)

// DecideRefresh applies the checks of spec 7.4 in order: unknown token,
// revoked session, reuse of a rotated token, expiry (token, then the absolute
// cap of the session), inactive user. Reuse wins over expiry, so replaying an
// old token after its lifetime still revokes the family.
func DecideRefresh(s RefreshSnapshot, now time.Time) RefreshOutcome {
	switch {
	case !s.Found:
		return RefreshInvalid
	case s.SessionRevokedAt != nil:
		return RefreshRevoked
	case s.TokenRotatedAt != nil:
		return RefreshReuse
	case !s.TokenExpiresAt.After(now), !s.SessionAbsoluteExpiresAt.After(now):
		return RefreshInvalid
	case s.UserStatus != StatusActive:
		return RefreshUserInactive
	}
	return RefreshOK
}
