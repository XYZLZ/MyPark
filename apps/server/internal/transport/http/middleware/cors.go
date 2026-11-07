package middleware

import (
	"net/http"
	"slices"
	"strings"
)

const (
	corsAllowMethods = "GET, POST, PATCH, DELETE"
	corsAllowHeaders = "Authorization, Content-Type, X-Request-Id"
	corsExposeHeader = "X-Request-Id, Retry-After"
	corsMaxAge       = "600"
)

// CORS allows cross-origin requests from the exact origins in allowed.
// Credentials are never allowed (auth is a bearer header). A preflight from
// an allowed origin is answered here with 204, before the router, so POST-only
// routes do not turn it into a 405. Requests from other origins pass through
// without any Access-Control-* header.
func CORS(allowed []string) func(http.Handler) http.Handler {
	allowed = slices.Clone(allowed)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			h := w.Header()
			if origin == "" {
				next.ServeHTTP(w, r)
				return
			}
			h.Add("Vary", "Origin")
			if !slices.Contains(allowed, origin) {
				next.ServeHTTP(w, r)
				return
			}
			h.Set("Access-Control-Allow-Origin", origin)
			if r.Method == http.MethodOptions && strings.TrimSpace(r.Header.Get("Access-Control-Request-Method")) != "" {
				h.Add("Vary", "Access-Control-Request-Method")
				h.Add("Vary", "Access-Control-Request-Headers")
				h.Set("Access-Control-Allow-Methods", corsAllowMethods)
				h.Set("Access-Control-Allow-Headers", corsAllowHeaders)
				h.Set("Access-Control-Max-Age", corsMaxAge)
				w.WriteHeader(http.StatusNoContent)
				return
			}
			h.Set("Access-Control-Expose-Headers", corsExposeHeader)
			next.ServeHTTP(w, r)
		})
	}
}
