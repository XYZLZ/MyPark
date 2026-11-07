package middleware

import (
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/XYZLZ/MyPark/apps/server/internal/domain/apperr"
	"github.com/XYZLZ/MyPark/apps/server/internal/domain/authz"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/ratelimit"
	"github.com/XYZLZ/MyPark/apps/server/internal/transport/http/httpx"
)

// RateLimit returns the per-route rate-limit middleware factory. KeyIP rules
// are keyed by the client IP (ClientIP must run first), KeyUser rules by the
// authenticated user's public id (Authn must run first). A denied request
// gets 429 with Retry-After. A limiter failure returns 500 for fail-closed
// rules and is logged and ignored for FailOpen rules (spec 10.2). Throttling
// is logged at warn level with the rule name and the hashed key, never the IP
// or user id, and is not audited.
func RateLimit(l ratelimit.Limiter, log *slog.Logger) func(ratelimit.Rule) func(http.Handler) http.Handler {
	return func(rule ratelimit.Rule) func(http.Handler) http.Handler {
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				key, err := bucketKey(r, rule)
				var d ratelimit.Decision
				if err == nil {
					d, err = l.Allow(r.Context(), rule, key)
				}
				if err != nil {
					if rule.FailOpen {
						log.ErrorContext(r.Context(), "ratelimit: limiter failed, allowing request",
							"rule", rule.Name, "err", err)
						next.ServeHTTP(w, r)
						return
					}
					httpx.WriteError(w, r, log, fmt.Errorf("ratelimit: %s: %w", rule.Name, err))
					return
				}
				if !d.Allowed {
					log.WarnContext(r.Context(), "ratelimit: throttled",
						"rule", rule.Name,
						"key_hash", hex.EncodeToString(ratelimit.HashKey(rule, key)))
					httpx.WriteError(w, r, log, apperr.RateLimited(d.RetryAfter))
					return
				}
				next.ServeHTTP(w, r)
			})
		}
	}
}

// bucketKey derives the raw bucket key of rule from the request.
func bucketKey(r *http.Request, rule ratelimit.Rule) (string, error) {
	switch rule.Key {
	case ratelimit.KeyIP:
		return ratelimit.IPKey(ClientIPFrom(r.Context())), nil
	case ratelimit.KeyUser:
		p, ok := authz.PrincipalFrom(r.Context())
		if !ok {
			return "", errors.New("user-keyed rule without an authenticated principal")
		}
		return p.UserPublicID.String(), nil
	default:
		return "", fmt.Errorf("rule %s is not keyed by the router", rule.Name)
	}
}
