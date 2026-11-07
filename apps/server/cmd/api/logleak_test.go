package main

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/XYZLZ/MyPark/apps/server/internal/config"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/legal"
	"github.com/XYZLZ/MyPark/apps/server/internal/testsupport"
)

// leakFlow drives requests through the handler and keeps every error body.
type leakFlow struct {
	t         *testing.T
	e         *apiEnv
	errBodies []string
	secrets   map[string]string // label -> value, matched case-insensitively
	codes     map[string]string // label -> 6-digit code, matched as a whole number
}

func (l *leakFlow) call(method, path, token string, body any) (*httptest.ResponseRecorder, map[string]any) {
	l.t.Helper()
	rec := l.e.serve(jsonReq(method, path, body), token)
	if rec.Code >= http.StatusBadRequest {
		l.errBodies = append(l.errBodies, rec.Body.String())
	}
	return rec, decodeBody(l.t, rec)
}

func (l *leakFlow) tokens(rec *httptest.ResponseRecorder, body map[string]any, label string) (access, refresh string) {
	l.t.Helper()
	if rec.Code != http.StatusOK {
		l.t.Fatalf("%s: %d %s", label, rec.Code, rec.Body)
	}
	data, _ := body["data"].(map[string]any)
	access, _ = data["access_token"].(string)
	refresh, _ = data["refresh_token"].(string)
	if access == "" || refresh == "" {
		l.t.Fatalf("%s: no tokens in %s", label, rec.Body)
	}
	l.secrets[label+" access token"] = access
	l.secrets[label+" refresh token"] = refresh
	return access, refresh
}

func (l *leakFlow) outboxCode(template, to string) string {
	l.t.Helper()
	var code string
	err := l.e.db.Super.QueryRow(l.t.Context(), `select payload->>'code' from outbox_messages
		where to_address = $1 and template = $2 order by id desc limit 1`, to, template).Scan(&code)
	if err != nil {
		l.t.Fatalf("outbox code %s: %v", template, err)
	}
	l.codes[template+" code"] = code
	return code
}

// Spec 15.4: secrets and personal data never reach the logs, the audit rows
// or an error body, across the whole account lifecycle.
func TestNoSecretsInLogs(t *testing.T) {
	var logs testsupport.SyncBuffer
	log := slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	e := newAPIEnv(t, func(d *deps) { d.log = log })
	l := &leakFlow{t: t, e: e, secrets: map[string]string{}, codes: map[string]string{}}

	const (
		email    = "zephyrine.quill.qf@example.test"
		local    = "zephyrine.quill.qf"
		first    = "Zephyrine"
		last     = "Quillfeather Ortega"
		plate    = "QZX7741"
		password = "Zx9!kelp-orbit-LEAK"
		newPass  = "Nq4#plum-harbor-LEAK2"
		weakPass = "unbelievable"
	)
	for label, v := range map[string]string{
		"email": email, "email local part": local, "first name": first, "last name": last,
		"plate": plate, "password": password, "new password": newPass, "weak password": weakPass,
		"JWT_SIGNING_KEY": config.DevJWTSigningKey, "AUTH_CODE_PEPPER": config.DevAuthCodePepper,
	} {
		l.secrets[label] = v
	}

	register := map[string]any{
		"email": " Zephyrine.Quill.QF@Example.TEST ", "password": password, "first_name": first, "last_name": last,
		"accepted_terms_version": legal.InitialVersion, "accepted_privacy_version": legal.InitialVersion,
	}
	// A rejected password must not be echoed back in the validation error.
	weak := map[string]any{}
	for k, v := range register {
		weak[k] = v
	}
	weak["password"] = weakPass
	if rec, _ := l.call("POST", "/v1/auth/register", "", weak); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("weak register = %d %s, want 422", rec.Code, rec.Body)
	}

	if rec, _ := l.call("POST", "/v1/auth/register", "", register); rec.Code != http.StatusAccepted {
		t.Fatalf("register = %d %s", rec.Code, rec.Body)
	}
	code := l.outboxCode("verify_email", email)
	if rec, _ := l.call("POST", "/v1/auth/verify", "", map[string]any{"email": email, "code": "000000"}); rec.Code < 400 {
		t.Fatalf("wrong code accepted: %d", rec.Code)
	}
	if rec, _ := l.call("POST", "/v1/auth/verify", "", map[string]any{"email": email, "code": code}); rec.Code != http.StatusNoContent {
		t.Fatalf("verify = %d %s", rec.Code, rec.Body)
	}

	login := func(pw, kind string) (*httptest.ResponseRecorder, map[string]any) {
		return l.call("POST", "/v1/auth/login", "", map[string]any{"email": email, "password": pw, "client_kind": kind})
	}
	if rec, _ := login("wrong-"+password, "web"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong password login = %d", rec.Code)
	}
	rec, body := login(password, "web")
	access1, refresh1 := l.tokens(rec, body, "login")

	vehicle := map[string]any{"plate": plate, "make": "Toyota", "model": "Corolla", "color": "Blue"}
	if rec, _ := l.call("POST", "/v1/vehicles", access1, vehicle); rec.Code != http.StatusCreated {
		t.Fatalf("create vehicle = %d %s", rec.Code, rec.Body)
	}
	if rec, _ := l.call("POST", "/v1/vehicles", access1, vehicle); rec.Code != http.StatusConflict {
		t.Fatalf("duplicate plate = %d %s, want 409", rec.Code, rec.Body)
	}

	rec, body = l.call("POST", "/v1/auth/refresh", "", map[string]any{"refresh_token": refresh1})
	l.tokens(rec, body, "refresh")
	if rec, _ := l.call("POST", "/v1/auth/refresh", "", map[string]any{"refresh_token": refresh1}); rec.Code != http.StatusUnauthorized {
		t.Fatalf("reuse = %d %s, want 401", rec.Code, rec.Body)
	}
	if rec, _ := l.call("GET", "/v1/vehicles", access1, nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("revoked session = %d, want 401", rec.Code)
	}

	if rec, _ := l.call("POST", "/v1/auth/password/forgot", "", map[string]any{"email": email}); rec.Code != http.StatusAccepted {
		t.Fatalf("forgot = %d %s", rec.Code, rec.Body)
	}
	resetCode := l.outboxCode("password_reset", email)
	if rec, _ := l.call("POST", "/v1/auth/password/reset", "", map[string]any{
		"email": email, "code": resetCode, "new_password": newPass}); rec.Code != http.StatusNoContent {
		t.Fatalf("reset = %d %s", rec.Code, rec.Body)
	}
	if rec, _ := login(password, "mobile"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("old password login = %d, want 401", rec.Code)
	}
	rec, body = login(newPass, "mobile")
	access4, _ := l.tokens(rec, body, "post-reset login")

	if rec, _ := l.call("DELETE", "/v1/me", access4, map[string]any{"password": password}); rec.Code < 400 {
		t.Fatalf("delete with the old password = %d, want an error", rec.Code)
	}
	if rec, _ := l.call("DELETE", "/v1/me", access4, map[string]any{"password": newPass}); rec.Code != http.StatusNoContent {
		t.Fatalf("delete account = %d %s", rec.Code, rec.Body)
	}

	var audit string
	err := e.db.Super.QueryRow(t.Context(),
		"select coalesce(jsonb_agg(to_jsonb(a)), '[]'::jsonb)::text from audit_log a").Scan(&audit)
	if err != nil {
		t.Fatalf("read audit_log: %v", err)
	}

	output := logs.String()
	if !strings.Contains(output, "/v1/auth/login") || strings.Count(output, "\n") < 10 {
		t.Fatalf("the capture looks empty, so the check would prove nothing:\n%s", output)
	}
	if len(audit) < 100 {
		t.Fatalf("audit_log looks empty: %s", audit)
	}
	if len(l.errBodies) < 5 {
		t.Fatalf("only %d error bodies were captured", len(l.errBodies))
	}

	surfaces := map[string]string{"logs": output, "audit_log": audit, "error bodies": strings.Join(l.errBodies, "\n")}
	for surface, text := range surfaces {
		lower := strings.ToLower(text)
		for label, secret := range l.secrets {
			if strings.Contains(lower, strings.ToLower(secret)) {
				t.Errorf("%s contain the %s", surface, label)
			}
		}
		for label, code := range l.codes {
			if regexp.MustCompile(`(^|[^0-9])` + code + `([^0-9]|$)`).MatchString(text) {
				t.Errorf("%s contain the %s", surface, label)
			}
		}
		if strings.Contains(text, "$argon2") {
			t.Errorf("%s contain a password hash", surface)
		}
	}
}
