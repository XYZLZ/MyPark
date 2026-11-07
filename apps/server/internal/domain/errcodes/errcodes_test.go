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
		"health.not_ready",
		"http.invalid_json",
		"http.method_not_allowed",
		"http.not_found",
		"http.payload_too_large",
		"http.unsupported_media_type",
		"internal.unexpected",
		"legal.acceptance_required",
		"legal.version_outdated",
		"rate_limit.exceeded",
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
