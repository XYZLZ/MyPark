package auth_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/XYZLZ/MyPark/apps/server/internal/features/audit"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/auth"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/legal"
)

const pathRefresh = "/v1/auth/refresh"

// pair is a login or refresh result.
type pair struct{ access, refresh string }

func pairOf(t *testing.T, rec resp) pair {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", rec.Code, rec.Body)
	}
	data, _ := rec.json()["data"].(map[string]any)
	a, _ := data["access_token"].(string)
	r, _ := data["refresh_token"].(string)
	return pair{access: a, refresh: r}
}

func (e *env) loginPair(email string) pair {
	e.t.Helper()
	return pairOf(e.t, e.post(pathLogin, loginBody(email, loginPassword)))
}

func (e *env) refresh(token string) resp {
	e.t.Helper()
	return e.post(pathRefresh, map[string]any{"refresh_token": token})
}

func tokenHash(token string) []byte {
	raw, _ := base64.RawURLEncoding.DecodeString(token)
	sum := sha256.Sum256(raw)
	return sum[:]
}

func (e *env) wantUnauthorized(rec resp, code string) {
	e.t.Helper()
	if rec.Code != http.StatusUnauthorized || rec.errCode() != code {
		e.t.Errorf("got %d %s, want 401 %s (body %s)", rec.Code, rec.errCode(), code, rec.Body)
	}
}

func TestRefresh_RotatesAndSlides(t *testing.T) {
	e := newEnv(t)
	e.activeDriver("ana@example.com")
	first := e.loginPair("ana@example.com")
	sid := e.claims(first.access).SessionID

	e.clock.Advance(10 * 24 * time.Hour)
	now := e.clock.Now()
	rec := e.refresh(first.refresh)
	second := pairOf(t, rec)
	if second.refresh == "" || second.refresh == first.refresh || second.access == "" {
		t.Fatalf("refresh must return a new pair, got %+v", second)
	}
	data, _ := rec.json()["data"].(map[string]any)
	if got := data["refresh_expires_in"]; got != float64(30*24*3600) {
		t.Errorf("refresh_expires_in = %v, want 30 days", got)
	}
	if got := data["token_type"]; got != "Bearer" {
		t.Errorf("token_type = %v", got)
	}
	if u, _ := data["user"].(map[string]any); u == nil || u["email"] != "ana@example.com" {
		t.Errorf("user missing from the refresh response: %v", data["user"])
	}
	if n := e.count(`select count(*) from refresh_tokens where token_hash = $1 and rotated_at = $2`,
		tokenHash(first.refresh), now); n != 1 {
		t.Errorf("old token rotated_at = now rows = %d, want 1", n)
	}
	if n := e.count(`select count(*) from refresh_tokens where token_hash = $1 and rotated_at is null and expires_at = $2`,
		tokenHash(second.refresh), now.Add(30*24*time.Hour)); n != 1 {
		t.Errorf("successor with expires_at = now+30d rows = %d, want 1", n)
	}
	if n := e.count(`select count(*) from auth_sessions where public_id = $1 and last_used_at = $2 and revoked_at is null`,
		sid, now); n != 1 {
		t.Errorf("session last_used_at = now rows = %d, want 1", n)
	}
	// The new access token belongs to the same session and works.
	if got := e.claims(second.access).SessionID; got != sid {
		t.Errorf("new access token sid = %s, want %s", got, sid)
	}
	if rec := e.do(http.MethodGet, pathProbe, second.access); rec.Code != http.StatusOK {
		t.Errorf("probe with the new access token: %d", rec.Code)
	}
}

func TestRefresh_SuccessorCappedByAbsolute(t *testing.T) {
	e := newEnv(t)
	e.activeDriver("ana@example.com")
	p := e.loginPair("ana@example.com")
	sid := e.claims(p.access).SessionID
	absolute := e.clock.Now().Add(90 * 24 * time.Hour)

	for range 3 { // day 25, 50, 75
		e.clock.Advance(25 * 24 * time.Hour)
		p = pairOf(t, e.refresh(p.refresh))
	}
	if n := e.count(`select count(*) from refresh_tokens where token_hash = $1 and expires_at = $2`,
		tokenHash(p.refresh), absolute); n != 1 {
		t.Errorf("successor at day 75 must expire at the absolute cap, rows = %d", n)
	}
	if n := e.count(`select count(*) from auth_sessions where public_id = $1 and absolute_expires_at = $2`, sid, absolute); n != 1 {
		t.Errorf("absolute_expires_at must not slide")
	}
}

func TestRefresh_ReuseRevokesFamily(t *testing.T) {
	e := newEnv(t)
	u := e.activeDriver("ana@example.com")
	r1 := e.loginPair("ana@example.com")
	sid := e.claims(r1.access).SessionID
	r2 := pairOf(t, e.refresh(r1.refresh))

	e.wantUnauthorized(e.refresh(r1.refresh), "auth.session_revoked")
	if got := e.reason(sid); got != "reuse_detected" {
		t.Errorf("revoked_reason = %q, want reuse_detected", got)
	}
	if n := e.count(`select count(*) from audit_log where action = $1 and actor_kind = 'system'
		and actor_user_id is null and entity_type = 'session' and entity_id = $2
		and after = jsonb_build_object('user_id', $3::text)`,
		audit.AuthRefreshReuseDetected, sid.String(), u.PublicID.String()); n != 1 {
		t.Errorf("auth.refresh_reuse_detected audit rows = %d, want 1", n)
	}
	e.wantUnauthorized(e.refresh(r2.refresh), "auth.session_revoked")
	e.wantUnauthorized(e.do(http.MethodGet, pathProbe, r2.access), "auth.session_revoked")
	// Presenting the family again after it is revoked does not audit again.
	e.wantUnauthorized(e.refresh(r1.refresh), "auth.session_revoked")
	if n := e.count(`select count(*) from audit_log where action = $1`, audit.AuthRefreshReuseDetected); n != 1 {
		t.Errorf("reuse audit rows after replays = %d, want 1", n)
	}
}

func TestRefresh_ReuseRevocationSurvivesErrorResponse(t *testing.T) {
	e := newEnv(t)
	e.activeDriver("ana@example.com")
	r1 := e.loginPair("ana@example.com")
	sid := e.claims(r1.access).SessionID
	pairOf(t, e.refresh(r1.refresh))
	e.wantUnauthorized(e.refresh(r1.refresh), "auth.session_revoked")

	// Read through the app pool and the superuser pool: fresh connections,
	// as a different request would use.
	read := map[string]func(string, ...any) int64{
		"app": func(q string, a ...any) int64 {
			var n int64
			if err := e.db.App.QueryRow(t.Context(), q, a...).Scan(&n); err != nil {
				t.Fatal(err)
			}
			return n
		},
		"super": e.count,
	}
	for name, q := range read {
		if n := q(`select count(*) from auth_sessions where public_id = $1 and revoked_reason = 'reuse_detected'`, sid); n != 1 {
			t.Errorf("%s: revoked session rows = %d, want 1 (revocation rolled back?)", name, n)
		}
		if n := q(`select count(*) from audit_log where action = $1 and entity_id = $2`,
			audit.AuthRefreshReuseDetected, sid.String()); n != 1 {
			t.Errorf("%s: audit rows = %d, want 1 (audit rolled back?)", name, n)
		}
	}
}

func TestRefresh_ConcurrentSameToken(t *testing.T) {
	e := newEnv(t)
	e.activeDriver("ana@example.com")
	r1 := e.loginPair("ana@example.com")
	sid := e.claims(r1.access).SessionID

	var ok, rejected, other atomic.Int64
	var wg sync.WaitGroup
	done := make(chan struct{})
	for range 20 {
		wg.Go(func() {
			rec := e.refresh(r1.refresh)
			switch {
			case rec.Code == http.StatusOK:
				ok.Add(1)
			case rec.Code == http.StatusUnauthorized && rec.errCode() == "auth.session_revoked":
				rejected.Add(1)
			default:
				other.Add(1)
			}
		})
	}
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("concurrent refreshes did not finish in 10s (deadlock?)")
	}
	if ok.Load() != 1 || other.Load() != 0 || rejected.Load() != 19 {
		t.Errorf("ok=%d rejected=%d other=%d, want exactly one success and the rest session_revoked",
			ok.Load(), rejected.Load(), other.Load())
	}
	if got := e.reason(sid); got != "reuse_detected" {
		t.Errorf("revoked_reason = %q, want reuse_detected", got)
	}
	if n := e.count(`select count(*) from audit_log where action = $1 and entity_id = $2`,
		audit.AuthRefreshReuseDetected, sid.String()); n != 1 {
		t.Errorf("reuse audit rows = %d, want exactly 1", n)
	}
}

func TestRefresh_ExpiredIsInvalid(t *testing.T) {
	e := newEnv(t)
	e.activeDriver("ana@example.com")
	p := e.loginPair("ana@example.com")
	e.clock.Advance(30*24*time.Hour + time.Second)
	e.wantUnauthorized(e.refresh(p.refresh), "auth.refresh_invalid")
	if n := e.count(`select count(*) from refresh_tokens where rotated_at is not null`); n != 0 {
		t.Errorf("an expired token must not rotate, rotated = %d", n)
	}
}

func TestRefresh_AfterAbsoluteCapIsInvalid(t *testing.T) {
	e := newEnv(t)
	e.activeDriver("ana@example.com")
	p := e.loginPair("ana@example.com")
	// The token itself would still be live; only the session cap is over.
	e.exec(`update refresh_tokens set expires_at = expires_at + interval '365 days'`)
	e.clock.Advance(90*24*time.Hour + time.Second)
	e.wantUnauthorized(e.refresh(p.refresh), "auth.refresh_invalid")
}

func TestRefresh_DeletedUserRevokes(t *testing.T) {
	e := newEnv(t)
	u := e.activeDriver("ana@example.com")
	p := e.loginPair("ana@example.com")
	sid := e.claims(p.access).SessionID
	e.exec(`update users set status = 'deleted', deleted_at = now() where id = $1`, u.ID)

	e.wantUnauthorized(e.refresh(p.refresh), "auth.session_revoked")
	if got := e.reason(sid); got != "account_deleted" {
		t.Errorf("revoked_reason = %q, want account_deleted", got)
	}
	if n := e.count(`select count(*) from refresh_tokens where rotated_at is not null`); n != 0 {
		t.Errorf("no token may rotate for a deleted user")
	}
}

func TestRefresh_SuspendedUserIsRejectedWithoutWrite(t *testing.T) {
	e := newEnv(t)
	u := e.activeDriver("ana@example.com")
	p := e.loginPair("ana@example.com")
	sid := e.claims(p.access).SessionID
	e.exec(`update users set status = 'suspended' where id = $1`, u.ID)

	e.wantUnauthorized(e.refresh(p.refresh), "auth.session_revoked")
	if got := e.reason(sid); got != "<null>" {
		t.Errorf("revoked_reason = %q, want the session untouched", got)
	}
	if n := e.count(`select count(*) from refresh_tokens where rotated_at is not null`); n != 0 {
		t.Errorf("no token may rotate for a suspended user")
	}
	// Unsuspended, the same token works again: nothing was consumed.
	e.exec(`update users set status = 'active' where id = $1`, u.ID)
	pairOf(t, e.refresh(p.refresh))
}

func TestRefresh_UnknownTokenIsInvalid(t *testing.T) {
	e := newEnv(t)
	tok := base64.RawURLEncoding.EncodeToString([]byte(strings.Repeat("k", 32)))
	e.wantUnauthorized(e.refresh(tok), "auth.refresh_invalid")
}

func TestRefresh_MalformedTokenIsInvalid(t *testing.T) {
	e := newEnv(t)
	valid43 := strings.Repeat("A", 43)
	for name, tok := range map[string]string{
		"empty":            "",
		"symbols":          "!!!",
		"short":            strings.Repeat("A", 10),
		"long":             strings.Repeat("A", 200),
		"43 invalid chars": strings.Repeat("!", 43),
		"padded":           valid43[:42] + "==",
		"std alphabet":     strings.Repeat("+/", 21) + "A",
		"whitespace":       " " + valid43,
	} {
		t.Run(name, func(t *testing.T) {
			e.wantUnauthorized(e.refresh(tok), "auth.refresh_invalid")
		})
	}
	// A missing field is the same as an empty token, never a 422.
	e.wantUnauthorized(e.post(pathRefresh, map[string]any{}), "auth.refresh_invalid")
	if n := e.count(`select count(*) from audit_log where action = $1`, audit.AuthRefreshReuseDetected); n != 0 {
		t.Errorf("malformed tokens must not audit, rows = %d", n)
	}
}

// flakyLegal is a legal.Current whose Versions can be switched to fail.
type flakyLegal struct {
	legal.Current
	fail atomic.Bool
}

func (f *flakyLegal) Versions(ctx context.Context) (legal.Versions, error) {
	if f.fail.Load() {
		return legal.Versions{}, errors.New("legal store down")
	}
	return f.Current.Versions(ctx)
}

// A failure to read the legal versions must not leave the client logged
// out: it happens before the rotation commits, so the presented token stays
// valid and works once the lookup recovers.
func TestRefresh_LegalFailureDoesNotRotate(t *testing.T) {
	fl := &flakyLegal{}
	e := newEnv(t, func(d *auth.Deps) {
		fl.Current = d.Legal
		d.Legal = fl
	})
	e.activeDriver("ana@example.com")
	first := e.loginPair("ana@example.com")

	fl.fail.Store(true)
	if rec := e.refresh(first.refresh); rec.Code != http.StatusInternalServerError {
		t.Fatalf("refresh with legal down = %d %s, want 500", rec.Code, rec.Body)
	}
	fl.fail.Store(false)
	if rec := e.refresh(first.refresh); rec.Code != http.StatusOK {
		t.Errorf("retry with the same token = %d %s, want 200", rec.Code, rec.Body)
	}
}
