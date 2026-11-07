package users_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/XYZLZ/MyPark/apps/server/internal/features/legal"
)

func TestGetMe_DriverAndAdmin(t *testing.T) {
	e := newEnv(t)
	d := e.driver("ana@example.com")
	a := e.newUser("admin@example.com", "admin", "")

	r := e.do(d.Token, http.MethodGet, "/v1/me", nil)
	if r.Code != http.StatusOK {
		t.Fatalf("driver: %d %s", r.Code, r.Body)
	}
	data := r.data()
	if data["first_name"] != "Ana" || data["last_name"] != "Pérez" || data["role"] != "driver" || data["phone"] != "+18095551234" {
		t.Errorf("driver data = %#v", data)
	}
	if got := data["legal"].(map[string]any)["terms_version"]; got != legal.InitialVersion {
		t.Errorf("legal.terms_version = %v", got)
	}

	r = e.do(a.Token, http.MethodGet, "/v1/me", nil)
	if r.Code != http.StatusOK || r.data()["role"] != "admin" {
		t.Fatalf("admin: %d %s", r.Code, r.Body)
	}
	for k, v := range r.data()["legal"].(map[string]any) {
		if v != nil {
			t.Errorf("admin legal.%s = %v, want null", k, v)
		}
	}
}

func TestGetMe_AllowedWhileLegalPending(t *testing.T) {
	e := newEnv(t)
	d := e.newUser("old@example.com", "driver", "1999-01-01")
	if r := e.do(d.Token, http.MethodGet, "/v1/me", nil); r.Code != http.StatusOK {
		t.Fatalf("%d %s", r.Code, r.Body)
	}
}

func TestPatchMe_UpdatesAndAuditsFieldNames(t *testing.T) {
	e := newEnv(t)
	d := e.driver("ana@example.com")
	r := e.do(d.Token, http.MethodPatch, "/v1/me", map[string]any{"first_name": " Ana María ", "locale": "en"})
	if r.Code != http.StatusOK {
		t.Fatalf("%d %s", r.Code, r.Body)
	}
	if r.data()["first_name"] != "Ana María" || r.data()["locale"] != "en" || r.data()["last_name"] != "Pérez" {
		t.Errorf("data = %#v", r.data())
	}
	after, actor, entity := e.lastAudit("user.profile_updated")
	if after != `{"fields": ["first_name", "locale"]}` || actor != d.ID || entity != d.PublicID.String() {
		t.Errorf("audit = %s actor=%d entity=%s", after, actor, entity)
	}
	if strings.Contains(after, "Ana") {
		t.Error("audit holds a value")
	}
}

func TestPatchMe_PhoneNullClears(t *testing.T) {
	e := newEnv(t)
	d := e.driver("ana@example.com")
	r := e.do(d.Token, http.MethodPatch, "/v1/me", `{"phone": null}`)
	if r.Code != http.StatusOK || r.data()["phone"] != nil {
		t.Fatalf("%d %s", r.Code, r.Body)
	}
	if got := e.text(`select phone from users where id = $1`, d.ID); got != "<null>" {
		t.Errorf("stored phone = %q", got)
	}
}

func TestPatchMe_EmptyPhoneClears(t *testing.T) {
	e := newEnv(t)
	d := e.driver("ana@example.com")
	for _, body := range []string{`{"phone": ""}`, `{"phone": "   "}`} {
		e.exec(`update users set phone = '+18095551234' where id = $1`, d.ID)
		r := e.do(d.Token, http.MethodPatch, "/v1/me", body)
		if r.Code != http.StatusOK || r.data()["phone"] != nil {
			t.Fatalf("%s: %d %s", body, r.Code, r.Body)
		}
		if got := e.text(`select phone from users where id = $1`, d.ID); got != "<null>" {
			t.Errorf("%s: stored phone = %q", body, got)
		}
	}
}

func TestPatchMe_SetsValidPhoneAndRejectsBad(t *testing.T) {
	e := newEnv(t)
	d := e.driver("ana@example.com")
	r := e.do(d.Token, http.MethodPatch, "/v1/me", map[string]any{"phone": "+18295550000"})
	if r.Code != http.StatusOK || r.data()["phone"] != "+18295550000" {
		t.Fatalf("%d %s", r.Code, r.Body)
	}
	for _, bad := range []string{"809-555-1234", "+0123456", "12345"} {
		r := e.do(d.Token, http.MethodPatch, "/v1/me", map[string]any{"phone": bad})
		if r.Code != http.StatusUnprocessableEntity || r.errCode() != "validation.failed" {
			t.Errorf("phone %q: %d %s", bad, r.Code, r.Body)
		}
	}
}

func TestPatchMe_BlankNamesAre422(t *testing.T) {
	e := newEnv(t)
	d := e.driver("ana@example.com")
	for _, body := range []string{
		`{"first_name": "   "}`, `{"last_name": ""}`, `{"first_name": "\t\n"}`,
		`{"locale": "fr"}`, `{"first_name": "` + strings.Repeat("a", 61) + `"}`,
	} {
		r := e.do(d.Token, http.MethodPatch, "/v1/me", body)
		if r.Code != http.StatusUnprocessableEntity || r.errCode() != "validation.failed" {
			t.Errorf("%s: %d %s", body, r.Code, r.Body)
		}
	}
	if got := e.text(`select first_name from users where id = $1`, d.ID); got != "Ana" {
		t.Errorf("first_name = %q", got)
	}
}

func TestPatchMe_RoleAndEmailIgnored(t *testing.T) {
	e := newEnv(t)
	d := e.driver("ana@example.com")
	r := e.do(d.Token, http.MethodPatch, "/v1/me", map[string]any{"role": "admin", "email": "x@y.z"})
	if r.Code != http.StatusOK || r.data()["role"] != "driver" || r.data()["email"] != "ana@example.com" {
		t.Fatalf("%d %s", r.Code, r.Body)
	}
	if n := e.count(`select count(*) from audit_log where action = 'user.profile_updated'`); n != 0 {
		t.Errorf("audit rows = %d for a no-op patch", n)
	}
}

func TestPatchMe_BlockedWhileLegalPending(t *testing.T) {
	e := newEnv(t)
	d := e.newUser("old@example.com", "driver", "1999-01-01")
	r := e.do(d.Token, http.MethodPatch, "/v1/me", map[string]any{"first_name": "Nuevo"})
	if r.Code != http.StatusForbidden || r.errCode() != "legal.acceptance_required" {
		t.Fatalf("%d %s", r.Code, r.Body)
	}
}

func TestPatchMe_AdminCanPatch(t *testing.T) {
	e := newEnv(t)
	a := e.newUser("admin@example.com", "admin", "")
	r := e.do(a.Token, http.MethodPatch, "/v1/me", map[string]any{"last_name": "Root"})
	if r.Code != http.StatusOK || r.data()["last_name"] != "Root" {
		t.Fatalf("%d %s", r.Code, r.Body)
	}
}

func TestDeleteMe_WrongPassword403(t *testing.T) {
	e := newEnv(t)
	d := e.driver("ana@example.com")
	before := e.text(`select u::text from users u where id = $1`, d.ID)
	r := e.do(d.Token, http.MethodDelete, "/v1/me", map[string]any{"password": "not-the-password"})
	if r.Code != http.StatusForbidden || r.errCode() != "auth.password_incorrect" {
		t.Fatalf("%d %s", r.Code, r.Body)
	}
	if after := e.text(`select u::text from users u where id = $1`, d.ID); after != before {
		t.Error("user row changed")
	}
	if n := e.count(`select count(*) from audit_log where action = 'user.deleted'`); n != 0 {
		t.Errorf("deleted audit rows = %d", n)
	}
	if r := e.do(d.Token, http.MethodGet, "/v1/me", nil); r.Code != http.StatusOK {
		t.Errorf("session after failed delete: %d", r.Code)
	}
}

func TestDeleteMe_AnonymizesAndFrees(t *testing.T) {
	e := newEnv(t)
	d := e.driver("ana@example.com")
	second := e.login(d.Email)
	vehicle := map[string]any{"plate": "ABC 123", "make": "Toyota", "model": "Corolla", "color": "Gris"}
	if r := e.do(d.Token, http.MethodPost, "/v1/vehicles", vehicle); r.Code != http.StatusCreated {
		t.Fatalf("vehicle: %d %s", r.Code, r.Body)
	}
	e.exec(`insert into outbox_messages (template, locale, to_address, user_id, payload, dedupe_key)
		values ('verify_email', 'en', $1, $2, '{"code":"123456"}', 'k1')`, d.Email, d.ID)
	e.exec(`insert into auth_codes (user_id, purpose, code_hash, expires_at) values ($1, 'reset_password', $2, $3)`,
		d.ID, e.codes.Hash("reset_password", d.PublicID, "123456"), e.clock.Now().Add(time.Hour))

	r := e.do(d.Token, http.MethodDelete, "/v1/me", map[string]any{"password": password})
	if r.Code != http.StatusNoContent || r.Body.Len() != 0 {
		t.Fatalf("%d %s", r.Code, r.Body)
	}

	row := e.text(`select concat_ws('|', email, first_name, last_name, coalesce(phone, 'NULL'),
		coalesce(password_hash, 'NULL'), status, (deleted_at = $2)::text) from users where id = $1`, d.ID, e.clock.Now())
	want := "deleted+" + d.PublicID.String() + "@deleted.invalid|deleted|deleted|NULL|NULL|deleted|true"
	if row != want {
		t.Errorf("user row = %q, want %q", row, want)
	}
	if n := e.count(`select count(*) from vehicles where user_id = $1 and deleted_at is null`, d.ID); n != 0 {
		t.Errorf("live vehicles = %d", n)
	}
	if n := e.count(`select count(*) from auth_sessions where user_id = $1 and revoked_reason = 'account_deleted'`, d.ID); n != 2 {
		t.Errorf("revoked sessions = %d, want 2", n)
	}
	if n := e.count(`select count(*) from auth_sessions where user_id = $1 and revoked_at is null`, d.ID); n != 0 {
		t.Errorf("live sessions = %d", n)
	}
	if n := e.count(`select count(*) from auth_codes where user_id = $1 and consumed_at is null`, d.ID); n != 0 {
		t.Errorf("live codes = %d", n)
	}
	if got := e.text(`select payload::text from outbox_messages where dedupe_key = 'k1'`); strings.Contains(got, "code") {
		t.Errorf("outbox payload = %s", got)
	}
	after, actor, entity := e.lastAudit("user.deleted")
	if actor != d.ID || entity != d.PublicID.String() || strings.Contains(after, "ana") || strings.Contains(after, "@") {
		t.Errorf("audit after=%q actor=%d entity=%q", after, actor, entity)
	}

	for _, tok := range []string{d.Token, second} {
		if r := e.do(tok, http.MethodGet, "/v1/me", nil); r.Code != http.StatusUnauthorized || r.errCode() != "auth.session_revoked" {
			t.Errorf("old token: %d %s", r.Code, r.Body)
		}
	}

	// The address is free again.
	reg := map[string]any{
		"email": d.Email, "password": "Zx9!kelp-orbit", "first_name": "Nueva", "last_name": "Cuenta",
		"accepted_terms_version": legal.InitialVersion, "accepted_privacy_version": legal.InitialVersion,
	}
	if r := e.do("", http.MethodPost, "/v1/auth/register", reg); r.Code != http.StatusAccepted {
		t.Fatalf("re-register: %d %s", r.Code, r.Body)
	}
	if n := e.count(`select count(*) from users where email = $1 and status = 'pending_verification'`, d.Email); n != 1 {
		t.Errorf("pending users = %d", n)
	}
}

func TestDeleteMe_RollsBackWhenAuditFails(t *testing.T) {
	e := newEnv(t)
	d := e.driver("ana@example.com")
	// Break the audit insert so the last step of the transaction fails.
	e.exec(`alter table audit_log add constraint no_deletes check (action <> 'user.deleted')`)
	r := e.do(d.Token, http.MethodDelete, "/v1/me", map[string]any{"password": password})
	if r.Code != http.StatusInternalServerError {
		t.Fatalf("%d %s", r.Code, r.Body)
	}
	if got := e.text(`select status from users where id = $1`, d.ID); got != "active" {
		t.Errorf("status = %q, want active (rolled back)", got)
	}
	if got := e.text(`select first_name from users where id = $1`, d.ID); got != "Ana" {
		t.Errorf("first_name = %q after rollback", got)
	}
	if n := e.count(`select count(*) from auth_sessions where user_id = $1 and revoked_at is not null`, d.ID); n != 0 {
		t.Errorf("revoked sessions = %d after rollback", n)
	}
}

func TestDeleteMe_AllowedWhileLegalPending(t *testing.T) {
	e := newEnv(t)
	d := e.newUser("old@example.com", "driver", "1999-01-01")
	r := e.do(d.Token, http.MethodDelete, "/v1/me", map[string]any{"password": password})
	if r.Code != http.StatusNoContent {
		t.Fatalf("%d %s", r.Code, r.Body)
	}
}

func TestDeleteMe_Admin403(t *testing.T) {
	e := newEnv(t)
	a := e.newUser("admin@example.com", "admin", "")
	r := e.do(a.Token, http.MethodDelete, "/v1/me", map[string]any{"password": password})
	if r.Code != http.StatusForbidden || r.errCode() != "auth.forbidden" {
		t.Fatalf("%d %s", r.Code, r.Body)
	}
	if got := e.text(`select status from users where id = $1`, a.ID); got != "active" {
		t.Errorf("status = %q", got)
	}
}

func TestDeleteMe_NullBody400(t *testing.T) {
	e := newEnv(t)
	d := e.driver("ana@example.com")
	for _, body := range []string{"null", "[]", `{}garbage`} {
		r := e.do(d.Token, http.MethodDelete, "/v1/me", body)
		if r.Code != http.StatusBadRequest || r.errCode() != "http.invalid_json" {
			t.Errorf("body %q: %d %s", body, r.Code, r.Body)
		}
	}
	if r := e.do(d.Token, http.MethodDelete, "/v1/me", `{}`); r.Code != http.StatusUnprocessableEntity {
		t.Errorf("missing password: %d %s", r.Code, r.Body)
	}
}

func TestAcceptLegal_ClearsPending(t *testing.T) {
	e := newEnv(t)
	d := e.newUser("old@example.com", "driver", "1999-01-01")
	if r := e.do(d.Token, http.MethodGet, "/v1/vehicles", nil); r.Code != http.StatusForbidden {
		t.Fatalf("gate before acceptance: %d", r.Code)
	}
	e.clock.Advance(time.Minute)
	body := map[string]any{"terms_version": legal.InitialVersion, "privacy_version": legal.InitialVersion}
	r := e.do(d.Token, http.MethodPost, "/v1/me/legal-acceptance", body)
	if r.Code != http.StatusNoContent || r.Body.Len() != 0 {
		t.Fatalf("%d %s", r.Code, r.Body)
	}
	row := e.text(`select concat_ws('|', terms_version, privacy_version, (terms_accepted_at = $2)::text,
		(privacy_accepted_at = $2)::text) from users where id = $1`, d.ID, e.clock.Now())
	if want := legal.InitialVersion + "|" + legal.InitialVersion + "|true|true"; row != want {
		t.Errorf("row = %q, want %q", row, want)
	}
	after, actor, entity := e.lastAudit("user.legal_accepted")
	var got map[string]string
	if err := json.Unmarshal([]byte(after), &got); err != nil ||
		got["terms_version"] != legal.InitialVersion || got["privacy_version"] != legal.InitialVersion || len(got) != 2 {
		t.Errorf("audit after = %s", after)
	}
	if actor != d.ID || entity != d.PublicID.String() {
		t.Errorf("audit actor=%d entity=%q", actor, entity)
	}
	if r := e.do(d.Token, http.MethodGet, "/v1/vehicles", nil); r.Code != http.StatusOK {
		t.Errorf("gate after acceptance: %d %s", r.Code, r.Body)
	}
}

func TestAcceptLegal_Outdated409(t *testing.T) {
	e := newEnv(t)
	d := e.newUser("old@example.com", "driver", "1999-01-01")
	body := map[string]any{"terms_version": "1999-01-01", "privacy_version": legal.InitialVersion}
	r := e.do(d.Token, http.MethodPost, "/v1/me/legal-acceptance", body)
	if r.Code != http.StatusConflict || r.errCode() != "legal.version_outdated" {
		t.Fatalf("%d %s", r.Code, r.Body)
	}
	if got := e.text(`select terms_version from users where id = $1`, d.ID); got != "1999-01-01" {
		t.Errorf("terms_version = %q", got)
	}
}

func TestAcceptLegal_AdminForbiddenAndEmptyVersions(t *testing.T) {
	e := newEnv(t)
	a := e.newUser("admin@example.com", "admin", "")
	body := map[string]any{"terms_version": legal.InitialVersion, "privacy_version": legal.InitialVersion}
	if r := e.do(a.Token, http.MethodPost, "/v1/me/legal-acceptance", body); r.Code != http.StatusForbidden {
		t.Errorf("admin: %d", r.Code)
	}
	d := e.driver("ana@example.com")
	if r := e.do(d.Token, http.MethodPost, "/v1/me/legal-acceptance", `{}`); r.Code != http.StatusUnprocessableEntity {
		t.Errorf("empty versions: %d %s", r.Code, r.Body)
	}
}
