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

	DeviceUnauthenticated      Code = "device.unauthenticated"
	DeviceTimestampOutOfWindow Code = "device.timestamp_out_of_window"
	DeviceEventNotAllowed      Code = "device.event_not_allowed"
	DeviceEventConflict        Code = "device.event_conflict"

	LotNotFound     Code = "lot.not_found"
	LotSpaceUnknown Code = "lot.space_unknown"

	GateLotFull           Code = "gate.lot_full"
	GateLotPhysicallyFull Code = "gate.lot_physically_full"
	GateLotClosed         Code = "gate.lot_closed"
	GateCredentialInvalid Code = "gate.credential_invalid"
	GateCredentialInUse   Code = "gate.credential_in_use"
	GatePaymentRequired   Code = "gate.payment_required"
	GateAmountMismatch    Code = "gate.amount_mismatch"
	GateNothingDue        Code = "gate.nothing_due"
	GateStayUnknown       Code = "gate.stay_unknown"
	GateExitNotAuthorized Code = "gate.exit_not_authorized"
	GateDuplicatePass     Code = "gate.duplicate_pass"

	SimulatorUnauthorized        Code = "simulator.unauthorized"
	SimulatorAlreadyRunning      Code = "simulator.already_running"
	SimulatorNotRunning          Code = "simulator.not_running"
	SimulatorScenarioUnavailable Code = "simulator.scenario_unavailable"
	SimulatorCarNotFound         Code = "simulator.car_not_found"

	// M3: reservations and payments. ReservationTooEarly and
	// ReservationWindowClosed are only ever a device `reason`, never an HTTP error.
	ReservationNotFound            Code = "reservation.not_found"
	ReservationCapacityReached     Code = "reservation.capacity_reached"
	ReservationActiveExists        Code = "reservation.active_exists"
	ReservationBlockedNoShows      Code = "reservation.blocked_no_shows"
	ReservationLotClosed           Code = "reservation.lot_closed"
	ReservationNoSpaceNow          Code = "reservation.no_space_now"
	ReservationArrivalInPast       Code = "reservation.arrival_in_past"
	ReservationBeyondHorizon       Code = "reservation.beyond_horizon"
	ReservationInvalidState        Code = "reservation.invalid_state"
	ReservationPaymentExpired      Code = "reservation.payment_expired"
	ReservationVehicleChangeClosed Code = "reservation.vehicle_change_closed"
	ReservationTooEarly            Code = "reservation.too_early"
	ReservationWindowClosed        Code = "reservation.window_closed"
	PaymentDeclined                Code = "payment.declined"
	PaymentInProgress              Code = "payment.in_progress"
	PaymentNotFound                Code = "payment.not_found"
	IdempotencyKeyReused           Code = "idempotency.key_reused"

	// Returned by the web BFF only.
	SessionLogoutUnconfirmed Code = "session.logout_unconfirmed"
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
	DeviceUnauthenticated,
	DeviceTimestampOutOfWindow,
	DeviceEventNotAllowed,
	DeviceEventConflict,
	LotNotFound,
	LotSpaceUnknown,
	GateLotFull,
	GateLotPhysicallyFull,
	GateLotClosed,
	GateCredentialInvalid,
	GateCredentialInUse,
	GatePaymentRequired,
	GateAmountMismatch,
	GateNothingDue,
	GateStayUnknown,
	GateExitNotAuthorized,
	GateDuplicatePass,
	SimulatorUnauthorized,
	SimulatorAlreadyRunning,
	SimulatorNotRunning,
	SimulatorScenarioUnavailable,
	SimulatorCarNotFound,
	SessionLogoutUnconfirmed,
	ReservationNotFound,
	ReservationCapacityReached,
	ReservationActiveExists,
	ReservationBlockedNoShows,
	ReservationLotClosed,
	ReservationNoSpaceNow,
	ReservationArrivalInPast,
	ReservationBeyondHorizon,
	ReservationInvalidState,
	ReservationPaymentExpired,
	ReservationVehicleChangeClosed,
	ReservationTooEarly,
	ReservationWindowClosed,
	PaymentDeclined,
	PaymentInProgress,
	PaymentNotFound,
	IdempotencyKeyReused,
}

// All returns every known code, sorted ascending. The returned slice is a
// fresh copy and may be modified by the caller.
func All() []Code {
	out := slices.Clone(catalog)
	slices.Sort(out)
	return out
}
