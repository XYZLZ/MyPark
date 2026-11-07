package auth_test

import (
	"testing"
	"time"

	"github.com/XYZLZ/MyPark/apps/server/internal/features/auth"
)

func TestDecideRefresh_Table(t *testing.T) {
	now := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	past, future := now.Add(-time.Hour), now.Add(time.Hour)
	live := auth.RefreshSnapshot{
		Found: true, TokenExpiresAt: future, SessionAbsoluteExpiresAt: future, UserStatus: "active",
	}
	with := func(mod func(*auth.RefreshSnapshot)) auth.RefreshSnapshot {
		s := live
		mod(&s)
		return s
	}
	tests := []struct {
		name string
		s    auth.RefreshSnapshot
		want auth.RefreshOutcome
	}{
		{"not found", auth.RefreshSnapshot{}, auth.RefreshInvalid},
		{"session revoked", with(func(s *auth.RefreshSnapshot) { s.SessionRevokedAt = &past }), auth.RefreshRevoked},
		{"revoked beats rotated", with(func(s *auth.RefreshSnapshot) {
			s.SessionRevokedAt, s.TokenRotatedAt = &past, &past
		}), auth.RefreshRevoked},
		{"rotated is reuse", with(func(s *auth.RefreshSnapshot) { s.TokenRotatedAt = &past }), auth.RefreshReuse},
		{"rotated and expired is still reuse", with(func(s *auth.RefreshSnapshot) {
			s.TokenRotatedAt, s.TokenExpiresAt = &past, past
		}), auth.RefreshReuse},
		{"token expired", with(func(s *auth.RefreshSnapshot) { s.TokenExpiresAt = past }), auth.RefreshInvalid},
		{"token expires exactly now", with(func(s *auth.RefreshSnapshot) { s.TokenExpiresAt = now }), auth.RefreshInvalid},
		{"absolute cap passed", with(func(s *auth.RefreshSnapshot) { s.SessionAbsoluteExpiresAt = past }), auth.RefreshInvalid},
		{"absolute cap exactly now", with(func(s *auth.RefreshSnapshot) { s.SessionAbsoluteExpiresAt = now }), auth.RefreshInvalid},
		{"expired beats inactive user", with(func(s *auth.RefreshSnapshot) {
			s.TokenExpiresAt, s.UserStatus = past, "suspended"
		}), auth.RefreshInvalid},
		{"suspended", with(func(s *auth.RefreshSnapshot) { s.UserStatus = "suspended" }), auth.RefreshUserInactive},
		{"deleted", with(func(s *auth.RefreshSnapshot) { s.UserStatus = "deleted" }), auth.RefreshUserInactive},
		{"pending", with(func(s *auth.RefreshSnapshot) { s.UserStatus = "pending_verification" }), auth.RefreshUserInactive},
		{"fresh", live, auth.RefreshOK},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := auth.DecideRefresh(tc.s, now); got != tc.want {
				t.Errorf("DecideRefresh = %d, want %d", got, tc.want)
			}
		})
	}
}
