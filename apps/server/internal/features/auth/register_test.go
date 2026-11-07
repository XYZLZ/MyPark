package auth_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/XYZLZ/MyPark/apps/server/internal/domain/secret"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/audit"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/auth"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/ratelimit"
)

const (
	pathRegister = "/v1/auth/register"
	pathVerify   = "/v1/auth/verify"
	pathResend   = "/v1/auth/verify/resend"
)

func TestRegister_NewEmailCreatesPendingDriverCodeOutboxAudit(t *testing.T) {
	e := newEnv(t)
	rec := e.post(pathRegister, registerBody("ana@example.com"))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
	data, _ := rec.json()["data"].(map[string]any)
	if !reflect.DeepEqual(data, map[string]any{"status": "verification_pending"}) {
		t.Errorf("data = %v", data)
	}
	u := e.user("ana@example.com")
	if got := e.text(`select role||'/'||status||'/'||terms_version||'/'||privacy_version||'/'||locale from users where id=$1`, u.ID); got != "driver/pending_verification/2026-11-16/2026-11-16/es-DO" {
		t.Errorf("user row = %s", got)
	}
	if e.text(`select password_hash from users where id=$1`, u.ID) == "<null>" {
		t.Error("password hash not stored")
	}
	if n := e.count(`select count(*) from auth_codes where user_id=$1 and purpose='verify_email' and consumed_at is null`, u.ID); n != 1 {
		t.Fatalf("live codes = %d, want 1", n)
	}
	code := e.outboxCode("ana@example.com")
	if len(code) != 6 {
		t.Fatalf("outbox code %q is not 6 digits", code)
	}
	var codeID int64
	var hash []byte
	if err := e.db.Super.QueryRow(t.Context(), `select id, code_hash from auth_codes where user_id=$1`, u.ID).Scan(&codeID, &hash); err != nil {
		t.Fatal(err)
	}
	if want := e.codes.Hash("verify_email", u.PublicID, code); string(hash) != string(want) {
		t.Error("stored code_hash != CodeHasher.Hash(purpose, user, outbox code)")
	}
	if got := e.text(`select dedupe_key from outbox_messages where to_address='ana@example.com'`); got != fmt.Sprintf("verify_email:%d", codeID) {
		t.Errorf("dedupe_key = %s", got)
	}
	if got := e.text(`select actor_kind||'/'||entity_type||'/'||entity_id||'/'||after::text from audit_log where action='auth.registered'`); got != fmt.Sprintf(`anonymous/user/%s/{"locale": "es-DO"}`, u.PublicID) {
		t.Errorf("audit row = %s", got)
	}
}

func TestRegister_AtomicOnFailureAfterEnqueue(t *testing.T) {
	e := newEnv(t, func(d *auth.Deps) { d.Audit = failingAudit{} })
	err := e.svc.Register(t.Context(), auth.RegisterInput{
		Email: "ana@example.com", Password: secret.String("Zx9!kelp-orbit"), FirstName: "Ana", LastName: "P",
		Locale: "es-DO", TermsVersion: "2026-11-16", PrivacyVersion: "2026-11-16",
	})
	if err == nil {
		t.Fatal("Register = nil, want the audit error")
	}
	for _, table := range []string{"users", "auth_codes", "outbox_messages"} {
		if n := e.count("select count(*) from " + table); n != 0 {
			t.Errorf("%s has %d rows after a failed register, want 0", table, n)
		}
	}
}

func TestRegister_RoleInBodyIgnored(t *testing.T) {
	e := newEnv(t)
	rec := e.post(pathRegister, registerBody("ana@example.com", func(b map[string]any) { b["role"] = "admin" }))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d", rec.Code)
	}
	if got := e.text(`select role from users where email='ana@example.com'`); got != "driver" {
		t.Errorf("role = %s, want driver", got)
	}
}

func TestRegister_OutdatedLegal409(t *testing.T) {
	e := newEnv(t)
	rec := e.post(pathRegister, registerBody("ana@example.com", func(b map[string]any) { b["accepted_terms_version"] = "2025-01-01" }))
	if rec.Code != http.StatusConflict || rec.errCode() != "legal.version_outdated" {
		t.Fatalf("status = %d code = %s", rec.Code, rec.errCode())
	}
	details := rec.json()["error"].(map[string]any)["details"].(map[string]any)
	if details["terms_version"] != "2026-11-16" {
		t.Errorf("details = %v", details)
	}
	if n := e.count(`select count(*) from users`); n != 0 {
		t.Errorf("users = %d, want 0", n)
	}
}

func TestRegister_PendingDriverIsOverwritten(t *testing.T) {
	e := newEnv(t)
	e.post(pathRegister, registerBody("ana@example.com"))
	first := e.outboxCode("ana@example.com")
	u := e.user("ana@example.com")
	// Same address with different case and padding is the same account.
	rec := e.post(pathRegister, registerBody(" ANA@Example.com ", func(b map[string]any) {
		b["password"] = "Another-pass-42"
		b["first_name"] = "Anita"
		b["locale"] = "en"
	}))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
	if n := e.count(`select count(*) from users`); n != 1 {
		t.Fatalf("users = %d, want 1", n)
	}
	if got := e.user("ana@example.com"); got.ID != u.ID {
		t.Error("pending row was replaced instead of overwritten")
	}
	if got := e.text(`select first_name||'/'||locale from users where id=$1`, u.ID); got != "Anita/en" {
		t.Errorf("names/locale = %s", got)
	}
	hash := e.text(`select password_hash from users where id=$1`, u.ID)
	ok, _, err := e.hasher.Verify(t.Context(), hash, "Another-pass-42")
	if err != nil || !ok {
		t.Errorf("new password does not verify: ok=%v err=%v", ok, err)
	}
	if n := e.count(`select count(*) from auth_codes where user_id=$1 and consumed_at is not null`, u.ID); n != 1 {
		t.Errorf("consumed codes = %d, want 1", n)
	}
	if n := e.count(`select count(*) from auth_codes where user_id=$1 and consumed_at is null`, u.ID); n != 1 {
		t.Errorf("live codes = %d, want 1", n)
	}
	second := e.outboxCode("ana@example.com")
	if second == first {
		t.Log("new code equals the old one by chance; skipping inequality check")
	}
	if n := e.count(`select count(*) from audit_log where action='auth.registration_replaced'`); n != 1 {
		t.Errorf("registration_replaced audit rows = %d, want 1", n)
	}
	// The first code no longer works, the new one does.
	if first != second {
		if r := e.post(pathVerify, map[string]any{"email": "ana@example.com", "code": first}); r.Code != http.StatusUnprocessableEntity {
			t.Errorf("old code verify = %d, want 422", r.Code)
		}
	}
	if r := e.post(pathVerify, map[string]any{"email": "ana@example.com", "code": second}); r.Code != http.StatusNoContent {
		t.Errorf("new code verify = %d, want 204 (body %s)", r.Code, r.Body)
	}
}

func TestRegister_ActiveOwnerGetsAccountExistsEmail(t *testing.T) {
	e := newEnv(t)
	u := e.insertUser("ana@example.com", "driver", "active", true)
	before := e.userSnapshot("ana@example.com")
	for range 2 {
		if rec := e.post(pathRegister, registerBody("ana@example.com")); rec.Code != http.StatusAccepted {
			t.Fatalf("status = %d", rec.Code)
		}
	}
	if got := e.userSnapshot("ana@example.com"); got != before {
		t.Error("active user row changed")
	}
	if n := e.count(`select count(*) from outbox_messages where template='account_exists'`); n != 1 {
		t.Fatalf("account_exists rows = %d, want 1 (deduped per hour)", n)
	}
	want := fmt.Sprintf("account_exists:%d:2026112012", u.ID)
	if got := e.text(`select dedupe_key from outbox_messages`); got != want {
		t.Errorf("dedupe_key = %s, want %s", got, want)
	}
	if got := e.text(`select locale from outbox_messages`); got != "en" {
		t.Errorf("locale = %s, want the owner's locale en", got)
	}
	e.clock.Advance(time.Hour)
	e.post(pathRegister, registerBody("ana@example.com"))
	if n := e.count(`select count(*) from outbox_messages where template='account_exists'`); n != 2 {
		t.Errorf("account_exists rows next hour = %d, want 2", n)
	}
	if n := e.count(`select count(*) from auth_codes`); n != 0 {
		t.Errorf("auth_codes = %d, want 0", n)
	}
}

func TestRegister_PendingAdminInviteeUntouched(t *testing.T) {
	e := newEnv(t)
	inv := e.insertUser("boss@example.com", "admin", "pending_verification", false)
	e.insertCode(inv, "admin_invite", "424242")
	userBefore := e.userSnapshot("boss@example.com")
	codeBefore := e.text(`select c::text from auth_codes c`)

	rec := e.post(pathRegister, registerBody("boss@example.com"))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d", rec.Code)
	}
	if got := e.userSnapshot("boss@example.com"); got != userBefore {
		t.Errorf("invitee row changed:\n%s\n%s", userBefore, got)
	}
	if got := e.text(`select c::text from auth_codes c`); got != codeBefore {
		t.Error("invitee code changed")
	}
	if n := e.count(`select count(*) from outbox_messages`); n != 0 {
		t.Errorf("outbox rows = %d, want 0", n)
	}
	if n := e.count(`select count(*) from users where role='admin' and status='active'`); n != 0 {
		t.Error("an active admin exists")
	}
	if n := e.count(`select count(*) from audit_log`); n != 0 {
		t.Errorf("audit rows = %d, want 0", n)
	}
}

func TestRegister_EveryPathHashesOnce(t *testing.T) {
	e := newEnv(t)
	e.insertUser("active@example.com", "driver", "active", true)
	e.insertUser("invitee@example.com", "admin", "pending_verification", false)
	e.post(pathRegister, registerBody("pending@example.com")) // creates the pending driver
	e.hasher.hashes.Store(0)
	for _, email := range []string{"new@example.com", "pending@example.com", "active@example.com", "invitee@example.com"} {
		before := e.hasher.hashes.Load()
		if rec := e.post(pathRegister, registerBody(email)); rec.Code != http.StatusAccepted {
			t.Fatalf("%s: status = %d", email, rec.Code)
		}
		if got := e.hasher.hashes.Load() - before; got != 1 {
			t.Errorf("%s: Hash calls = %d, want 1", email, got)
		}
	}
}

func TestRegister_IdenticalResponses(t *testing.T) {
	e := newEnv(t)
	e.insertUser("active@example.com", "driver", "active", true)
	e.insertUser("invitee@example.com", "admin", "pending_verification", false)
	e.post(pathRegister, registerBody("pending@example.com"))
	var bodies []map[string]any
	var headers [][]string
	for _, email := range []string{"new@example.com", "pending@example.com", "active@example.com", "invitee@example.com"} {
		rec := e.post(pathRegister, registerBody(email))
		if rec.Code != http.StatusAccepted {
			t.Fatalf("%s: status = %d", email, rec.Code)
		}
		bodies = append(bodies, rec.normalized())
		headers = append(headers, rec.headerNames())
	}
	if !reflect.DeepEqual(bodies[0]["data"], map[string]any{"status": "verification_pending"}) {
		t.Errorf("data = %v", bodies[0]["data"])
	}
	for i := 1; i < len(bodies); i++ {
		if !reflect.DeepEqual(bodies[i], bodies[0]) {
			t.Errorf("body %d = %v, want %v", i, bodies[i], bodies[0])
		}
		if !slices.Equal(headers[i], headers[0]) {
			t.Errorf("headers %d = %v, want %v", i, headers[i], headers[0])
		}
	}
}

func TestRegister_BlankNamesAndEmptyPhone(t *testing.T) {
	e := newEnv(t)
	rec := e.post(pathRegister, registerBody("ana@example.com", func(b map[string]any) { b["first_name"] = "   " }))
	if rec.Code != http.StatusUnprocessableEntity || rec.errCode() != "validation.failed" {
		t.Fatalf("blank first_name: status = %d code = %s", rec.Code, rec.errCode())
	}
	fields := rec.json()["error"].(map[string]any)["details"].(map[string]any)["fields"].([]any)
	if !reflect.DeepEqual(fields[0], map[string]any{"field": "first_name", "rule": "required", "param": ""}) {
		t.Errorf("fields[0] = %v", fields[0])
	}
	rec = e.post(pathRegister, registerBody("ana@example.com", func(b map[string]any) { b["last_name"] = "\t " }))
	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("blank last_name: status = %d, want 422", rec.Code)
	}
	rec = e.post(pathRegister, registerBody("ana@example.com", func(b map[string]any) { b["phone"] = "+0123456789" }))
	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("phone +0123456789: status = %d, want 422", rec.Code)
	}
	for _, phone := range []string{"", "   "} {
		rec = e.post(pathRegister, registerBody("ana@example.com", func(b map[string]any) { b["phone"] = phone }))
		if rec.Code != http.StatusAccepted {
			t.Fatalf("phone %q: status = %d, body %s", phone, rec.Code, rec.Body)
		}
		if got := e.text(`select phone from users where email='ana@example.com'`); got != "<null>" {
			t.Errorf("phone %q stored as %s, want NULL", phone, got)
		}
	}
	rec = e.post(pathRegister, registerBody("ana@example.com", func(b map[string]any) { b["phone"] = "+18095551234" }))
	if rec.Code != http.StatusAccepted || e.text(`select phone from users where email='ana@example.com'`) != "+18095551234" {
		t.Errorf("valid phone not stored: status = %d", rec.Code)
	}
}

func TestRegister_CommonPasswordRejected(t *testing.T) {
	e := newEnv(t)
	for _, pw := range []string{"1234567890", "charlie123", "CHARLIE123"} {
		rec := e.post(pathRegister, registerBody("ana@example.com", func(b map[string]any) { b["password"] = pw }))
		if rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("password %q: status = %d, want 422", pw, rec.Code)
		}
	}
	if e.hasher.hashes.Load() != 0 {
		t.Error("a rejected password was hashed")
	}
}

func TestRegister_EmailRateLimit(t *testing.T) {
	e := newEnv(t)
	e.limiter.deny = map[string]time.Duration{ratelimit.CodeRequestEmail.Name: 30 * time.Minute}
	rec := e.post(pathRegister, registerBody(" Ana@Example.COM "))
	if rec.Code != http.StatusTooManyRequests || rec.errCode() != "rate_limit.exceeded" {
		t.Fatalf("status = %d code = %s", rec.Code, rec.errCode())
	}
	if got := rec.Header().Get("Retry-After"); got != "1800" {
		t.Errorf("Retry-After = %q, want 1800", got)
	}
	if n := e.count(`select count(*) from users`); n != 0 {
		t.Errorf("users = %d, want 0", n)
	}
	if e.hasher.hashes.Load() != 0 {
		t.Error("Hash was called for a throttled request")
	}
	if keys := e.limiter.keysByRule[ratelimit.CodeRequestEmail.Name]; !slices.Equal(keys, []string{"ana@example.com"}) {
		t.Errorf("limiter keys = %v, want the normalized email", keys)
	}
}

func TestVerify_ActivatesDriver(t *testing.T) {
	e := newEnv(t)
	e.post(pathRegister, registerBody("ana@example.com"))
	u := e.user("ana@example.com")
	code := e.outboxCode("ana@example.com")
	e.clock.Advance(time.Minute)
	rec := e.post(pathVerify, map[string]any{"email": " Ana@example.com", "code": code})
	if rec.Code != http.StatusNoContent || rec.Body.Len() != 0 {
		t.Fatalf("status = %d body %q, want 204 empty", rec.Code, rec.Body)
	}
	if got := e.text(`select status||'/'||(email_verified_at = $2)::text from users where id=$1`, u.ID, e.clock.Now()); got != "active/true" {
		t.Errorf("user = %s", got)
	}
	if n := e.count(`select count(*) from auth_codes where user_id=$1 and consumed_at is null`, u.ID); n != 0 {
		t.Error("code not consumed")
	}
	if got := e.text(`select actor_kind||'/'||actor_user_id::text from audit_log where action=$1`, audit.AuthEmailVerified); got != fmt.Sprintf("user/%d", u.ID) {
		t.Errorf("audit = %s", got)
	}
	// The code is single use.
	if r := e.post(pathVerify, map[string]any{"email": "ana@example.com", "code": code}); r.Code != http.StatusUnprocessableEntity {
		t.Errorf("replayed code = %d, want 422", r.Code)
	}
}

func TestVerify_AttemptsPersistAcrossFailures(t *testing.T) {
	e := newEnv(t)
	e.post(pathRegister, registerBody("ana@example.com"))
	code := e.outboxCode("ana@example.com")
	for i := range 5 {
		rec := e.post(pathVerify, map[string]any{"email": "ana@example.com", "code": wrongCode(code)})
		if rec.Code != http.StatusUnprocessableEntity || rec.errCode() != "auth.code_invalid" {
			t.Fatalf("wrong code %d: status = %d code = %s", i, rec.Code, rec.errCode())
		}
	}
	if got := e.count(`select attempts from auth_codes where consumed_at is null`); got != 5 {
		t.Errorf("attempts = %d, want 5", got)
	}
	rec := e.post(pathVerify, map[string]any{"email": "ana@example.com", "code": code})
	if rec.Code != http.StatusUnprocessableEntity || rec.errCode() != "auth.code_invalid" {
		t.Errorf("6th attempt with the right code: status = %d code = %s", rec.Code, rec.errCode())
	}
	if got := e.text(`select status from users`); got != "pending_verification" {
		t.Errorf("status = %s, want pending_verification", got)
	}
}

func TestVerify_ExpiredCode(t *testing.T) {
	e := newEnv(t)
	e.post(pathRegister, registerBody("ana@example.com"))
	code := e.outboxCode("ana@example.com")
	e.clock.Advance(auth.CodeTTL + time.Second)
	if rec := e.post(pathVerify, map[string]any{"email": "ana@example.com", "code": code}); rec.Code != http.StatusUnprocessableEntity || rec.errCode() != "auth.code_invalid" {
		t.Errorf("expired code: status = %d code = %s", rec.Code, rec.errCode())
	}
}

func TestVerify_UnknownEmailSameAsWrongCode(t *testing.T) {
	e := newEnv(t)
	e.post(pathRegister, registerBody("ana@example.com"))
	code := e.outboxCode("ana@example.com")
	wrong := e.post(pathVerify, map[string]any{"email": "ana@example.com", "code": wrongCode(code)})
	unknown := e.post(pathVerify, map[string]any{"email": "nobody@example.com", "code": wrongCode(code)})
	if wrong.Code != unknown.Code || !reflect.DeepEqual(wrong.normalized(), unknown.normalized()) {
		t.Errorf("wrong = %d %v, unknown = %d %v", wrong.Code, wrong.normalized(), unknown.Code, unknown.normalized())
	}
}

func TestVerify_InviteeBehavesLikeUnknown(t *testing.T) {
	e := newEnv(t)
	inv := e.insertUser("boss@example.com", "admin", "pending_verification", false)
	e.insertCode(inv, "admin_invite", "424242")
	before := e.userSnapshot("boss@example.com")
	codeBefore := e.text(`select c::text from auth_codes c`)
	rec := e.post(pathVerify, map[string]any{"email": "boss@example.com", "code": "424242"})
	unknown := e.post(pathVerify, map[string]any{"email": "nobody@example.com", "code": "424242"})
	if rec.Code != http.StatusUnprocessableEntity || rec.errCode() != "auth.code_invalid" {
		t.Fatalf("status = %d code = %s", rec.Code, rec.errCode())
	}
	if !reflect.DeepEqual(rec.normalized(), unknown.normalized()) {
		t.Error("invitee response differs from unknown email")
	}
	if e.userSnapshot("boss@example.com") != before {
		t.Error("invitee row changed")
	}
	if e.text(`select c::text from auth_codes c`) != codeBefore {
		t.Error("invitee code was touched (attempts or consumed)")
	}
	if n := e.count(`select count(*) from auth_codes where purpose='verify_email'`); n != 0 {
		t.Errorf("verify_email codes = %d, want 0", n)
	}
}

func TestResend_PendingDriverReissues(t *testing.T) {
	e := newEnv(t)
	e.post(pathRegister, registerBody("ana@example.com"))
	u := e.user("ana@example.com")
	rec := e.post(pathResend, map[string]any{"email": " ANA@example.com "})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d", rec.Code)
	}
	data, _ := rec.json()["data"].(map[string]any)
	if !reflect.DeepEqual(data, map[string]any{"status": "accepted"}) {
		t.Errorf("data = %v", data)
	}
	if n := e.count(`select count(*) from auth_codes where user_id=$1 and consumed_at is not null`, u.ID); n != 1 {
		t.Errorf("consumed codes = %d, want 1", n)
	}
	if n := e.count(`select count(*) from auth_codes where user_id=$1 and consumed_at is null`, u.ID); n != 1 {
		t.Errorf("live codes = %d, want 1", n)
	}
	if n := e.count(`select count(*) from outbox_messages where template='verify_email'`); n != 2 {
		t.Errorf("verify_email messages = %d, want 2", n)
	}
	if keys := e.limiter.keysByRule[ratelimit.CodeRequestEmail.Name]; !slices.Equal(keys, []string{"ana@example.com", "ana@example.com"}) {
		t.Errorf("limiter keys = %v", keys)
	}
}

func TestResend_OthersDoNothing(t *testing.T) {
	e := newEnv(t)
	e.insertUser("active@example.com", "driver", "active", true)
	inv := e.insertUser("boss@example.com", "admin", "pending_verification", false)
	e.insertCode(inv, "admin_invite", "424242")
	codeBefore := e.text(`select c::text from auth_codes c`)
	var bodies []map[string]any
	for _, email := range []string{"nobody@example.com", "active@example.com", "boss@example.com"} {
		rec := e.post(pathResend, map[string]any{"email": email})
		if rec.Code != http.StatusAccepted {
			t.Fatalf("%s: status = %d", email, rec.Code)
		}
		bodies = append(bodies, rec.normalized())
	}
	for _, b := range bodies[1:] {
		if !reflect.DeepEqual(b, bodies[0]) {
			t.Errorf("body %v differs from %v", b, bodies[0])
		}
	}
	if n := e.count(`select count(*) from outbox_messages`); n != 0 {
		t.Errorf("outbox rows = %d, want 0", n)
	}
	if e.text(`select c::text from auth_codes c`) != codeBefore {
		t.Error("invitee code changed")
	}
	if n := e.count(`select count(*) from auth_codes`); n != 1 {
		t.Errorf("auth_codes = %d, want 1", n)
	}
}

// TestInvitee_NeverBecomesActiveAdmin drives every public pending-account
// flow at an invitee and checks the row can neither be taken over nor
// activated (OC-38).
func TestInvitee_NeverBecomesActiveAdmin(t *testing.T) {
	e := newEnv(t)
	inv := e.insertUser("boss@example.com", "admin", "pending_verification", false)
	e.insertCode(inv, "admin_invite", "424242")
	before := e.userSnapshot("boss@example.com")
	for _, step := range []struct {
		path string
		body map[string]any
	}{
		{pathRegister, registerBody("boss@example.com")},
		{pathResend, map[string]any{"email": "boss@example.com"}},
		{pathVerify, map[string]any{"email": "boss@example.com", "code": "424242"}},
	} {
		e.post(step.path, step.body)
	}
	if got := e.userSnapshot("boss@example.com"); got != before {
		t.Errorf("invitee row changed:\n%s\n%s", before, got)
	}
	if n := e.count(`select count(*) from users where role = 'admin' and status = 'active'`); n != 0 {
		t.Error("an active admin exists")
	}
}

func TestRegisterDTO_NotEchoedInErrors(t *testing.T) {
	e := newEnv(t)
	rec := e.post(pathRegister, registerBody("not-an-email", func(b map[string]any) { b["password"] = "short" }))
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d", rec.Code)
	}
	raw, _ := json.Marshal(rec.json())
	for _, secretValue := range []string{"not-an-email", "short"} {
		if strings.Contains(string(raw), `"`+secretValue+`"`) {
			t.Errorf("response echoes %q: %s", secretValue, raw)
		}
	}
}

func TestRegister_ConcurrentSameEmailCreatesOneAccount(t *testing.T) {
	e := newEnv(t)
	codes := make(chan int, 6)
	for range 6 {
		go func() { codes <- e.post(pathRegister, registerBody("ana@example.com")).Code }()
	}
	for range 6 {
		if got := <-codes; got != http.StatusAccepted {
			t.Errorf("status = %d, want 202", got)
		}
	}
	if n := e.count(`select count(*) from users`); n != 1 {
		t.Errorf("users = %d, want 1", n)
	}
	if n := e.count(`select count(*) from auth_codes where consumed_at is null`); n != 1 {
		t.Errorf("live codes = %d, want 1", n)
	}
}
