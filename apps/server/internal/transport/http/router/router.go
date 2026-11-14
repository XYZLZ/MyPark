// Package router registers HTTP routes on a ServeMux and refuses any route
// that lacks an explicit, consistent access policy (deny-by-default).
package router

import (
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/XYZLZ/MyPark/apps/server/internal/domain/authz"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/ratelimit"
)

// Role sets for Policy.Roles.
var (
	DriverOnly = []authz.Role{authz.RoleDriver}
	AdminOnly  = []authz.Role{authz.RoleAdmin}
	AnyRole    = []authz.Role{authz.RoleDriver, authz.RoleAdmin}
)

const (
	adminPrefix  = "/v1/admin/"
	devicePrefix = "/v1/devices/"
)

// Policy is the mandatory access policy of a route: exactly one of Public,
// Roles or Device (M2C-48).
type Policy struct {
	Public      bool
	Roles       []authz.Role
	LegalExempt bool
	Limit       *ratelimit.Rule
	// Device routes authenticate a device by HMAC (design §7.3). Their rate
	// limits are fixed by the router (DeviceIP, then Middlewares.DeviceLimit),
	// so Limit must stay nil. A user bearer token is ignored on them.
	Device bool
}

// Middlewares are the building blocks the router composes per route.
type Middlewares struct {
	ClientIP, Authn, Legal func(http.Handler) http.Handler
	Roles                  func([]authz.Role) func(http.Handler) http.Handler
	RateLimit              func(ratelimit.Rule) func(http.Handler) http.Handler
	// DeviceAuth verifies the device HMAC and stores the device in the context.
	DeviceAuth func(http.Handler) http.Handler
	// DeviceLimit is the per-device rule run after DeviceAuth (KeyDevice).
	DeviceLimit ratelimit.Rule
}

// Route is a registry entry.
type Route struct {
	Pattern string
	Policy  Policy
}

// Router registers routes on a mux enforcing policies.
type Router struct {
	mux    *http.ServeMux
	mw     Middlewares
	routes []Route
}

// New returns a Router registering on mux.
func New(mux *http.ServeMux, mw Middlewares) *Router {
	return &Router{mux: mux, mw: mw}
}

// Handle registers h under pattern with policy p. It panics on an invalid
// policy or a missing middleware: these are programmer errors caught at startup.
func (r *Router) Handle(pattern string, p Policy, h http.HandlerFunc) {
	r.validate(pattern, p)

	var handler http.Handler = h
	if p.Device {
		// ClientIP -> RateLimit(DeviceIP) -> DeviceAuth -> RateLimit(DeviceLimit) -> h.
		handler = r.mw.RateLimit(r.mw.DeviceLimit)(handler)
		handler = r.mw.DeviceAuth(handler)
		handler = r.mw.RateLimit(ratelimit.DeviceIP)(handler)
		r.register(pattern, p, r.mw.ClientIP(handler))
		return
	}
	// Build inside-out so the execution order is:
	// ClientIP -> RateLimit(ip) -> Authn -> Roles -> Legal -> RateLimit(user) -> h.
	if p.Limit != nil && p.Limit.Key == ratelimit.KeyUser {
		handler = r.mw.RateLimit(*p.Limit)(handler)
	}
	if !p.Public {
		if !p.LegalExempt {
			handler = r.mw.Legal(handler)
		}
		handler = r.mw.Roles(p.Roles)(handler)
		handler = r.mw.Authn(handler)
	}
	if p.Limit != nil && p.Limit.Key == ratelimit.KeyIP {
		handler = r.mw.RateLimit(*p.Limit)(handler)
	}
	r.register(pattern, p, r.mw.ClientIP(handler))
}

func (r *Router) register(pattern string, p Policy, h http.Handler) {
	r.mux.Handle(pattern, h)
	r.routes = append(r.routes, Route{Pattern: pattern, Policy: p})
}

// Routes returns the registry sorted by pattern.
func (r *Router) Routes() []Route {
	out := slices.Clone(r.routes)
	slices.SortFunc(out, func(a, b Route) int { return strings.Compare(a.Pattern, b.Pattern) })
	return out
}

func (r *Router) validate(pattern string, p Policy) {
	fail := func(msg string) { panic(fmt.Sprintf("router: %q: %s", pattern, msg)) }

	kinds := 0
	for _, set := range []bool{p.Public, len(p.Roles) > 0, p.Device} {
		if set {
			kinds++
		}
	}
	if kinds != 1 {
		fail("policy must be exactly one of Public, Roles or Device")
	}
	if p.Device {
		r.validateDevice(pattern, p, fail)
		return
	}
	if p.Public && p.LegalExempt {
		fail("LegalExempt is meaningless on a public route")
	}
	if isUnder(pattern, devicePrefix) {
		fail("a /v1/devices/ path requires a Device policy")
	}
	if isUnder(pattern, adminPrefix) && (p.Public || !slices.Equal(p.Roles, AdminOnly)) {
		fail("admin path requires AdminOnly roles")
	}
	if p.Limit != nil && p.Limit.Key != ratelimit.KeyIP && p.Limit.Key != ratelimit.KeyUser {
		fail("route policy Limit must be KeyIP or KeyUser; handler-keyed rules belong in handlers")
	}
	if p.Limit != nil && p.Limit.Key == ratelimit.KeyUser && p.Public {
		fail("user-keyed rate limit requires an authenticated route")
	}

	if r.mw.ClientIP == nil {
		fail("Middlewares.ClientIP is nil")
	}
	if !p.Public {
		if r.mw.Authn == nil {
			fail("Middlewares.Authn is nil")
		}
		if r.mw.Roles == nil {
			fail("Middlewares.Roles is nil")
		}
		if !p.LegalExempt && r.mw.Legal == nil {
			fail("Middlewares.Legal is nil")
		}
	}
	if p.Limit != nil && r.mw.RateLimit == nil {
		fail("Middlewares.RateLimit is nil but the policy has a Limit")
	}
}

// validateDevice checks a Device policy (design §7.8): only on /v1/devices/
// paths, alone, and with every middleware of the device chain present.
func (r *Router) validateDevice(pattern string, p Policy, fail func(string)) {
	switch {
	case p.LegalExempt:
		fail("LegalExempt is meaningless on a device route")
	case p.Limit != nil:
		fail("a device route cannot declare a Limit; its limits are DeviceIP and Middlewares.DeviceLimit")
	case !isUnder(pattern, devicePrefix):
		fail("Device policy is valid only under /v1/devices/")
	case r.mw.ClientIP == nil:
		fail("Middlewares.ClientIP is nil")
	case r.mw.RateLimit == nil:
		fail("Middlewares.RateLimit is nil")
	case r.mw.DeviceAuth == nil:
		fail("Middlewares.DeviceAuth is nil")
	case r.mw.DeviceLimit.Key != ratelimit.KeyDevice || r.mw.DeviceLimit.Name == "":
		// The zero Rule is KeyIP with limit 0: refuse it rather than key by IP.
		fail("Middlewares.DeviceLimit must be a KeyDevice rule")
	}
}

// isUnder reports whether the path part of "METHOD /path" starts with prefix.
func isUnder(pattern, prefix string) bool {
	_, path, ok := strings.Cut(pattern, " ")
	if !ok {
		path = pattern
	}
	return strings.HasPrefix(strings.TrimSpace(path), prefix)
}

// PassthroughRateLimit is a no-op RateLimit middleware used until the real
// limiter is wired, so limited routes can register without panicking.
func PassthroughRateLimit(ratelimit.Rule) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler { return next }
}
