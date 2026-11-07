// Package authn holds the per-route gates that sit between the router and a
// handler: bearer authentication, the role gate and the legal re-acceptance
// gate. It depends on an interface, so it never imports a feature's use cases.
package authn

import (
	"context"
	"log/slog"
	"net/http"
	"strings"

	"github.com/XYZLZ/MyPark/apps/server/internal/domain/apperr"
	"github.com/XYZLZ/MyPark/apps/server/internal/domain/authz"
	"github.com/XYZLZ/MyPark/apps/server/internal/domain/errcodes"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/legal"
	"github.com/XYZLZ/MyPark/apps/server/internal/transport/http/httpx"
	"github.com/XYZLZ/MyPark/apps/server/internal/transport/http/reqctx"
)

// Authenticator resolves a bearer token (without the scheme) to a principal.
type Authenticator interface {
	Authenticate(ctx context.Context, bearer string) (authz.Principal, error)
}

// Gates builds the middlewares the router composes per route.
type Gates struct {
	a     Authenticator
	log   *slog.Logger
	legal legal.Current
}

// Option configures Gates.
type Option func(*Gates)

// WithLegal gives the legal gate the source of the current versions it
// reports in its error details. Without it the gate still blocks, with empty
// details.
func WithLegal(l legal.Current) Option { return func(g *Gates) { g.legal = l } }

// New returns Gates over a.
func New(a Authenticator, log *slog.Logger, opts ...Option) *Gates {
	g := &Gates{a: a, log: log}
	for _, o := range opts {
		o(g)
	}
	return g
}

// Authn requires a valid bearer token, puts the principal in the request
// context and records the opaque user id for the access log.
func (g *Gates) Authn(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, ok := bearerToken(r.Header.Get("Authorization"))
		if !ok {
			httpx.WriteError(w, r, g.log, apperr.Unauthenticated(errcodes.AuthUnauthenticated))
			return
		}
		p, err := g.a.Authenticate(r.Context(), token)
		if err != nil {
			httpx.WriteError(w, r, g.log, err)
			return
		}
		ctx := authz.WithPrincipal(r.Context(), p)
		if f := reqctx.Fields(ctx); f != nil {
			f.UserPublicID = p.UserPublicID.String()
		}
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// Roles allows only principals with one of roles. It must run after Authn.
func (g *Gates) Roles(roles []authz.Role) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			p, ok := authz.PrincipalFrom(r.Context())
			switch {
			case !ok:
				httpx.WriteError(w, r, g.log, apperr.Unauthenticated(errcodes.AuthUnauthenticated))
			case !p.Is(roles...):
				httpx.WriteError(w, r, g.log, apperr.Forbidden(errcodes.AuthForbidden))
			default:
				next.ServeHTTP(w, r)
			}
		})
	}
}

// Legal blocks a principal that owes a legal re-acceptance (OC-15). The
// Authenticator never sets LegalPending for an admin, so admins pass. Routes
// that must stay reachable are registered LegalExempt and skip this gate.
func (g *Gates) Legal(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, ok := authz.PrincipalFrom(r.Context())
		if !ok {
			httpx.WriteError(w, r, g.log, apperr.Unauthenticated(errcodes.AuthUnauthenticated))
			return
		}
		if !p.LegalPending {
			next.ServeHTTP(w, r)
			return
		}
		e := apperr.Forbidden(errcodes.LegalAcceptanceRequired)
		if g.legal != nil {
			cur, err := g.legal.Versions(r.Context())
			if err != nil {
				httpx.WriteError(w, r, g.log, err)
				return
			}
			e.WithDetails(map[string]any{
				"terms_version":   cur.Terms.Version,
				"privacy_version": cur.Privacy.Version,
			})
		}
		httpx.WriteError(w, r, g.log, e)
	})
}

// bearerToken extracts the token from an "Authorization: Bearer <token>"
// header. The scheme is case-insensitive (RFC 7235); a missing, empty or
// space-containing token is rejected.
func bearerToken(h string) (string, bool) {
	scheme, token, found := strings.Cut(h, " ")
	if !found || !strings.EqualFold(scheme, "Bearer") || token == "" || strings.ContainsAny(token, " \t") {
		return "", false
	}
	return token, true
}
