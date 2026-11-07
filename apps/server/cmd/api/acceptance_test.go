package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"regexp"
	"slices"
	"testing"

	"github.com/XYZLZ/MyPark/apps/server/internal/config"
	"github.com/XYZLZ/MyPark/apps/server/internal/domain/errcodes"
	"github.com/XYZLZ/MyPark/apps/server/internal/domain/secret"
	auditpg "github.com/XYZLZ/MyPark/apps/server/internal/features/audit/postgres"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/auth"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/legal"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/notifications"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/database"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/email"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/ratelimit"
	"github.com/XYZLZ/MyPark/apps/server/internal/testsupport"
)

var sixDigits = regexp.MustCompile(`\b(\d{6})\b`)

// acceptance drives the API like a client and records every error code.
type acceptance struct {
	t    *testing.T
	e    *apiEnv
	seen map[string]bool
}

func (a *acceptance) do(method, path, token string, body any) (*httptest.ResponseRecorder, map[string]any) {
	a.t.Helper()
	rec := a.e.serve(jsonReq(method, path, body), token)
	b := decodeBody(a.t, rec)
	if c := errCode(b); c != "" {
		a.seen[c] = true
	}
	return rec, b
}

func (a *acceptance) mustStatus(rec *httptest.ResponseRecorder, want int, what string) {
	a.t.Helper()
	if rec.Code != want {
		a.t.Fatalf("%s = %d %s, want %d", what, rec.Code, rec.Body, want)
	}
}

func (a *acceptance) login(emailAddr, password, kind string) (access, refresh string) {
	a.t.Helper()
	rec, b := a.do("POST", "/v1/auth/login", "", map[string]any{
		"email": emailAddr, "password": password, "client_kind": kind})
	a.mustStatus(rec, http.StatusOK, "login "+kind)
	data, _ := b["data"].(map[string]any)
	access, _ = data["access_token"].(string)
	refresh, _ = data["refresh_token"].(string)
	if access == "" || refresh == "" {
		a.t.Fatalf("login %s: no tokens in %s", kind, rec.Body)
	}
	return access, refresh
}

func dataObject(b map[string]any) map[string]any {
	d, _ := b["data"].(map[string]any)
	return d
}

// Spec 15.6 and the M1 backend exit criteria.
func TestM1BackendAcceptance(t *testing.T) {
	smtpAddr, api := testsupport.Mailpit(t)
	e := newAPIEnv(t, func(d *deps) { d.limiter = ratelimit.NewPostgres(d.pool, d.clock) })
	a := &acceptance{t: t, e: e, seen: map[string]bool{}}

	// Seed the first admin, as cmd/seed does.
	err := database.NewTxRunner(e.db.App).InTx(t.Context(), func(ctx context.Context) error {
		created, err := auth.SeedFirstAdmin(ctx, auth.SeedDeps{
			Hasher: e.hasher, Audit: auditpg.NewRecorder(), Clock: e.clk,
		}, auth.SeedAdmin{
			Email: "admin@mypark.test", FirstName: "Root", LastName: "Admin",
			Password: secret.String(config.DevSeedAdminPassword),
		})
		if err == nil && !created {
			t.Error("the first admin was not created")
		}
		return err
	})
	if err != nil {
		t.Fatalf("seed admin: %v", err)
	}

	// Register; the verification email reaches Mailpit through the outbox.
	driverEmail := testsupport.UniqueRecipient(t, api)
	const password = "Zx9!kelp-orbit"
	rec, _ := a.do("POST", "/v1/auth/register", "", map[string]any{
		"email": driverEmail, "password": password, "first_name": "Ana", "last_name": "Perez Gomez",
		"accepted_terms_version": legal.InitialVersion, "accepted_privacy_version": legal.InitialVersion,
	})
	a.mustStatus(rec, http.StatusAccepted, "register")

	host, port := testsupport.SplitHostPort(t, smtpAddr)
	dispatcher := notifications.NewDispatcher(e.db.App, email.NewSMTP(email.SMTPConfig{Host: host, Port: port}),
		e.clk, "MyPark <no-reply@mypark.local>", e.d.log)
	if n, err := dispatcher.Tick(t.Context()); err != nil || n != 1 {
		t.Fatalf("dispatch = %d, %v; want 1 email", n, err)
	}
	msgs := testsupport.MailpitMessages(t, api, driverEmail)
	if len(msgs) != 1 {
		t.Fatalf("mailpit holds %d messages for the driver, want 1", len(msgs))
	}
	m := sixDigits.FindStringSubmatch(msgs[0].Text)
	if m == nil {
		t.Fatalf("no 6-digit code in the email: %q", msgs[0].Text)
	}

	// Verify, then log in as the mobile app.
	rec, _ = a.do("POST", "/v1/auth/login", "", map[string]any{
		"email": driverEmail, "password": password, "client_kind": "mobile"})
	a.mustStatus(rec, http.StatusForbidden, "login before verification")
	rec, _ = a.do("POST", "/v1/auth/verify", "", map[string]any{"email": driverEmail, "code": m[1]})
	a.mustStatus(rec, http.StatusNoContent, "verify")
	mobileAccess, mobileRefresh := a.login(driverEmail, password, "mobile")

	// Vehicles: create, plate uniqueness (also across users), CRUD.
	vehicle := map[string]any{"plate": "ABC-123", "make": "Toyota", "model": "Corolla", "color": "Blue"}
	rec, b := a.do("POST", "/v1/vehicles", mobileAccess, vehicle)
	a.mustStatus(rec, http.StatusCreated, "create vehicle")
	vehicleID, _ := dataObject(b)["id"].(string)

	rec, b = a.do("POST", "/v1/vehicles", mobileAccess, vehicle)
	a.mustStatus(rec, http.StatusConflict, "duplicate plate, same user")
	if errCode(b) != string(errcodes.VehiclePlateTaken) {
		t.Errorf("duplicate plate code = %q", errCode(b))
	}
	other := e.mint(userSpec{role: "driver", status: "active", legalOK: true})
	rec, b = a.do("POST", "/v1/vehicles", other.token, map[string]any{
		"plate": "abc 123", "make": "Kia", "model": "Rio", "color": "Red"})
	a.mustStatus(rec, http.StatusConflict, "duplicate plate, other user")
	if errCode(b) != string(errcodes.VehiclePlateTaken) {
		t.Errorf("duplicate plate (other user) code = %q", errCode(b))
	}

	// The same account on the web sees the same vehicle.
	webAccess, _ := a.login(driverEmail, password, "web")
	rec, b = a.do("GET", "/v1/vehicles", webAccess, nil)
	a.mustStatus(rec, http.StatusOK, "list vehicles")
	list, _ := b["data"].([]any)
	if len(list) != 1 || dataObject(map[string]any{"data": list[0]})["plate"] != "ABC-123" {
		t.Fatalf("web list = %s, want the mobile vehicle", rec.Body)
	}
	rec, _ = a.do("PATCH", "/v1/vehicles/"+vehicleID, webAccess, map[string]any{"color": "Green"})
	a.mustStatus(rec, http.StatusOK, "patch vehicle")
	rec, b = a.do("GET", "/v1/vehicles/"+vehicleID, mobileAccess, nil)
	a.mustStatus(rec, http.StatusOK, "get vehicle")
	if dataObject(b)["color"] != "Green" {
		t.Errorf("patched color = %v", dataObject(b)["color"])
	}

	// Refresh rotates; presenting the old token again revokes the family.
	rec, b = a.do("POST", "/v1/auth/refresh", "", map[string]any{"refresh_token": mobileRefresh})
	a.mustStatus(rec, http.StatusOK, "refresh")
	newRefresh, _ := dataObject(b)["refresh_token"].(string)
	newAccess, _ := dataObject(b)["access_token"].(string)
	if newRefresh == "" || newRefresh == mobileRefresh {
		t.Fatalf("refresh did not rotate the token")
	}
	rec, b = a.do("POST", "/v1/auth/refresh", "", map[string]any{"refresh_token": mobileRefresh})
	a.mustStatus(rec, http.StatusUnauthorized, "refresh reuse")
	if errCode(b) != string(errcodes.AuthSessionRevoked) {
		t.Errorf("reuse code = %q, want auth.session_revoked", errCode(b))
	}
	rec, b = a.do("GET", "/v1/vehicles", newAccess, nil)
	a.mustStatus(rec, http.StatusUnauthorized, "access token after reuse")
	if errCode(b) != string(errcodes.AuthSessionRevoked) {
		t.Errorf("revoked session code = %q", errCode(b))
	}
	rec, _ = a.do("GET", "/v1/vehicles", webAccess, nil)
	a.mustStatus(rec, http.StatusOK, "the web session is a different family and survives")

	// Delete the vehicle: gone for the owner.
	rec, _ = a.do("DELETE", "/v1/vehicles/"+vehicleID, webAccess, nil)
	a.mustStatus(rec, http.StatusNoContent, "delete vehicle")
	rec, b = a.do("GET", "/v1/vehicles/"+vehicleID, webAccess, nil)
	a.mustStatus(rec, http.StatusNotFound, "get deleted vehicle")
	if errCode(b) != string(errcodes.VehicleNotFound) {
		t.Errorf("deleted vehicle code = %q", errCode(b))
	}

	// Admin area: the seeded admin gets in, a driver does not.
	adminAccess, _ := a.login("admin@mypark.test", config.DevSeedAdminPassword, "web")
	rec, b = a.do("GET", "/v1/admin/admins", adminAccess, nil)
	a.mustStatus(rec, http.StatusOK, "admin list")
	if admins, _ := b["data"].([]any); len(admins) != 1 {
		t.Errorf("admin list has %d entries, want the seeded admin", len(admins))
	}
	rec, b = a.do("GET", "/v1/admin/admins", webAccess, nil)
	a.mustStatus(rec, http.StatusForbidden, "driver on the admin area")
	if errCode(b) != string(errcodes.AuthForbidden) {
		t.Errorf("driver admin code = %q, want auth.forbidden", errCode(b))
	}
	rec, _ = a.do("GET", "/v1/vehicles", adminAccess, nil)
	a.mustStatus(rec, http.StatusForbidden, "admin on a driver route")

	known := map[string]bool{}
	for _, c := range errcodes.All() {
		known[string(c)] = true
	}
	for code := range a.seen {
		if !known[code] {
			t.Errorf("error code %q is not in errcodes.All()", code)
		}
	}
	if !slices.Contains(mapKeys(a.seen), string(errcodes.AuthSessionRevoked)) {
		t.Errorf("the run never produced auth.session_revoked: %v", a.seen)
	}
}

func mapKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
