package auth_test

import (
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/XYZLZ/MyPark/apps/server/internal/features/audit"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/ratelimit"
)

const (
	pathForgot = "/v1/auth/password/forgot"
	pathReset  = "/v1/auth/password/reset"
	newPass    = "Brand-new-pass-42"
)

func resetBody(email, code, pw string) map[string]any {
	return map[string]any{"email": email, "code": code, "new_password": pw}
}

// resetCode returns the code in the newest password_reset message to email.
func (e *env) resetCode(email string) string {
	e.t.Helper()
	return e.text(`select payload->>'code' from outbox_messages
		where to_address = $1 and template = 'password_reset' order by id desc limit 1`, email)
}

// requestReset runs forgot for an active user and returns the emailed code.
func (e *env) requestReset(email string) string {
	e.t.Helper()
	if rec := e.post(pathForgot, map[string]any{"email": email}); rec.Code != http.StatusAccepted {
		e.t.Fatalf("forgot: %d %s", rec.Code, rec.Body)
	}
	return e.resetCode(email)
}

func TestForgot_ActiveUserGetsResetCode(t *testing.T) {
	e := newEnv(t)
	u := e.activeDriver("ana@example.com")
	rec := e.post(pathForgot, map[string]any{"email": " Ana@Example.COM "})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
	if data, _ := rec.json()["data"].(map[string]any); !reflect.DeepEqual(data, map[string]any{"status": "accepted"}) {
		t.Errorf("data = %v", data)
	}
	code := e.resetCode("ana@example.com")
	if len(code) != 6 {
		t.Fatalf("outbox code %q is not 6 digits", code)
	}
	var hash []byte
	var expires time.Time
	err := e.db.Super.QueryRow(t.Context(), `select code_hash, expires_at from auth_codes
		where user_id=$1 and purpose='reset_password' and consumed_at is null`, u.ID).Scan(&hash, &expires)
	if err != nil {
		t.Fatal(err)
	}
	if string(hash) != string(e.codes.Hash("reset_password", u.PublicID, code)) {
		t.Error("stored code_hash does not match the emailed code")
	}
	if want := e.clock.Now().Add(15 * time.Minute); !expires.Equal(want) {
		t.Errorf("expires_at = %v, want %v", expires, want)
	}
	if got := e.text(`select actor_kind||'/'||entity_type||'/'||entity_id from audit_log where action=$1`, audit.AuthPasswordResetRequested); got != "anonymous/user/"+u.PublicID.String() {
		t.Errorf("audit = %s", got)
	}
	if keys := e.limiter.keysByRule[ratelimit.CodeRequestEmail.Name]; len(keys) != 1 || keys[0] != "ana@example.com" {
		t.Errorf("limiter keys = %v, want the normalized email", keys)
	}
}

func TestForgot_UnknownPendingSuspendedDoNothing(t *testing.T) {
	e := newEnv(t)
	e.insertUser("pending@example.com", "driver", "pending_verification", true)
	e.insertUser("invitee@example.com", "admin", "pending_verification", false)
	e.insertUser("sus@example.com", "driver", "suspended", true)
	want := e.post(pathForgot, map[string]any{"email": "nobody@example.com"})
	if want.Code != http.StatusAccepted {
		t.Fatalf("status = %d", want.Code)
	}
	for _, email := range []string{"pending@example.com", "invitee@example.com", "sus@example.com"} {
		got := e.post(pathForgot, map[string]any{"email": email})
		if got.Code != want.Code || !reflect.DeepEqual(got.normalized(), want.normalized()) {
			t.Errorf("%s: %d %v differs from unknown %d %v", email, got.Code, got.normalized(), want.Code, want.normalized())
		}
	}
	for _, table := range []string{"auth_codes", "outbox_messages", "audit_log"} {
		if n := e.count("select count(*) from " + table); n != 0 {
			t.Errorf("%s has %d rows, want 0", table, n)
		}
	}
}

func TestReset_ChangesPasswordAndRevokesAllSessions(t *testing.T) {
	e := newEnv(t)
	u := e.activeDriver("ana@example.com")
	a := e.loginAccess("ana@example.com")
	b := e.loginAccess("ana@example.com")
	code := e.requestReset("ana@example.com")
	e.clock.Advance(time.Minute)
	rec := e.post(pathReset, resetBody(" Ana@Example.com", code, newPass))
	if rec.Code != http.StatusNoContent || rec.Body.Len() != 0 {
		t.Fatalf("status = %d body %q, want 204 empty", rec.Code, rec.Body)
	}
	if r := e.post(pathLogin, loginBody("ana@example.com", loginPassword)); r.Code != http.StatusUnauthorized {
		t.Errorf("old password login = %d, want 401", r.Code)
	}
	if r := e.post(pathLogin, loginBody("ana@example.com", newPass)); r.Code != http.StatusOK {
		t.Errorf("new password login = %d, want 200", r.Code)
	}
	if got := e.text(`select (password_changed_at = $2)::text from users where id=$1`, u.ID, e.clock.Now()); got != "true" {
		t.Errorf("password_changed_at == now: %s", got)
	}
	if got := e.count(`select count(*) from auth_sessions where user_id=$1 and revoked_reason='password_reset'`, u.ID); got != 2 {
		t.Errorf("password_reset sessions = %d, want 2", got)
	}
	for _, tok := range []string{a, b} {
		if r := e.do(http.MethodGet, pathProbe, tok); r.Code != http.StatusUnauthorized || r.errCode() != "auth.session_revoked" {
			t.Errorf("old access token: %d %s, want 401 auth.session_revoked", r.Code, r.errCode())
		}
	}
	if r := e.post(pathReset, resetBody("ana@example.com", code, newPass)); r.Code != http.StatusUnprocessableEntity || r.errCode() != "auth.code_invalid" {
		t.Errorf("replayed code: %d %s", r.Code, r.errCode())
	}
}

func TestReset_ConsumesOtherLiveCodes(t *testing.T) {
	e := newEnv(t)
	u := e.activeDriver("ana@example.com")
	e.insertCode(u, "verify_email", "111111")
	code := e.requestReset("ana@example.com")
	if rec := e.post(pathReset, resetBody("ana@example.com", code, newPass)); rec.Code != http.StatusNoContent {
		t.Fatalf("reset = %d %s", rec.Code, rec.Body)
	}
	if n := e.count(`select count(*) from auth_codes where user_id=$1 and consumed_at is null`, u.ID); n != 0 {
		t.Errorf("live codes = %d, want 0", n)
	}
}

func TestReset_EnqueuesPasswordChanged(t *testing.T) {
	e := newEnv(t)
	u := e.activeDriver("ana@example.com")
	e.loginAccess("ana@example.com")
	e.loginAccess("ana@example.com")
	code := e.requestReset("ana@example.com")
	if rec := e.post(pathReset, resetBody("ana@example.com", code, newPass)); rec.Code != http.StatusNoContent {
		t.Fatalf("reset = %d %s", rec.Code, rec.Body)
	}
	if n := e.count(`select count(*) from outbox_messages where template='password_changed' and to_address='ana@example.com' and user_id=$1`, u.ID); n != 1 {
		t.Errorf("password_changed messages = %d, want 1", n)
	}
	want := fmt.Sprintf(`user/%s/{"sessions_revoked": 2}`, u.PublicID)
	if got := e.text(`select actor_kind||'/'||entity_id||'/'||after::text from audit_log where action=$1`, audit.AuthPasswordReset); got != want {
		t.Errorf("audit = %s, want %s", got, want)
	}
}

func TestReset_WeakPassword422(t *testing.T) {
	e := newEnv(t)
	u := e.activeDriver("ana@example.com")
	code := e.requestReset("ana@example.com")
	rec := e.post(pathReset, resetBody("ana@example.com", code, "1234567890"))
	if rec.Code != http.StatusUnprocessableEntity || rec.errCode() != "validation.failed" {
		t.Fatalf("status = %d code = %s", rec.Code, rec.errCode())
	}
	if !strings.Contains(rec.Body.String(), "password_common") {
		t.Errorf("body lacks rule password_common: %s", rec.Body)
	}
	if got := e.count(`select attempts from auth_codes where user_id=$1 and purpose='reset_password'`, u.ID); got != 0 {
		t.Errorf("attempts = %d, want 0", got)
	}
}

func TestReset_WrongCodePersistsAttempt(t *testing.T) {
	e := newEnv(t)
	u := e.activeDriver("ana@example.com")
	code := e.requestReset("ana@example.com")
	rec := e.post(pathReset, resetBody("ana@example.com", wrongCode(code), newPass))
	if rec.Code != http.StatusUnprocessableEntity || rec.errCode() != "auth.code_invalid" {
		t.Fatalf("status = %d code = %s", rec.Code, rec.errCode())
	}
	if got := e.count(`select attempts from auth_codes where user_id=$1 and purpose='reset_password'`, u.ID); got != 1 {
		t.Errorf("attempts = %d, want 1", got)
	}
	if r := e.post(pathLogin, loginBody("ana@example.com", loginPassword)); r.Code != http.StatusOK {
		t.Errorf("password changed by a wrong code: login = %d", r.Code)
	}
}

func TestReset_InviteeAndSuspendedBehaveLikeUnknown(t *testing.T) {
	e := newEnv(t)
	inv := e.insertUser("boss@example.com", "admin", "pending_verification", false)
	e.insertCode(inv, "admin_invite", "424242")
	sus := e.insertUser("sus@example.com", "driver", "suspended", true)
	e.insertCode(sus, "reset_password", "424242")
	want := e.post(pathReset, resetBody("nobody@example.com", "424242", newPass))
	snap := e.userSnapshot("boss@example.com")
	codes := e.text(`select string_agg(c::text, ',' order by id) from auth_codes c`)
	for _, email := range []string{"boss@example.com", "sus@example.com"} {
		got := e.post(pathReset, resetBody(email, "424242", newPass))
		if got.Code != http.StatusUnprocessableEntity || got.errCode() != "auth.code_invalid" ||
			!reflect.DeepEqual(got.normalized(), want.normalized()) {
			t.Errorf("%s: %d %s", email, got.Code, got.errCode())
		}
	}
	if e.userSnapshot("boss@example.com") != snap {
		t.Error("invitee row changed")
	}
	if e.text(`select string_agg(c::text, ',' order by id) from auth_codes c`) != codes {
		t.Error("codes touched")
	}
}

// The code is exactly six ASCII digits (OpenAPI ^[0-9]{6}$): go-playground's
// numeric would accept -12345 and 1.2345. Verify shares the rule.
func TestCodeField_RejectsNonDigitSixCharCodes(t *testing.T) {
	e := newEnv(t)
	u := e.activeDriver("ana@example.com")
	e.requestReset("ana@example.com")
	for _, bad := range []string{"-12345", "1.2345", "12ab56", "+12345", " 12345"} {
		rec := e.post(pathReset, resetBody("ana@example.com", bad, newPass))
		if rec.Code != http.StatusUnprocessableEntity || rec.errCode() != "validation.failed" {
			t.Errorf("reset code %q: %d %s, want 422 validation.failed", bad, rec.Code, rec.errCode())
		}
		vr := e.post(pathVerify, map[string]any{"email": "ana@example.com", "code": bad})
		if vr.Code != http.StatusUnprocessableEntity || vr.errCode() != "validation.failed" {
			t.Errorf("verify code %q: %d %s, want 422 validation.failed", bad, vr.Code, vr.errCode())
		}
	}
	if got := e.count(`select attempts from auth_codes where user_id=$1 and purpose='reset_password'`, u.ID); got != 0 {
		t.Errorf("attempts = %d, want 0", got)
	}
}
