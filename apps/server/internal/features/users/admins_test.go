package users_test

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

const (
	adminsPath = "/v1/admin/admins"
	acceptPath = "/v1/auth/invitations/accept"
	invitePW   = "Invitee-pass-2026"
)

// invite posts a CreateAdmin request as token.
func (e *env) invite(token, email string) resp {
	e.t.Helper()
	return e.do(token, http.MethodPost, adminsPath, map[string]any{
		"email": email, "first_name": "Nora", "last_name": "Gómez Díaz", "locale": "en",
	})
}

// inviteCode returns the plaintext admin_invite code queued for email.
func (e *env) inviteCode(email string) string {
	e.t.Helper()
	return e.text(`select payload->>'code' from outbox_messages
		where template = 'admin_invite' and to_address = $1 order by id desc limit 1`, email)
}

func (e *env) accept(email, code, pw string) resp {
	e.t.Helper()
	return e.do("", http.MethodPost, acceptPath, map[string]any{"email": email, "code": code, "password": pw})
}

func TestCreateAdmin_NewInvite(t *testing.T) {
	e := newEnv(t)
	admin := e.newUser("root@example.com", "admin", "")

	r := e.invite(admin.Token, " Nora@Example.COM ")
	if r.Code != http.StatusCreated {
		t.Fatalf("status = %d %s, want 201", r.Code, r.Body)
	}
	d := r.data()
	if d["status"] != "pending_verification" || d["email"] != "nora@example.com" || d["locale"] != "en" ||
		d["first_name"] != "Nora" || d["last_name"] != "Gómez Díaz" || d["id"] == "" {
		t.Errorf("data = %v", d)
	}
	for _, k := range []string{"role", "phone", "legal", "password_hash"} {
		if _, has := d[k]; has {
			t.Errorf("AdminUser leaks %q: %v", k, d)
		}
	}
	if e.count(`select count(*) from outbox_messages where template='admin_invite' and to_address='nora@example.com' and locale='en'`) != 1 {
		t.Error("want one admin_invite email in the invitee's locale")
	}
	if got := e.text(`select role||'/'||status||'/'||coalesce(password_hash,'<null>') from users where email='nora@example.com'`); got != "admin/pending_verification/<null>" {
		t.Errorf("row = %q", got)
	}
	want := e.clock.Now().Add(48 * time.Hour)
	var exp time.Time
	if err := e.db.Super.QueryRow(t.Context(), `select c.expires_at from auth_codes c join users u on u.id=c.user_id
		where u.email='nora@example.com' and c.purpose='admin_invite' and c.consumed_at is null`).Scan(&exp); err != nil {
		t.Fatal(err)
	}
	if !exp.Equal(want) {
		t.Errorf("code expires at %v, want %v", exp, want)
	}
	after, actor, entity := e.lastAudit("admin.created")
	if actor != admin.ID || entity != d["id"] || !strings.Contains(after, `"role"`) || strings.Contains(after, "replaced_pending_driver") {
		t.Errorf("audit after=%s actor=%d entity=%s", after, actor, entity)
	}
	if kind := e.text(`select actor_kind from audit_log where action='admin.created' order by id desc limit 1`); kind != "admin" {
		t.Errorf("actor_kind = %s", kind)
	}
	if strings.Contains(after, "nora") || strings.Contains(after, "@") {
		t.Errorf("audit holds PII: %s", after)
	}
}

func TestCreateAdmin_ReissueForPendingInvitee(t *testing.T) {
	e := newEnv(t)
	admin := e.newUser("root@example.com", "admin", "")
	first := e.invite(admin.Token, "nora@example.com")
	old := e.inviteCode("nora@example.com")

	e.clock.Advance(time.Minute)
	again := e.invite(admin.Token, "NORA@example.com")
	if again.Code != http.StatusOK {
		t.Fatalf("status = %d %s, want 200", again.Code, again.Body)
	}
	if again.data()["id"] != first.data()["id"] {
		t.Errorf("id changed: %v vs %v", again.data()["id"], first.data()["id"])
	}
	if e.count(`select count(*) from auth_codes c join users u on u.id=c.user_id
		where u.email='nora@example.com' and c.purpose='admin_invite'`) != 2 ||
		e.count(`select count(*) from auth_codes c join users u on u.id=c.user_id
		where u.email='nora@example.com' and c.purpose='admin_invite' and c.consumed_at is null`) != 1 {
		t.Error("want the old code consumed and one new live code")
	}
	fresh := e.inviteCode("nora@example.com")
	if old != fresh {
		if r := e.accept("nora@example.com", old, invitePW); r.Code != http.StatusUnprocessableEntity || r.errCode() != "auth.code_invalid" {
			t.Errorf("old code accepted: %d %s", r.Code, r.Body)
		}
	}
	if _, _, entity := e.lastAudit("admin.invitation_reissued"); entity != again.data()["id"] {
		t.Errorf("reissue audit entity = %s", entity)
	}
	if e.count(`select count(*) from outbox_messages where template='admin_invite'`) != 2 {
		t.Error("want a second admin_invite email")
	}
}

func TestCreateAdmin_TakesOverPendingDriver(t *testing.T) {
	e := newEnv(t)
	admin := e.newUser("root@example.com", "admin", "")
	reg := e.do("", http.MethodPost, "/v1/auth/register", map[string]any{
		"email": "dora@example.com", "password": password, "first_name": "Dora", "last_name": "Real",
		"phone": "+18095550000", "accepted_terms_version": "2026-11-16", "accepted_privacy_version": "2026-11-16",
	})
	if reg.Code != http.StatusAccepted {
		t.Fatalf("register: %d %s", reg.Code, reg.Body)
	}
	driverCode := e.text(`select payload->>'code' from outbox_messages where template='verify_email' and to_address='dora@example.com'`)

	r := e.invite(admin.Token, "dora@example.com")
	if r.Code != http.StatusCreated {
		t.Fatalf("status = %d %s, want 201", r.Code, r.Body)
	}
	if d := r.data(); d["first_name"] != "Nora" || d["last_name"] != "Gómez Díaz" || d["status"] != "pending_verification" {
		t.Errorf("data = %v", d)
	}
	row := e.text(`select role||'/'||status||'/'||coalesce(password_hash,'<null>')||'/'||coalesce(phone,'<null>')||'/'||
		coalesce(terms_version,'<null>')||'/'||coalesce(privacy_version,'<null>') from users where email='dora@example.com'`)
	if row != "admin/pending_verification/<null>/<null>/<null>/<null>" {
		t.Errorf("row = %q", row)
	}
	if e.count(`select count(*) from auth_codes c join users u on u.id=c.user_id
		where u.email='dora@example.com' and c.purpose='verify_email' and c.consumed_at is null`) != 0 {
		t.Error("driver verify code still live")
	}
	after, actor, _ := e.lastAudit("admin.created")
	if actor != admin.ID || !strings.Contains(after, `"replaced_pending_driver": true`) {
		t.Errorf("audit after=%s actor=%d", after, actor)
	}
	// The old driver credentials are dead.
	if r := e.do("", http.MethodPost, "/v1/auth/login",
		map[string]any{"email": "dora@example.com", "password": password, "client_kind": "web"}); r.Code != http.StatusUnauthorized {
		t.Errorf("old password login = %d %s", r.Code, r.Body)
	}
	if r := e.do("", http.MethodPost, "/v1/auth/verify",
		map[string]any{"email": "dora@example.com", "code": driverCode}); r.Code != http.StatusUnprocessableEntity {
		t.Errorf("old driver code verify = %d", r.Code)
	}
}

func TestCreateAdmin_ActiveOrSuspended409(t *testing.T) {
	e := newEnv(t)
	admin := e.newUser("root@example.com", "admin", "")
	e.driver("ana@example.com")
	e.driver("sus@example.com")
	e.exec(`update users set status='suspended' where email='sus@example.com'`)
	e.newUser("other@example.com", "admin", "")
	for _, email := range []string{"ana@example.com", "sus@example.com", "other@example.com", "root@example.com"} {
		r := e.invite(admin.Token, email)
		if r.Code != http.StatusConflict || r.errCode() != "user.email_taken" {
			t.Errorf("%s: %d %s, want 409 user.email_taken", email, r.Code, r.Body)
		}
	}
	if e.count(`select count(*) from outbox_messages where template='admin_invite'`) != 0 {
		t.Error("a conflict queued an invite")
	}
}

func TestCreateAdmin_RejectsBadInput(t *testing.T) {
	e := newEnv(t)
	admin := e.newUser("root@example.com", "admin", "")
	for name, body := range map[string]any{
		"blank first name": map[string]any{"email": "a@example.com", "first_name": "   ", "last_name": "X"},
		"bad email":        map[string]any{"email": "nope", "first_name": "A", "last_name": "X"},
		"bad locale":       map[string]any{"email": "a@example.com", "first_name": "A", "last_name": "X", "locale": "fr"},
	} {
		if r := e.do(admin.Token, http.MethodPost, adminsPath, body); r.Code != http.StatusUnprocessableEntity || r.errCode() != "validation.failed" {
			t.Errorf("%s: %d %s", name, r.Code, r.Body)
		}
	}
	for _, body := range []string{"", "null", "[]", "{}garbage"} {
		if r := e.do(admin.Token, http.MethodPost, adminsPath, body); r.Code != http.StatusBadRequest || r.errCode() != "http.invalid_json" {
			t.Errorf("body %q: %d %s", body, r.Code, r.Body)
		}
	}
	// A client-supplied role is ignored.
	r := e.do(admin.Token, http.MethodPost, adminsPath, map[string]any{
		"email": "ok@example.com", "first_name": "A", "last_name": "X", "role": "driver", "status": "active"})
	if r.Code != http.StatusCreated || r.data()["status"] != "pending_verification" {
		t.Errorf("extra fields: %d %s", r.Code, r.Body)
	}
	if e.text(`select role from users where email='ok@example.com'`) != "admin" {
		t.Error("role came from the client")
	}
}

func TestAcceptInvitation_ActivatesAdmin(t *testing.T) {
	e := newEnv(t)
	admin := e.newUser("root@example.com", "admin", "")
	e.invite(admin.Token, "nora@example.com")
	code := e.inviteCode("nora@example.com")

	if r := e.accept(" Nora@Example.com ", code, invitePW); r.Code != http.StatusNoContent || r.Body.Len() != 0 {
		t.Fatalf("accept = %d %s, want 204", r.Code, r.Body)
	}
	if got := e.text(`select status||'/'||(email_verified_at is not null)::text||'/'||(password_hash is not null)::text from users where email='nora@example.com'`); got != "active/true/true" {
		t.Errorf("row = %q", got)
	}
	lr := e.do("", http.MethodPost, "/v1/auth/login",
		map[string]any{"email": "nora@example.com", "password": invitePW, "client_kind": "web"})
	if lr.Code != http.StatusOK {
		t.Fatalf("login = %d %s", lr.Code, lr.Body)
	}
	user, _ := lr.data()["user"].(map[string]any)
	if user["role"] != "admin" || lr.data()["legal_acceptance_required"] != false {
		t.Errorf("login user = %v", lr.data())
	}
	tok := lr.data()["access_token"].(string)
	if r := e.do(tok, http.MethodGet, adminsPath, nil); r.Code != http.StatusOK {
		t.Errorf("admin list = %d %s", r.Code, r.Body)
	}
	if _, actor, _ := e.lastAudit("admin.invitation_accepted"); actor == 0 {
		t.Error("no admin.invitation_accepted audit")
	}
	// The code is single use.
	if r := e.accept("nora@example.com", code, invitePW); r.Code != http.StatusUnprocessableEntity || r.errCode() != "auth.code_invalid" {
		t.Errorf("reuse = %d %s", r.Code, r.Body)
	}
}

func TestAcceptInvitation_WrongCodeCountsAttempts(t *testing.T) {
	e := newEnv(t)
	admin := e.newUser("root@example.com", "admin", "")
	e.invite(admin.Token, "nora@example.com")
	code := e.inviteCode("nora@example.com")
	wrong := "000000"
	if code == wrong {
		wrong = "111111"
	}
	for range 5 {
		if r := e.accept("nora@example.com", wrong, invitePW); r.errCode() != "auth.code_invalid" {
			t.Fatalf("wrong code: %d %s", r.Code, r.Body)
		}
	}
	if r := e.accept("nora@example.com", code, invitePW); r.Code != http.StatusUnprocessableEntity || r.errCode() != "auth.code_invalid" {
		t.Errorf("after 5 attempts the right code still works: %d %s", r.Code, r.Body)
	}
	// Unknown emails answer the same way.
	if r := e.accept("ghost@example.com", code, invitePW); r.Code != http.StatusUnprocessableEntity || r.errCode() != "auth.code_invalid" {
		t.Errorf("unknown email: %d %s", r.Code, r.Body)
	}
}

func TestAcceptInvitation_Validation(t *testing.T) {
	e := newEnv(t)
	for name, body := range map[string]map[string]any{
		"short code":      {"email": "a@example.com", "code": "12345", "password": invitePW},
		"signed code":     {"email": "a@example.com", "code": "+12345", "password": invitePW},
		"short password":  {"email": "a@example.com", "code": "123456", "password": "short"},
		"common password": {"email": "a@example.com", "code": "123456", "password": "1234567890"},
	} {
		if r := e.do("", http.MethodPost, acceptPath, body); r.Code != http.StatusUnprocessableEntity || r.errCode() != "validation.failed" {
			t.Errorf("%s: %d %s", name, r.Code, r.Body)
		}
	}
}

func TestAcceptInvitation_RejectsDriverRows(t *testing.T) {
	e := newEnv(t)
	reg := e.do("", http.MethodPost, "/v1/auth/register", map[string]any{
		"email": "dora@example.com", "password": password, "first_name": "Dora", "last_name": "Real",
		"accepted_terms_version": "2026-11-16", "accepted_privacy_version": "2026-11-16",
	})
	if reg.Code != http.StatusAccepted {
		t.Fatalf("register: %d %s", reg.Code, reg.Body)
	}
	code := e.text(`select payload->>'code' from outbox_messages where template='verify_email' and to_address='dora@example.com'`)
	r := e.accept("dora@example.com", code, invitePW)
	if r.Code != http.StatusUnprocessableEntity || r.errCode() != "auth.code_invalid" {
		t.Fatalf("accept on a driver row = %d %s", r.Code, r.Body)
	}
	if got := e.text(`select status||'/'||role from users where email='dora@example.com'`); got != "pending_verification/driver" {
		t.Errorf("row = %q", got)
	}
}

// TestInviteeEscalationClosed is OC-38: no public flow can activate an invitee.
func TestInviteeEscalationClosed(t *testing.T) {
	e := newEnv(t)
	admin := e.newUser("root@example.com", "admin", "")
	e.invite(admin.Token, "nora@example.com")
	inviteCode := e.inviteCode("nora@example.com")
	codesBefore := e.count(`select count(*) from auth_codes`)
	mailBefore := e.count(`select count(*) from outbox_messages`)

	reg := e.do("", http.MethodPost, "/v1/auth/register", map[string]any{
		"email": "nora@example.com", "password": "Attacker-pass-99", "first_name": "Evil", "last_name": "Twin",
		"accepted_terms_version": "2026-11-16", "accepted_privacy_version": "2026-11-16",
	})
	other := e.do("", http.MethodPost, "/v1/auth/register", map[string]any{
		"email": "fresh@example.com", "password": "Attacker-pass-99", "first_name": "Evil", "last_name": "Twin",
		"accepted_terms_version": "2026-11-16", "accepted_privacy_version": "2026-11-16",
	})
	if reg.Code != http.StatusAccepted || reg.Code != other.Code || reg.data()["status"] != other.data()["status"] {
		t.Errorf("register differs: %d %s vs %d %s", reg.Code, reg.Body, other.Code, other.Body)
	}
	for _, c := range []string{inviteCode, "000000", "123456"} {
		if r := e.do("", http.MethodPost, "/v1/auth/verify", map[string]any{"email": "nora@example.com", "code": c}); r.Code != http.StatusUnprocessableEntity || r.errCode() != "auth.code_invalid" {
			t.Errorf("verify with %s: %d %s", c, r.Code, r.Body)
		}
	}
	if r := e.do("", http.MethodPost, "/v1/auth/verify/resend", map[string]any{"email": "nora@example.com"}); r.Code != http.StatusAccepted {
		t.Errorf("resend = %d", r.Code)
	}
	if r := e.do("", http.MethodPost, "/v1/auth/login",
		map[string]any{"email": "nora@example.com", "password": "Attacker-pass-99", "client_kind": "web"}); r.Code == http.StatusOK {
		t.Errorf("attacker logged in: %s", r.Body)
	}
	if got := e.text(`select role||'/'||status||'/'||coalesce(password_hash,'<null>')||'/'||first_name from users where email='nora@example.com'`); got != "admin/pending_verification/<null>/Nora" {
		t.Errorf("invitee row = %q", got)
	}
	// register(fresh) adds exactly one code and one email; nothing was issued for the invitee.
	if e.count(`select count(*) from auth_codes`) != codesBefore+1 || e.count(`select count(*) from outbox_messages`) != mailBefore+1 {
		t.Error("public flows issued codes or emails for the invitee")
	}
	if e.count(`select count(*) from auth_codes c join users u on u.id=c.user_id where u.email='nora@example.com' and c.purpose<>'admin_invite'`) != 0 {
		t.Error("driver codes exist for the invitee")
	}
	// The invitation itself still works.
	if r := e.accept("nora@example.com", inviteCode, invitePW); r.Code != http.StatusNoContent {
		t.Errorf("invite accept = %d %s", r.Code, r.Body)
	}
}

func TestListAdmins_KeysetPagination(t *testing.T) {
	e := newEnv(t)
	admin := e.newUser("a0@example.com", "admin", "")
	for i := 1; i < 5; i++ {
		e.clock.Advance(time.Minute)
		e.newUser(fmt.Sprintf("a%d@example.com", i), "admin", "")
	}
	e.driver("driver@example.com")
	// Two admins share a created_at: the id breaks the tie.
	e.exec(`update users set created_at = (select created_at from users where email='a3@example.com') where email='a2@example.com'`)

	seen := map[string]bool{}
	cursor := ""
	pages := 0
	for {
		path := adminsPath + "?limit=2"
		if cursor != "" {
			path += "&cursor=" + url.QueryEscape(cursor)
		}
		r := e.do(admin.Token, http.MethodGet, path, nil)
		if r.Code != http.StatusOK {
			t.Fatalf("page %d: %d %s", pages, r.Code, r.Body)
		}
		items, _ := r.json()["data"].([]any)
		page, _ := r.json()["meta"].(map[string]any)["page"].(map[string]any)
		if page["limit"] != float64(2) {
			t.Errorf("meta.page.limit = %v", page["limit"])
		}
		for _, it := range items {
			m := it.(map[string]any)
			if seen[m["email"].(string)] {
				t.Errorf("duplicate %v", m["email"])
			}
			seen[m["email"].(string)] = true
			if _, has := m["role"]; has {
				t.Errorf("AdminUser has role: %v", m)
			}
		}
		pages++
		next, _ := page["next_cursor"].(string)
		if next == "" {
			if page["next_cursor"] != nil {
				t.Errorf("last next_cursor = %v, want null", page["next_cursor"])
			}
			break
		}
		cursor = next
		if pages > 5 {
			t.Fatal("pagination does not end")
		}
	}
	if pages != 3 || len(seen) != 5 || seen["driver@example.com"] {
		t.Errorf("pages=%d seen=%v", pages, seen)
	}
	// Newest first.
	r := e.do(admin.Token, http.MethodGet, adminsPath, nil)
	items, _ := r.json()["data"].([]any)
	if len(items) != 5 {
		t.Fatalf("default page = %d items", len(items))
	}
	if page := r.json()["meta"].(map[string]any)["page"].(map[string]any); page["limit"] != float64(50) || page["next_cursor"] != nil {
		t.Errorf("default page meta = %v", page)
	}
}

func TestListAdmins_BadCursorAndLimit(t *testing.T) {
	e := newEnv(t)
	admin := e.newUser("a0@example.com", "admin", "")
	cases := []struct{ query, field string }{
		{"cursor=%25%25%25", "cursor"},
		{"cursor=e30", "cursor"}, // base64url of "{}"
		{"cursor=" + url.QueryEscape("bm90LWpzb24"), "cursor"},
		{"limit=0", "limit"},
		{"limit=101", "limit"},
		{"limit=1000", "limit"},
		{"limit=-1", "limit"},
		{"limit=abc", "limit"},
	}
	for _, c := range cases {
		r := e.do(admin.Token, http.MethodGet, adminsPath+"?"+c.query, nil)
		if r.Code != http.StatusUnprocessableEntity || r.errCode() != "validation.failed" {
			t.Errorf("%s: %d %s", c.query, r.Code, r.Body)
			continue
		}
		fields, _ := r.json()["error"].(map[string]any)["details"].(map[string]any)["fields"].([]any)
		if len(fields) != 1 || fields[0].(map[string]any)["field"] != c.field {
			t.Errorf("%s: fields = %v, want %s", c.query, fields, c.field)
		}
	}
	if r := e.do(admin.Token, http.MethodGet, adminsPath+"?limit=100", nil); r.Code != http.StatusOK {
		t.Errorf("limit=100: %d %s", r.Code, r.Body)
	}
}

// TestAdminRoutes_DriverGets403 is the M1 exit criterion: an admin reaches
// the admin area, a driver does not.
func TestAdminRoutes_DriverGets403(t *testing.T) {
	e := newEnv(t)
	d := e.driver("ana@example.com")
	a := e.newUser("root@example.com", "admin", "")
	for _, tc := range []struct{ method, body string }{{http.MethodGet, ""}, {http.MethodPost, `{"email":"x@example.com","first_name":"A","last_name":"B"}`}} {
		var body any
		if tc.body != "" {
			body = tc.body
		}
		if r := e.do(d.Token, tc.method, adminsPath, body); r.Code != http.StatusForbidden || r.errCode() != "auth.forbidden" {
			t.Errorf("driver %s: %d %s", tc.method, r.Code, r.Body)
		}
		if r := e.do("", tc.method, adminsPath, body); r.Code != http.StatusUnauthorized {
			t.Errorf("anonymous %s: %d", tc.method, r.Code)
		}
	}
	if r := e.do(a.Token, http.MethodGet, adminsPath, nil); r.Code != http.StatusOK {
		t.Errorf("admin GET: %d %s", r.Code, r.Body)
	}
	if e.count(`select count(*) from users where role='admin'`) != 1 {
		t.Error("a forbidden request created an admin")
	}
}

// wantActorKind asserts the actor_kind of the newest audit row of action.
func (e *env) wantActorKind(action, want string) {
	e.t.Helper()
	if got := e.text(`select actor_kind from audit_log where action = $1 order by id desc limit 1`, action); got != want {
		e.t.Errorf("%s actor_kind = %q, want %q", action, got, want)
	}
}

func TestAdminAudit_ActorKinds(t *testing.T) {
	e := newEnv(t)
	admin := e.newUser("root@example.com", "admin", "")
	e.invite(admin.Token, "nora@example.com")
	e.wantActorKind("admin.created", "admin")
	e.invite(admin.Token, "nora@example.com")
	e.wantActorKind("admin.invitation_reissued", "admin")
	if r := e.accept("nora@example.com", e.inviteCode("nora@example.com"), invitePW); r.Code != http.StatusNoContent {
		t.Fatalf("accept = %d %s", r.Code, r.Body)
	}
	e.wantActorKind("admin.invitation_accepted", "user")
	var id, nora int64
	if err := e.db.Super.QueryRow(t.Context(), `select actor_user_id, (select id from users where email='nora@example.com')
		from audit_log where action='admin.invitation_accepted'`).Scan(&id, &nora); err != nil || id != nora {
		t.Errorf("actor_user_id = %d, want invitee %d (err %v)", id, nora, err)
	}
}
