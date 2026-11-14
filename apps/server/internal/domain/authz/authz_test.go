package authz_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/XYZLZ/MyPark/apps/server/internal/domain/authz"
)

func TestPrincipal_ContextRoundTrip(t *testing.T) {
	if _, ok := authz.PrincipalFrom(context.Background()); ok {
		t.Fatal("empty context has a principal")
	}
	p := authz.Principal{UserID: 7, UserPublicID: uuid.New(), Role: authz.RoleAdmin, SessionID: 9, SessionPublicID: uuid.New(), LegalPending: true}
	got, ok := authz.PrincipalFrom(authz.WithPrincipal(context.Background(), p))
	if !ok || got != p {
		t.Fatalf("round trip = %+v, %v", got, ok)
	}
	if !p.Is(authz.RoleAdmin) || !p.Is(authz.RoleDriver, authz.RoleAdmin) || p.Is(authz.RoleDriver) || p.Is() {
		t.Error("Is mismatch")
	}
}

func TestParseRole(t *testing.T) {
	if r, ok := authz.ParseRole("driver"); !ok || r != authz.RoleDriver {
		t.Error("driver")
	}
	if r, ok := authz.ParseRole("admin"); !ok || r != authz.RoleAdmin {
		t.Error("admin")
	}
	if _, ok := authz.ParseRole("root"); ok {
		t.Error("root accepted")
	}
}

func TestDeviceKinds(t *testing.T) {
	want := map[authz.DeviceKind]string{
		authz.DevicePodium: "podium", authz.DeviceDispenser: "dispenser",
		authz.DeviceEntryBarrier: "entry_barrier", authz.DeviceExitBarrier: "exit_barrier",
		authz.DevicePayStation: "pay_station", authz.DeviceSensorGateway: "sensor_gateway",
	}
	for k, s := range want {
		if string(k) != s {
			t.Errorf("%q != %q", k, s)
		}
	}
}

func TestDeviceRoundTrip(t *testing.T) {
	if _, ok := authz.DeviceFrom(t.Context()); ok {
		t.Fatal("empty context carries a device")
	}
	d := authz.Device{ID: 7, PublicID: uuid.New(), LotID: 3, Kind: authz.DevicePayStation}
	got, ok := authz.DeviceFrom(authz.WithDevice(t.Context(), d))
	if !ok || got != d {
		t.Errorf("DeviceFrom = %+v, %v; want %+v", got, ok, d)
	}
	if _, ok := authz.PrincipalFrom(authz.WithDevice(t.Context(), d)); ok {
		t.Error("a device is not a user principal")
	}
}
