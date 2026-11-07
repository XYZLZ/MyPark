package middleware

import "net/http"

// SecurityHeaders are the headers set on every response (OC-32). The API
// serves only JSON, so the CSP forbids everything and Cache-Control forbids
// storing responses. Recover re-applies them after dropping stale headers.
var SecurityHeaders = map[string]string{
	"X-Content-Type-Options":  "nosniff",
	"Cache-Control":           "no-store",
	"Referrer-Policy":         "no-referrer",
	"Content-Security-Policy": "default-src 'none'; frame-ancestors 'none'",
}

// SecureHeaders sets SecurityHeaders before calling next.
func SecureHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		applySecurityHeaders(w.Header())
		next.ServeHTTP(w, r)
	})
}

func applySecurityHeaders(h http.Header) {
	for k, v := range SecurityHeaders {
		h.Set(k, v)
	}
}
