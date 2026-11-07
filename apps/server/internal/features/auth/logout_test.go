package auth_test

import (
	"net/http"
	"testing"

	"github.com/XYZLZ/MyPark/apps/server/internal/features/audit"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/auth"
)

const (
	pathLogout    = "/v1/auth/logout"
	pathLogoutAll = "/v1/auth/logout-all"
)

// reason returns the revoked_reason of a session, "<null>" while it is live.
func (e *env) reason(sid any) string {
	e.t.Helper()
	return e.text(`select revoked_reason from auth_sessions where public_id = $1`, sid)
}

func TestLogout_RevokesOnlyCurrentSession(t *testing.T) {
	e := newEnv(t)
	u := e.activeDriver("ana@example.com")
	a := e.loginAccess("ana@example.com")
	b := e.loginAccess("ana@example.com")
	sidA, sidB := e.claims(a).SessionID, e.claims(b).SessionID

	rec := e.do(http.MethodPost, pathLogout, a)
	if rec.Code != http.StatusNoContent || rec.Body.Len() != 0 {
		t.Fatalf("logout: status %d body %q, want 204 and no body", rec.Code, rec.Body)
	}
	if got := e.reason(sidA); got != "logout" {
		t.Errorf("session A reason = %q, want logout", got)
	}
	if got := e.reason(sidB); got != "<null>" {
		t.Errorf("session B reason = %q, want it live", got)
	}
	if rec := e.do(http.MethodGet, pathProbe, b); rec.Code != http.StatusOK {
		t.Errorf("session B on the probe route: status %d, want 200", rec.Code)
	}
	rec = e.do(http.MethodGet, pathProbe, a)
	if rec.Code != http.StatusUnauthorized || rec.errCode() != "auth.session_revoked" {
		t.Errorf("session A right after logout: %d %s, want 401 auth.session_revoked", rec.Code, rec.errCode())
	}

	if n := e.count(`select count(*) from audit_log where action = $1 and entity_type = 'session'
		and entity_id = $2 and actor_kind = 'user' and actor_user_id = $3 and after = '{"sessions_revoked":1}'`,
		audit.AuthLogout, sidA.String(), u.ID); n != 1 {
		t.Errorf("auth.logout audit rows = %d, want 1", n)
	}
}

func TestLogout_SecondCallWithRevokedTokenIs401(t *testing.T) {
	e := newEnv(t)
	e.activeDriver("ana@example.com")
	a := e.loginAccess("ana@example.com")
	if rec := e.do(http.MethodPost, pathLogout, a); rec.Code != http.StatusNoContent {
		t.Fatalf("first logout: %d", rec.Code)
	}
	revokedAt := e.text(`select revoked_at::text from auth_sessions where public_id = $1`, e.claims(a).SessionID)

	rec := e.do(http.MethodPost, pathLogout, a)
	if rec.Code != http.StatusUnauthorized || rec.errCode() != "auth.session_revoked" {
		t.Errorf("second logout: %d %s, want 401 auth.session_revoked", rec.Code, rec.errCode())
	}
	if got := e.text(`select revoked_at::text from auth_sessions where public_id = $1`, e.claims(a).SessionID); got != revokedAt {
		t.Errorf("revoked_at changed from %s to %s", revokedAt, got)
	}
	if n := e.count(`select count(*) from audit_log where action = $1`, audit.AuthLogout); n != 1 {
		t.Errorf("auth.logout audit rows = %d, want 1", n)
	}
}

func TestLogout_RequiresAuthentication(t *testing.T) {
	e := newEnv(t)
	for _, path := range []string{pathLogout, pathLogoutAll} {
		rec := e.do(http.MethodPost, path, "")
		if rec.Code != http.StatusUnauthorized || rec.errCode() != "auth.unauthenticated" {
			t.Errorf("%s without a token: %d %s", path, rec.Code, rec.errCode())
		}
	}
}

func TestLogout_WorksWhileLegalAcceptancePending(t *testing.T) {
	e := newEnv(t)
	u := e.activeDriver("ana@example.com")
	e.exec(`update users set terms_version = '2025-01-01' where id = $1`, u.ID)
	tok := e.loginAccess("ana@example.com")
	if rec := e.do(http.MethodGet, pathProbe, tok); rec.Code != http.StatusForbidden || rec.errCode() != "legal.acceptance_required" {
		t.Fatalf("gated probe: %d %s, want 403 legal.acceptance_required", rec.Code, rec.errCode())
	}
	if rec := e.do(http.MethodPost, pathLogout, tok); rec.Code != http.StatusNoContent {
		t.Errorf("logout: %d, want 204 (LegalExempt)", rec.Code)
	}
}

func TestLogout_AdminCan(t *testing.T) {
	e := newEnv(t)
	e.insertUser("boss@example.com", "admin", "active", true)
	tok := e.loginAccess("boss@example.com")
	if rec := e.do(http.MethodPost, pathLogout, tok); rec.Code != http.StatusNoContent {
		t.Errorf("admin logout: %d, want 204", rec.Code)
	}
}

func TestLogoutAll_RevokesEverySession(t *testing.T) {
	e := newEnv(t)
	u := e.activeDriver("ana@example.com")
	other := e.activeDriver("bob@example.com")
	tokens := []string{e.loginAccess("ana@example.com"), e.loginAccess("ana@example.com"), e.loginAccess("ana@example.com")}
	bob := e.loginAccess("bob@example.com")

	rec := e.do(http.MethodPost, pathLogoutAll, tokens[0])
	if rec.Code != http.StatusNoContent || rec.Body.Len() != 0 {
		t.Fatalf("logout-all: status %d body %q, want 204 and no body", rec.Code, rec.Body)
	}
	for i, tok := range tokens {
		if got := e.reason(e.claims(tok).SessionID); got != "logout_all" {
			t.Errorf("session %d reason = %q, want logout_all", i, got)
		}
		rec := e.do(http.MethodGet, pathProbe, tok)
		if rec.Code != http.StatusUnauthorized || rec.errCode() != "auth.session_revoked" {
			t.Errorf("session %d after logout-all: %d %s", i, rec.Code, rec.errCode())
		}
	}
	if rec := e.do(http.MethodGet, pathProbe, bob); rec.Code != http.StatusOK {
		t.Errorf("another user's session: %d, want 200", rec.Code)
	}
	if got := e.count(`select count(*) from auth_sessions where user_id = $1 and revoked_at is null`, other.ID); got != 1 {
		t.Errorf("another user's live sessions = %d, want 1", got)
	}
	if n := e.count(`select count(*) from audit_log where action = $1 and entity_type = 'user'
		and entity_id = $2 and actor_user_id = $3 and after = '{"sessions_revoked":3}'`,
		audit.AuthLogoutAll, u.PublicID.String(), u.ID); n != 1 {
		t.Errorf("auth.logout_all audit rows = %d, want 1", n)
	}
}

func TestRevokeAllSessions_SkipsAlreadyRevoked(t *testing.T) {
	e := newEnv(t)
	u := e.activeDriver("ana@example.com")
	a := e.loginAccess("ana@example.com")
	e.loginAccess("ana@example.com")
	e.loginAccess("ana@example.com")
	if rec := e.do(http.MethodPost, pathLogout, a); rec.Code != http.StatusNoContent {
		t.Fatalf("logout: %d", rec.Code)
	}

	n, err := e.svc.RevokeAllSessions(t.Context(), u.ID, auth.ReasonPasswordReset)
	if err != nil || n != 2 {
		t.Fatalf("RevokeAllSessions = %d, %v, want 2", n, err)
	}
	if got := e.reason(e.claims(a).SessionID); got != "logout" {
		t.Errorf("already revoked session reason = %q, want it kept as logout", got)
	}
	if got := e.count(`select count(*) from auth_sessions where user_id = $1 and revoked_reason = 'password_reset'`, u.ID); got != 2 {
		t.Errorf("password_reset sessions = %d, want 2", got)
	}
}
