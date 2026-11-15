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
		{"DeleteAccountUser", ratelimit.DeleteAccountUser, 5, 15 * time.Minute, "delme:", ratelimit.KeyUser, false},
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

func TestRules_M1RulesStayShared(t *testing.T) {
	for _, r := range []ratelimit.Rule{
		ratelimit.LoginIPEmail, ratelimit.LoginEmail, ratelimit.RegisterIP, ratelimit.CodeRequestIP,
		ratelimit.CodeRequestEmail, ratelimit.CodeSubmitIP, ratelimit.RefreshIP,
		ratelimit.VehicleWriteUser, ratelimit.DeleteAccountUser,
	} {
		if r.Scope != ratelimit.ScopeShared {
			t.Errorf("%s scope = %d, want ScopeShared", r.Name, r.Scope)
		}
	}
	if ratelimit.ScopeShared != 0 {
		t.Error("ScopeShared must be the zero value")
	}
}

func TestRules_M2LocalRules(t *testing.T) {
	tests := []struct {
		rule   ratelimit.Rule
		name   string
		limit  int
		window time.Duration
		prefix string
	}{
		{ratelimit.DeviceIP, "device_ip", 1000, time.Second, "dev_ip:"},
		{ratelimit.PublicReadIP, "public_read_ip", 120, time.Minute, "pub:"},
		{ratelimit.SSEConnectIP, "sse_connect_ip", 30, time.Minute, "sse:"},
	}
	for _, tc := range tests {
		r := tc.rule
		if r.Name != tc.name || r.Limit != tc.limit || r.Window != tc.window || r.Prefix != tc.prefix ||
			r.Key != ratelimit.KeyIP || r.Scope != ratelimit.ScopeLocal || r.FailOpen {
			t.Errorf("%s = %+v", tc.name, r)
		}
	}
}

func TestDeviceRule(t *testing.T) {
	want := ratelimit.Rule{Name: "device", Limit: 100, Window: time.Second, Key: ratelimit.KeyDevice, Prefix: "dev:", Scope: ratelimit.ScopeLocal}
	if got := ratelimit.DeviceRule(100); got != want {
		t.Errorf("DeviceRule(100) = %+v, want %+v", got, want)
	}
	// KeyDevice is appended after the M1 kinds, so their values never move.
	if ratelimit.KeyIP != 0 || ratelimit.KeyUser != 1 || ratelimit.KeyCustom != 2 || ratelimit.KeyDevice != 3 {
		t.Error("KeyKind values were renumbered")
	}
}

// One IPv6 client usually holds a whole /64, so the in-memory (ScopeLocal)
// rules count the /64 as one client. IPv4 and IPv4-mapped IPv6 keep their
// per-address key, and the M1 Postgres (ScopeShared) rules are unchanged.
func TestRuleIPKey(t *testing.T) {
	a := netip.MustParseAddr("2001:db8:1:2::1")
	sameNet := netip.MustParseAddr("2001:db8:1:2:ffff:ffff:ffff:fffe")
	otherNet := netip.MustParseAddr("2001:db8:1:3::1")
	for _, rule := range []ratelimit.Rule{ratelimit.DeviceIP, ratelimit.PublicReadIP, ratelimit.SSEConnectIP} {
		if ka, kb := ratelimit.RuleIPKey(rule, a), ratelimit.RuleIPKey(rule, sameNet); ka != kb {
			t.Errorf("%s: same /64 keyed %q and %q, want one key", rule.Name, ka, kb)
		}
		if ka, kc := ratelimit.RuleIPKey(rule, a), ratelimit.RuleIPKey(rule, otherNet); ka == kc {
			t.Errorf("%s: different /64s share key %q", rule.Name, ka)
		}
		if got := ratelimit.RuleIPKey(rule, netip.MustParseAddr("203.0.113.9")); got != "203.0.113.9" {
			t.Errorf("%s: IPv4 key = %q, want the address", rule.Name, got)
		}
		if got := ratelimit.RuleIPKey(rule, netip.MustParseAddr("::ffff:203.0.113.9")); got != "203.0.113.9" {
			t.Errorf("%s: mapped IPv4 key = %q, want the unmapped address", rule.Name, got)
		}
		if got := ratelimit.RuleIPKey(rule, netip.Addr{}); got != "ip:unknown" {
			t.Errorf("%s: invalid addr key = %q, want ip:unknown", rule.Name, got)
		}
	}
	// ScopeShared (M1, Postgres) keeps the full address.
	if ka, kb := ratelimit.RuleIPKey(ratelimit.RegisterIP, a), ratelimit.RuleIPKey(ratelimit.RegisterIP, sameNet); ka == kb || ka != ratelimit.IPKey(a) {
		t.Errorf("RegisterIP keys = %q, %q; want the per-address IPKey", ka, kb)
	}
}

func TestClientNet(t *testing.T) {
	tests := []struct{ in, want string }{
		{"203.0.113.9", "203.0.113.9"},
		{"::ffff:203.0.113.9", "203.0.113.9"},
		{"2001:db8:1:2:aaaa:bbbb:cccc:dddd", "2001:db8:1:2::"},
		{"fe80::1%eth0", "fe80::"},
	}
	for _, tc := range tests {
		if got := ratelimit.ClientNet(netip.MustParseAddr(tc.in)); got != netip.MustParseAddr(tc.want) {
			t.Errorf("ClientNet(%s) = %s, want %s", tc.in, got, tc.want)
		}
	}
	if got := ratelimit.ClientNet(netip.Addr{}); got.IsValid() {
		t.Errorf("ClientNet(zero) = %s, want the zero Addr", got)
	}
}
