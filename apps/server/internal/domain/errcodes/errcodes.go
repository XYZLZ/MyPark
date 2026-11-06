// Package errcodes is the catalog of stable API error codes. Codes follow
// the "<domain>.<reason>" snake_case format and are the client contract:
// clients translate the code and never show the error message to end users.
package errcodes

import "slices"

// Code is a stable, machine-readable API error code.
type Code string

// Error codes. Add new codes here and to the catalog below.
const (
	InternalUnexpected   Code = "internal.unexpected"
	HTTPNotFound         Code = "http.not_found"
	HTTPMethodNotAllowed Code = "http.method_not_allowed"
	HealthNotReady       Code = "health.not_ready"
)

var catalog = []Code{
	InternalUnexpected,
	HTTPNotFound,
	HTTPMethodNotAllowed,
	HealthNotReady,
}

// All returns every known code, sorted ascending. The returned slice is a
// fresh copy and may be modified by the caller.
func All() []Code {
	out := slices.Clone(catalog)
	slices.Sort(out)
	return out
}
