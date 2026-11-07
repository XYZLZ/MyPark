// Package auth owns account credentials and lifecycle: registration, email
// verification and, in later slices, login, sessions and password reset.
package auth

import "strings"

// NormalizeEmail trims and lowercases an address. Every email-keyed lookup
// and rate-limit key uses the result, so case and whitespace variants of one
// address share an account and a bucket.
func NormalizeEmail(raw string) string {
	return strings.ToLower(strings.TrimSpace(raw))
}
