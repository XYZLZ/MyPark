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
