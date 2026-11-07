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

const adminPrefix = "/v1/admin/"

// Policy is the mandatory access policy of a route.
type Policy struct {
	Public      bool
	Roles       []authz.Role
	LegalExempt bool
	Limit       *ratelimit.Rule
}

// Middlewares are the building blocks the router composes per route.
type Middlewares struct {
	ClientIP, Authn, Legal func(http.Handler) http.Handler
	Roles                  func([]authz.Role) func(http.Handler) http.Handler
	RateLimit              func(ratelimit.Rule) func(http.Handler) http.Handler
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
	handler = r.mw.ClientIP(handler)

	r.mux.Handle(pattern, handler)
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

	switch {
	case !p.Public && len(p.Roles) == 0:
		fail("policy must be Public or declare Roles")
	case p.Public && len(p.Roles) > 0:
		fail("public route cannot declare Roles")
	case p.Public && p.LegalExempt:
		fail("LegalExempt is meaningless on a public route")
	}
	if isAdminPattern(pattern) && (p.Public || !slices.Equal(p.Roles, AdminOnly)) {
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

// isAdminPattern reports whether the path part of "METHOD /path" is under /v1/admin/.
func isAdminPattern(pattern string) bool {
	_, path, ok := strings.Cut(pattern, " ")
	if !ok {
		path = pattern
	}
	return strings.HasPrefix(strings.TrimSpace(path), adminPrefix)
}

// PassthroughRateLimit is a no-op RateLimit middleware used until the real
// limiter is wired, so limited routes can register without panicking.
func PassthroughRateLimit(ratelimit.Rule) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler { return next }
}
