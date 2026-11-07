// Package errcodes is the catalog of stable API error codes. Codes follow
// the "<domain>.<reason>" snake_case format and are the client contract:
// clients translate the code and never show the error message to end users.
package errcodes

//go:generate go run ./gen -out ../../../../../packages/contracts/src/error-codes.ts

import "slices"

// Code is a stable, machine-readable API error code.
type Code string

// Error codes. Add new codes here and to the catalog below.
const (
	InternalUnexpected   Code = "internal.unexpected"
	HTTPNotFound         Code = "http.not_found"
	HTTPMethodNotAllowed Code = "http.method_not_allowed"
	HealthNotReady       Code = "health.not_ready"
	ValidationFailed     Code = "validation.failed"

	AuthUnauthenticated      Code = "auth.unauthenticated"
	AuthSessionRevoked       Code = "auth.session_revoked"
	AuthRefreshInvalid       Code = "auth.refresh_invalid"
	AuthInvalidCredentials   Code = "auth.invalid_credentials"
	AuthEmailNotVerified     Code = "auth.email_not_verified"
	AuthAccountSuspended     Code = "auth.account_suspended"
	AuthForbidden            Code = "auth.forbidden"
	AuthPasswordIncorrect    Code = "auth.password_incorrect"
	AuthCodeInvalid          Code = "auth.code_invalid"
	LegalAcceptanceRequired  Code = "legal.acceptance_required"
	LegalVersionOutdated     Code = "legal.version_outdated"
	UserEmailTaken           Code = "user.email_taken"
	VehicleNotFound          Code = "vehicle.not_found"
	VehiclePlateTaken        Code = "vehicle.plate_taken"
	VehicleLimitReached      Code = "vehicle.limit_reached"
	VehicleInUse             Code = "vehicle.in_use"
	RateLimitExceeded        Code = "rate_limit.exceeded"
	HTTPInvalidJSON          Code = "http.invalid_json"
	HTTPPayloadTooLarge      Code = "http.payload_too_large"
	HTTPUnsupportedMediaType Code = "http.unsupported_media_type"
)

var catalog = []Code{
	InternalUnexpected,
	HTTPNotFound,
	HTTPMethodNotAllowed,
	HealthNotReady,
	ValidationFailed,
	AuthUnauthenticated,
	AuthSessionRevoked,
	AuthRefreshInvalid,
	AuthInvalidCredentials,
	AuthEmailNotVerified,
	AuthAccountSuspended,
	AuthForbidden,
	AuthPasswordIncorrect,
	AuthCodeInvalid,
	LegalAcceptanceRequired,
	LegalVersionOutdated,
	UserEmailTaken,
	VehicleNotFound,
	VehiclePlateTaken,
	VehicleLimitReached,
	VehicleInUse,
	RateLimitExceeded,
	HTTPInvalidJSON,
	HTTPPayloadTooLarge,
	HTTPUnsupportedMediaType,
}

// All returns every known code, sorted ascending. The returned slice is a
// fresh copy and may be modified by the caller.
func All() []Code {
	out := slices.Clone(catalog)
	slices.Sort(out)
	return out
}
