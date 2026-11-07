package ratelimit_test

import (
	"net/netip"
	"testing"
	"time"

	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/ratelimit"
)

func TestRules_MatchSpec(t *testing.T) {
	tests := []struct {
		name     string
		rule     ratelimit.Rule
		limit    int
		window   time.Duration
		prefix   string
		key      ratelimit.KeyKind
		failOpen bool
	}{
		{"LoginIPEmail", ratelimit.LoginIPEmail, 5, time.Minute, "login:", ratelimit.KeyCustom, false},
		{"LoginEmail", ratelimit.LoginEmail, 20, time.Hour, "login_e:", ratelimit.KeyCustom, false},
		{"RegisterIP", ratelimit.RegisterIP, 3, time.Minute, "reg:", ratelimit.KeyIP, false},
		{"CodeRequestIP", ratelimit.CodeRequestIP, 3, time.Minute, "code:", ratelimit.KeyIP, false},
		{"CodeRequestEmail", ratelimit.CodeRequestEmail, 5, time.Hour, "code_e:", ratelimit.KeyCustom, false},
		{"CodeSubmitIP", ratelimit.CodeSubmitIP, 10, time.Minute, "codesub:", ratelimit.KeyIP, false},
		{"RefreshIP", ratelimit.RefreshIP, 60, time.Minute, "refresh:", ratelimit.KeyIP, false},
		{"VehicleWriteUser", ratelimit.VehicleWriteUser, 20, time.Minute, "veh:", ratelimit.KeyUser, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := tc.rule
			if r.Limit != tc.limit || r.Window != tc.window || r.Prefix != tc.prefix ||
				r.Key != tc.key || r.FailOpen != tc.failOpen {
				t.Errorf("%s = %+v", tc.name, r)
			}
			if r.Name == "" {
				t.Errorf("%s has empty Name", tc.name)
			}
		})
	}
}

func TestNoop_AlwaysAllows(t *testing.T) {
	d, err := ratelimit.Noop{}.Allow(t.Context(), ratelimit.LoginEmail, "k")
	if err != nil || !d.Allowed {
		t.Errorf("Noop = %+v, %v", d, err)
	}
}

func TestIPKey_InvalidAddrUsesFixedKey(t *testing.T) {
	if got := ratelimit.IPKey(netip.Addr{}); got != "ip:unknown" {
		t.Errorf("IPKey(zero) = %q, want ip:unknown", got)
	}
	if got := ratelimit.IPKey(netip.MustParseAddr("203.0.113.9")); got != "203.0.113.9" {
		t.Errorf("IPKey(v4) = %q", got)
	}
	if got := ratelimit.IPKey(netip.MustParseAddr("::ffff:203.0.113.9")); got != "203.0.113.9" {
		t.Errorf("IPKey(mapped) = %q, want unmapped", got)
	}
}
