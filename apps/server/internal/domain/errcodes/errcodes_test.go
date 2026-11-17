package errcodes_test

import (
	"regexp"
	"slices"
	"testing"

	"github.com/XYZLZ/MyPark/apps/server/internal/domain/errcodes"
)

func TestAll_SortedAndComplete(t *testing.T) {
	want := []errcodes.Code{
		"auth.account_suspended",
		"auth.code_invalid",
		"auth.email_not_verified",
		"auth.forbidden",
		"auth.invalid_credentials",
		"auth.password_incorrect",
		"auth.refresh_invalid",
		"auth.session_revoked",
		"auth.unauthenticated",
		"device.event_conflict",
		"device.event_not_allowed",
		"device.timestamp_out_of_window",
		"device.unauthenticated",
		"gate.amount_mismatch",
		"gate.credential_in_use",
		"gate.credential_invalid",
		"gate.duplicate_pass",
		"gate.exit_not_authorized",
		"gate.lot_closed",
		"gate.lot_full",
		"gate.lot_physically_full",
		"gate.nothing_due",
		"gate.payment_required",
		"gate.stay_unknown",
		"health.not_ready",
		"http.invalid_json",
		"http.method_not_allowed",
		"http.not_found",
		"http.payload_too_large",
		"http.unsupported_media_type",
		"idempotency.key_reused",
		"internal.unexpected",
		"legal.acceptance_required",
		"legal.version_outdated",
		"lot.not_found",
		"lot.space_unknown",
		"payment.declined",
		"payment.in_progress",
		"payment.not_found",
		"rate_limit.exceeded",
		"reservation.active_exists",
		"reservation.arrival_in_past",
		"reservation.beyond_horizon",
		"reservation.blocked_no_shows",
		"reservation.capacity_reached",
		"reservation.invalid_state",
		"reservation.lot_closed",
		"reservation.no_space_now",
		"reservation.not_found",
		"reservation.payment_expired",
		"reservation.too_early",
		"reservation.vehicle_change_closed",
		"reservation.window_closed",
		"session.logout_unconfirmed",
		"simulator.already_running",
		"simulator.car_not_found",
		"simulator.not_running",
		"simulator.scenario_unavailable",
		"simulator.unauthorized",
		"user.email_taken",
		"validation.failed",
		"vehicle.in_use",
		"vehicle.limit_reached",
		"vehicle.not_found",
		"vehicle.plate_taken",
	}
	all := errcodes.All()
	if len(all) != len(want) {
		t.Fatalf("All() = %v, want %v", all, want)
	}
	for i := range want {
		if all[i] != want[i] {
			t.Errorf("All()[%d] = %q, want %q", i, all[i], want[i])
		}
	}
}

func TestAll_ReturnsCopy(t *testing.T) {
	a := errcodes.All()
	a[0] = "mutated"
	if errcodes.All()[0] == "mutated" {
		t.Fatal("All() exposes internal slice")
	}
}

var m1Codes = []struct {
	c    errcodes.Code
	want string
}{
	{errcodes.AuthUnauthenticated, "auth.unauthenticated"},
	{errcodes.AuthSessionRevoked, "auth.session_revoked"},
	{errcodes.AuthRefreshInvalid, "auth.refresh_invalid"},
	{errcodes.AuthInvalidCredentials, "auth.invalid_credentials"},
	{errcodes.AuthEmailNotVerified, "auth.email_not_verified"},
	{errcodes.AuthAccountSuspended, "auth.account_suspended"},
	{errcodes.AuthForbidden, "auth.forbidden"},
	{errcodes.AuthPasswordIncorrect, "auth.password_incorrect"},
	{errcodes.AuthCodeInvalid, "auth.code_invalid"},
	{errcodes.LegalAcceptanceRequired, "legal.acceptance_required"},
	{errcodes.LegalVersionOutdated, "legal.version_outdated"},
	{errcodes.UserEmailTaken, "user.email_taken"},
	{errcodes.VehicleNotFound, "vehicle.not_found"},
	{errcodes.VehiclePlateTaken, "vehicle.plate_taken"},
	{errcodes.VehicleLimitReached, "vehicle.limit_reached"},
	{errcodes.VehicleInUse, "vehicle.in_use"},
	{errcodes.RateLimitExceeded, "rate_limit.exceeded"},
	{errcodes.HTTPInvalidJSON, "http.invalid_json"},
	{errcodes.HTTPPayloadTooLarge, "http.payload_too_large"},
	{errcodes.HTTPUnsupportedMediaType, "http.unsupported_media_type"},
}

func TestCatalog_M1Codes(t *testing.T) {
	if len(m1Codes) != 20 {
		t.Fatalf("expected 20 M1 codes, table has %d", len(m1Codes))
	}
	all := errcodes.All()
	for _, tc := range m1Codes {
		if string(tc.c) != tc.want {
			t.Errorf("constant = %q, want %q", tc.c, tc.want)
		}
		if !slices.Contains(all, tc.c) {
			t.Errorf("%q missing from All()", tc.c)
		}
	}
}

var m2Codes = []struct {
	c    errcodes.Code
	want string
}{
	{errcodes.DeviceUnauthenticated, "device.unauthenticated"},
	{errcodes.DeviceTimestampOutOfWindow, "device.timestamp_out_of_window"},
	{errcodes.DeviceEventNotAllowed, "device.event_not_allowed"},
	{errcodes.DeviceEventConflict, "device.event_conflict"},
	{errcodes.LotNotFound, "lot.not_found"},
	{errcodes.LotSpaceUnknown, "lot.space_unknown"},
	{errcodes.GateLotFull, "gate.lot_full"},
	{errcodes.GateLotPhysicallyFull, "gate.lot_physically_full"},
	{errcodes.GateLotClosed, "gate.lot_closed"},
	{errcodes.GateCredentialInvalid, "gate.credential_invalid"},
	{errcodes.GateCredentialInUse, "gate.credential_in_use"},
	{errcodes.GatePaymentRequired, "gate.payment_required"},
	{errcodes.GateAmountMismatch, "gate.amount_mismatch"},
	{errcodes.GateNothingDue, "gate.nothing_due"},
	{errcodes.GateStayUnknown, "gate.stay_unknown"},
	{errcodes.GateExitNotAuthorized, "gate.exit_not_authorized"},
	{errcodes.GateDuplicatePass, "gate.duplicate_pass"},
	{errcodes.SimulatorUnauthorized, "simulator.unauthorized"},
	{errcodes.SimulatorAlreadyRunning, "simulator.already_running"},
	{errcodes.SimulatorNotRunning, "simulator.not_running"},
	{errcodes.SimulatorScenarioUnavailable, "simulator.scenario_unavailable"},
	{errcodes.SimulatorCarNotFound, "simulator.car_not_found"},
	{errcodes.SessionLogoutUnconfirmed, "session.logout_unconfirmed"},
}

func TestCatalog_M2Codes(t *testing.T) {
	if len(m2Codes) != 23 {
		t.Fatalf("expected 23 M2 codes, table has %d", len(m2Codes))
	}
	all := errcodes.All()
	for _, tc := range m2Codes {
		if string(tc.c) != tc.want {
			t.Errorf("constant = %q, want %q", tc.c, tc.want)
		}
		if !slices.Contains(all, tc.c) {
			t.Errorf("%q missing from All()", tc.c)
		}
	}
}

var m3Codes = []struct {
	c    errcodes.Code
	want string
}{
	{errcodes.ReservationNotFound, "reservation.not_found"},
	{errcodes.ReservationCapacityReached, "reservation.capacity_reached"},
	{errcodes.ReservationActiveExists, "reservation.active_exists"},
	{errcodes.ReservationBlockedNoShows, "reservation.blocked_no_shows"},
	{errcodes.ReservationLotClosed, "reservation.lot_closed"},
	{errcodes.ReservationNoSpaceNow, "reservation.no_space_now"},
	{errcodes.ReservationArrivalInPast, "reservation.arrival_in_past"},
	{errcodes.ReservationBeyondHorizon, "reservation.beyond_horizon"},
	{errcodes.ReservationInvalidState, "reservation.invalid_state"},
	{errcodes.ReservationPaymentExpired, "reservation.payment_expired"},
	{errcodes.ReservationVehicleChangeClosed, "reservation.vehicle_change_closed"},
	{errcodes.ReservationTooEarly, "reservation.too_early"},
	{errcodes.ReservationWindowClosed, "reservation.window_closed"},
	{errcodes.PaymentDeclined, "payment.declined"},
	{errcodes.PaymentInProgress, "payment.in_progress"},
	{errcodes.PaymentNotFound, "payment.not_found"},
	{errcodes.IdempotencyKeyReused, "idempotency.key_reused"},
}

func TestCatalog_M3Codes(t *testing.T) {
	if len(m3Codes) != 17 {
		t.Fatalf("expected 17 M3 codes, table has %d", len(m3Codes))
	}
	all := errcodes.All()
	for _, tc := range m3Codes {
		if string(tc.c) != tc.want {
			t.Errorf("constant = %q, want %q", tc.c, tc.want)
		}
		if !slices.Contains(all, tc.c) {
			t.Errorf("%q missing from All()", tc.c)
		}
	}
}

func TestCatalog_FormatAndUnique(t *testing.T) {
	format := regexp.MustCompile(`^[a-z][a-z0-9_]*[.][a-z][a-z0-9_]*$`)
	seen := map[errcodes.Code]bool{}
	for _, c := range errcodes.All() {
		if !format.MatchString(string(c)) {
			t.Errorf("%q does not match code format", c)
		}
		if seen[c] {
			t.Errorf("duplicate code %q", c)
		}
		seen[c] = true
	}
}
