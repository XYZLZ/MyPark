package auth_test

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/XYZLZ/MyPark/apps/server/internal/domain/secret"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/legal"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/hashing"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/jwt"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/ratelimit"
)

const (
	pathLogin     = "/v1/auth/login"
	loginPassword = "Original-pass-77"
)

// activeDriver inserts an active driver that accepted the current legal
// versions.
func (e *env) activeDriver(email string) userInfo {
	e.t.Helper()
	u := e.insertUser(email, "driver", "active", true)
	e.exec(`update users set terms_version=$2, terms_accepted_at=now(), privacy_version=$2,
		privacy_accepted_at=now() where id=$1`, u.ID, legal.InitialVersion)
	return u
}

func loginBody(email, password string) map[string]any {
	return map[string]any{"email": email, "password": password, "client_kind": "mobile"}
}

// wantFailedReasons asserts the auth.login_failed rows of the user, oldest
// first, carry exactly these reasons.
func (e *env) wantFailedReasons(email string, want ...string) {
	e.t.Helper()
	u := e.user(email)
	rows, err := e.db.Super.Query(e.t.Context(), `select after->>'reason' from audit_log
		where action='auth.login_failed' and entity_type='user' and entity_id=$1
		and actor_kind='anonymous' order by id`, u.PublicID.String())
	if err != nil {
		e.t.Fatal(err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			e.t.Fatal(err)
		}
		got = append(got, s)
	}
	if !reflect.DeepEqual(got, want) {
		e.t.Errorf("login_failed reasons = %v, want %v", got, want)
	}
}

func TestLogin_Success(t *testing.T) {
	e := newEnv(t)
	u := e.activeDriver("ana@example.com")
	rec := e.post(pathLogin, loginBody(" Ana@Example.com ", loginPassword))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
	data, _ := rec.json()["data"].(map[string]any)
	if data["token_type"] != "Bearer" || data["expires_in"] != float64(900) ||
		data["refresh_expires_in"] != float64(2592000) || data["legal_acceptance_required"] != false {
		t.Errorf("data = %v", data)
	}
	refresh, _ := data["refresh_token"].(string)
	raw, err := base64.RawURLEncoding.DecodeString(refresh)
	if err != nil || len(raw) != 32 {
		t.Errorf("refresh_token = %q does not decode to 32 bytes (err %v)", refresh, err)
	}
	user, _ := data["user"].(map[string]any)
	if user["first_name"] != "Orig" || user["last_name"] != "Inal" || user["id"] != u.PublicID.String() ||
		user["role"] != "driver" || user["phone"] != nil {
		t.Errorf("user = %v", user)
	}
	if _, ok := user["full_name"]; ok {
		t.Error("user has full_name")
	}
	legalObj, _ := user["legal"].(map[string]any)
	if legalObj["terms_version"] != legal.InitialVersion {
		t.Errorf("legal = %v", legalObj)
	}
	if strings.Contains(rec.Body.String(), "[redacted]") {
		t.Error("body holds a redaction marker")
	}

	if n := e.count(`select count(*) from auth_sessions where user_id=$1 and client_kind='mobile'
		and absolute_expires_at=$2`, u.ID, e.clock.Now().Add(90*24*time.Hour)); n != 1 {
		t.Errorf("sessions = %d, want 1 mobile with absolute now+90d", n)
	}
	if n := e.count(`select count(*) from refresh_tokens where expires_at=$1 and rotated_at is null`,
		e.clock.Now().Add(30*24*time.Hour)); n != 1 {
		t.Errorf("refresh tokens = %d, want 1 expiring in 30d", n)
	}
	if n := e.count(`select count(*) from refresh_tokens where token_hash = sha256($1::bytea)`, raw); n != 1 {
		t.Errorf("stored hash does not match the returned token: %d", n)
	}
	sid := e.text(`select public_id::text from auth_sessions where user_id=$1`, u.ID)
	if got := e.text(`select after->>'client_kind' from audit_log where action='auth.login_succeeded'
		and entity_type='session' and entity_id=$1`, sid); got != "mobile" {
		t.Errorf("audit client_kind = %s", got)
	}
	access, _ := data["access_token"].(string)
	verifier := jwt.NewVerifier(map[string]ed25519.PublicKey{"test": ed25519.NewKeyFromSeed(jwtSeed).Public().(ed25519.PublicKey)}, e.clock)
	claims, err := verifier.Verify(access)
	if err != nil || claims.Subject != u.PublicID || claims.SessionID.String() != sid {
		t.Errorf("claims = %+v, err %v", claims, err)
	}
}

func TestLogin_OneVerifyPerPath(t *testing.T) {
	e := newEnv(t)
	e.activeDriver("ok@example.com")
	e.insertUser("deleted@example.com", "driver", "active", true)
	e.exec(`update users set status='deleted', deleted_at=now() where email='deleted@example.com'`)
	e.insertUser("invitee@example.com", "admin", "pending_verification", false)
	cases := []struct {
		name, email, password string
		dummy                 bool
	}{
		{"unknown email", "nobody@example.com", loginPassword, true},
		{"deleted user", "deleted@example.com", loginPassword, true},
		{"invitee without hash", "invitee@example.com", loginPassword, true},
		{"wrong password", "ok@example.com", "Wrong-pass-123", false},
		{"correct password", "ok@example.com", loginPassword, false},
	}
	for _, c := range cases {
		e.hasher.resetVerifies()
		e.post(pathLogin, loginBody(c.email, c.password))
		got := e.hasher.verifies()
		if len(got) != 1 {
			t.Errorf("%s: Verify calls = %d, want 1", c.name, len(got))
			continue
		}
		if isDummy := got[0] == e.hasher.DummyHash(); isDummy != c.dummy {
			t.Errorf("%s: verified against dummy = %v, want %v", c.name, isDummy, c.dummy)
		}
	}
}

func TestLogin_WrongPasswordAndUnknownIdentical(t *testing.T) {
	e := newEnv(t)
	e.activeDriver("ana@example.com")
	wrong := e.post(pathLogin, loginBody("ana@example.com", "Wrong-pass-123"))
	unknown := e.post(pathLogin, loginBody("nobody@example.com", "Wrong-pass-123"))
	for _, r := range []resp{wrong, unknown} {
		if r.Code != http.StatusUnauthorized || r.errCode() != "auth.invalid_credentials" {
			t.Fatalf("status %d code %q", r.Code, r.errCode())
		}
	}
	if !reflect.DeepEqual(wrong.normalized(), unknown.normalized()) {
		t.Errorf("bodies differ: %v vs %v", wrong.normalized(), unknown.normalized())
	}
	if !reflect.DeepEqual(wrong.headerNames(), unknown.headerNames()) {
		t.Errorf("headers differ: %v vs %v", wrong.headerNames(), unknown.headerNames())
	}
}

func TestLogin_NotVerifiedOnlyWithCorrectPassword(t *testing.T) {
	e := newEnv(t)
	e.insertUser("ana@example.com", "driver", "pending_verification", true)
	if r := e.post(pathLogin, loginBody("ana@example.com", "Wrong-pass-123")); r.Code != http.StatusUnauthorized ||
		r.errCode() != "auth.invalid_credentials" {
		t.Errorf("wrong pw: %d %s", r.Code, r.errCode())
	}
	if r := e.post(pathLogin, loginBody("ana@example.com", loginPassword)); r.Code != http.StatusForbidden ||
		r.errCode() != "auth.email_not_verified" {
		t.Errorf("right pw: %d %s", r.Code, r.errCode())
	}
	e.wantFailedReasons("ana@example.com", "password", "not_verified")
	if n := e.count(`select count(*) from auth_sessions`); n != 0 {
		t.Errorf("sessions = %d, want 0", n)
	}
}

func TestLogin_SuspendedOnlyWithCorrectPassword(t *testing.T) {
	e := newEnv(t)
	e.insertUser("ana@example.com", "driver", "suspended", true)
	if r := e.post(pathLogin, loginBody("ana@example.com", "Wrong-pass-123")); r.errCode() != "auth.invalid_credentials" {
		t.Errorf("wrong pw: %d %s", r.Code, r.errCode())
	}
	if r := e.post(pathLogin, loginBody("ana@example.com", loginPassword)); r.Code != http.StatusForbidden ||
		r.errCode() != "auth.account_suspended" {
		t.Errorf("right pw: %d %s", r.Code, r.errCode())
	}
	e.wantFailedReasons("ana@example.com", "password", "suspended")
	if n := e.count(`select count(*) from auth_sessions`); n != 0 {
		t.Errorf("sessions = %d, want 0", n)
	}
}

func TestLogin_FailedAuditCommitsForKnownEmailOnly(t *testing.T) {
	e := newEnv(t)
	u := e.activeDriver("ana@example.com")
	if r := e.post(pathLogin, loginBody("ana@example.com", "Wrong-pass-123")); r.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d", r.Code)
	}
	if got := e.text(`select after->>'reason' from audit_log where action='auth.login_failed'
		and entity_type='user' and entity_id=$1`, u.PublicID.String()); got != "password" {
		t.Errorf("audit reason = %s, want password", got)
	}
	e.post(pathLogin, loginBody("nobody@example.com", "Wrong-pass-123"))
	if n := e.count(`select count(*) from audit_log where action='auth.login_failed'`); n != 1 {
		t.Errorf("failed-login audit rows = %d, want 1", n)
	}
}

func TestLogin_RehashOnOldParams(t *testing.T) {
	e := newEnv(t)
	u := e.activeDriver("ana@example.com")
	oldParams := fastParams
	oldParams.Memory = 512
	oldHash, err := hashing.NewArgon2id(oldParams, 1).Hash(t.Context(), loginPassword)
	if err != nil {
		t.Fatal(err)
	}
	e.exec(`update users set password_hash=$2 where id=$1`, u.ID, oldHash)
	if r := e.post(pathLogin, loginBody("ana@example.com", loginPassword)); r.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", r.Code, r.Body)
	}
	got := e.text(`select password_hash from users where id=$1`, u.ID)
	if !strings.HasPrefix(got, "$argon2id$v=19$m=1024,t=1,p=1$") {
		t.Errorf("hash not upgraded: %.32s", got)
	}
	if ok, _, err := e.hasher.Verify(t.Context(), got, loginPassword); err != nil || !ok {
		t.Errorf("upgraded hash does not verify: ok=%v err=%v", ok, err)
	}
}

func TestLogin_LegalPendingDriver(t *testing.T) {
	e := newEnv(t)
	u := e.activeDriver("ana@example.com")
	e.exec(`update users set terms_version='2025-01-01', privacy_version='2025-01-01' where id=$1`, u.ID)
	rec := e.post(pathLogin, loginBody("ana@example.com", loginPassword))
	data, _ := rec.json()["data"].(map[string]any)
	if rec.Code != http.StatusOK || data["legal_acceptance_required"] != true {
		t.Errorf("status %d data %v", rec.Code, data)
	}
}

func TestLogin_AdminIsNeverLegalPending(t *testing.T) {
	e := newEnv(t)
	e.insertUser("boss@example.com", "admin", "active", true)
	rec := e.post(pathLogin, loginBody("boss@example.com", loginPassword))
	data, _ := rec.json()["data"].(map[string]any)
	if rec.Code != http.StatusOK || data["legal_acceptance_required"] != false {
		t.Errorf("status %d data %v", rec.Code, data)
	}
}

func TestLogin_RateLimitBeforeHashing(t *testing.T) {
	e := newEnv(t)
	e.activeDriver("ana@example.com")
	e.limiter.deny = map[string]time.Duration{ratelimit.LoginIPEmail.Name: time.Minute}
	e.hasher.resetVerifies()
	rec := e.post(pathLogin, loginBody(" ANA@example.com", loginPassword))
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d", rec.Code)
	}
	if n := len(e.hasher.verifies()); n != 0 {
		t.Errorf("Verify calls = %d, want 0", n)
	}
	keys := e.limiter.keysByRule[ratelimit.LoginIPEmail.Name]
	if len(keys) != 1 || !strings.HasSuffix(keys[0], "|ana@example.com") {
		t.Errorf("login_ip_email keys = %v", keys)
	}

	e.limiter.deny = map[string]time.Duration{ratelimit.LoginEmail.Name: time.Minute}
	rec = e.post(pathLogin, loginBody("Ana@Example.com", loginPassword))
	if rec.Code != http.StatusTooManyRequests || len(e.hasher.verifies()) != 0 {
		t.Errorf("login_email: status %d, verifies %d", rec.Code, len(e.hasher.verifies()))
	}
	if k := e.limiter.keysByRule[ratelimit.LoginEmail.Name]; len(k) != 1 || k[0] != "ana@example.com" {
		t.Errorf("login_email keys = %v", k)
	}
}

func TestLogin_SameAccountWebAndMobile(t *testing.T) {
	e := newEnv(t)
	u := e.activeDriver("ana@example.com")
	for _, kind := range []string{"web", "mobile"} {
		b := loginBody("ana@example.com", loginPassword)
		b["client_kind"] = kind
		if r := e.post(pathLogin, b); r.Code != http.StatusOK {
			t.Fatalf("%s: status = %d", kind, r.Code)
		}
	}
	if n := e.count(`select count(distinct client_kind) from auth_sessions where user_id=$1 and revoked_at is null`, u.ID); n != 2 {
		t.Errorf("live sessions by kind = %d, want 2", n)
	}
	if n := e.count(`select count(*) from refresh_tokens where rotated_at is null`); n != 2 {
		t.Errorf("live refresh tokens = %d, want 2", n)
	}
}

func TestLogin_InvalidClientKind(t *testing.T) {
	e := newEnv(t)
	b := loginBody("ana@example.com", loginPassword)
	b["client_kind"] = "desktop"
	if r := e.post(pathLogin, b); r.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want 422", r.Code)
	}
}

// A reset that commits while a login is between its password check and its
// transaction must win: the login answers like a wrong password and leaves
// no live session behind.
func TestLogin_ResetDuringVerifyWins(t *testing.T) {
	e := newEnv(t)
	u := e.activeDriver("ana@example.com")
	code := e.requestReset("ana@example.com")
	var resetErr error
	e.hasher.onVerify = func(ctx context.Context, _ string) {
		resetErr = e.svc.Reset(ctx, "ana@example.com", secret.String(code), secret.String(newPass))
	}
	rec := e.post(pathLogin, loginBody("ana@example.com", loginPassword))
	if resetErr != nil {
		t.Fatalf("reset: %v", resetErr)
	}
	if rec.Code != http.StatusUnauthorized || rec.errCode() != "auth.invalid_credentials" {
		t.Errorf("login = %d %s, want 401 auth.invalid_credentials", rec.Code, rec.Body)
	}
	if n := e.count(`select count(*) from auth_sessions where user_id=$1 and revoked_at is null`, u.ID); n != 0 {
		t.Errorf("live sessions = %d, want 0", n)
	}
}
