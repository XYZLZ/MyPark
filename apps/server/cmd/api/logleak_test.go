package main

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/XYZLZ/MyPark/apps/server/internal/config"
	"github.com/XYZLZ/MyPark/apps/server/internal/devicefleet"
	"github.com/XYZLZ/MyPark/apps/server/internal/domain/authz"
	"github.com/XYZLZ/MyPark/apps/server/internal/domain/secret"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/gate"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/legal"
	"github.com/XYZLZ/MyPark/apps/server/internal/simulator"
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

// Spec 15.4 for the device surface: one virtual hour of weekday traffic, a
// lost ticket and employee cards, driven through the real handler and the
// simulator's control API with debug logging. Neither the logs, nor any error
// body, nor a stored device event holds a device or fleet secret, a sealing
// key, the card pepper, a card UID, a ticket code, the control token or the
// client IP of a rejected device.
func TestNoSecretsInLogs_Devices(t *testing.T) {
	var logs testsupport.SyncBuffer
	log := slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	r := newScenarioRig(t, weekdayStart, func(d *deps) { d.log = log })
	token := config.DevSimulatorControlToken
	ctl := simulator.NewControl(r.runner, r.log, secret.String(token), log)
	var errBodies []string

	control := func(method, path, body, tok string) (int, map[string]any) {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set(simulator.ControlTokenHeader, tok)
		rec := httptest.NewRecorder()
		ctl.ServeHTTP(rec, req)
		if rec.Code >= http.StatusBadRequest {
			errBodies = append(errBodies, rec.Body.String())
		}
		return rec.Code, decodeBody(t, rec)
	}

	if code, _ := control("GET", "/control/status", "", "wrong-"+token); code != http.StatusUnauthorized {
		t.Fatalf("wrong control token = %d, want 401", code)
	}
	startBody := `{"scenario":"weekday","seed":42,"start_at":"` + weekdayStart.Format(time.RFC3339) + `"}`
	if code, body := control("POST", "/control/scenarios/start", startBody, token); code != http.StatusOK && code != http.StatusAccepted {
		t.Fatalf("start = %d %v", code, body)
	}
	r.pace.runTo(t, weekdayStart.Add(30*time.Minute))
	code, body := control("POST", "/control/cars", `{"credential":"ticket","hold":true}`, token)
	if code != http.StatusAccepted {
		t.Fatalf("inject = %d %v", code, body)
	}
	lost, _ := body["data"].(map[string]any)["car_id"].(string)
	r.pace.runTo(t, weekdayStart.Add(40*time.Minute))
	if code, body := control("POST", "/control/cars/"+lost+"/leave", `{"lose_ticket":true}`, token); code != http.StatusAccepted {
		t.Fatalf("leave lost = %d %v", code, body)
	}
	r.pace.runTo(t, weekdayStart.Add(time.Hour))
	if code, _ := control("GET", "/control/events", "", token); code != http.StatusOK {
		t.Fatalf("events = %d", code)
	}
	control("POST", "/control/cars/"+lost+"/leave", `{}`, token) // a car that is gone: an error body

	// A rejected device: a bad signature from a client address that must not be logged.
	const clientIP = "198.51.100.77"
	bad := httptest.NewRequest("POST", deviceEventsPath, strings.NewReader(`{}`))
	bad.RemoteAddr = clientIP + ":4711"
	bad.Header.Set("X-Device-Id", devicefleet.ByKind(authz.DevicePayStation).PublicID.String())
	bad.Header.Set("X-Timestamp", r.clk.Now().Format(time.RFC3339))
	bad.Header.Set("X-Signature", strings.Repeat("0", 64))
	rec := r.serve(bad, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("bad device signature = %d %s, want 401", rec.Code, rec.Body)
	}
	errBodies = append(errBodies, rec.Body.String())
	unknown := httptest.NewRequest("POST", deviceEventsPath, strings.NewReader(`{}`))
	unknown.RemoteAddr = clientIP + ":4712"
	unknown.Header = bad.Header.Clone()
	unknown.Header.Set("X-Device-Id", uuid.NewString())
	rec = r.serve(unknown, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unknown device = %d %s, want 401", rec.Code, rec.Body)
	}
	errBodies = append(errBodies, rec.Body.String())

	// The run must have covered the flows the check is about.
	for _, want := range []string{
		"entry.credential_presented open", "paystation.lost_ticket ack", "paystation.cash_paid ack", "exit.credential_presented open",
	} {
		if r.tap.n(want) == 0 {
			t.Fatalf("the run never produced %q, so the check would prove nothing: %v", want, r.tap.count)
		}
	}
	if len(r.tap.cards) == 0 || len(r.tap.codes) == 0 {
		t.Fatalf("no card UIDs (%d) or ticket codes (%d) were seen", len(r.tap.cards), len(r.tap.codes))
	}

	secrets := map[string]string{
		"SIMULATOR_CONTROL_TOKEN": token, "SIMULATOR_FLEET_SECRET": config.DevSimulatorFleetSecret,
		"SECRETBOX_KEY": config.DevSecretboxKey, "CARD_UID_PEPPER": config.DevCardUIDPepper,
		"JWT_SIGNING_KEY": config.DevJWTSigningKey, "AUTH_CODE_PEPPER": config.DevAuthCodePepper,
		"client IP": clientIP,
	}
	addBytes := func(label string, raw []byte) {
		secrets[label+" (hex)"] = hex.EncodeToString(raw)
		secrets[label+" (base64)"] = base64.StdEncoding.EncodeToString(raw)
		secrets[label+" (base64url)"] = base64.RawURLEncoding.EncodeToString(raw)
	}
	addBytes("test sealer key", testsupport.TestSealerKey.Secret[:])
	addBytes("fleet secret", simFleet)
	for _, d := range devicefleet.Devices() {
		addBytes("secret of "+d.Name, devicefleet.DeviceSecret(simFleet, d.PublicID).Reveal())
	}
	for _, c := range devicefleet.Cards() {
		uid := devicefleet.CardUID(simFleet, c.Code)
		secrets["card UID of "+c.Code] = uid
		secrets["card UID of "+c.Code+" (colons)"] = colonHex(uid)
	}
	for card := range r.tap.cards {
		secrets["card value "+card] = card
		// An unkeyed hash of a short UID is enumerable: none may be stored.
		for _, form := range []string{card, strings.ToUpper(strings.ReplaceAll(card, ":", ""))} {
			sum := sha256.Sum256([]byte(form))
			secrets["unkeyed sha256 of card "+form] = hex.EncodeToString(sum[:])[:12]
		}
	}
	for code := range r.tap.codes {
		secrets["ticket code "+code] = code
	}

	var events string
	if err := r.db.Super.QueryRow(t.Context(),
		`select coalesce(string_agg(payload::text || ' ' || response::text, E'\n'), '') from device_events`).Scan(&events); err != nil {
		t.Fatalf("read device_events: %v", err)
	}
	var rows int
	if err := r.db.Super.QueryRow(t.Context(), `select count(*) from device_events`).Scan(&rows); err != nil || rows < 50 {
		t.Fatalf("device_events rows = %d, %v; the capture looks empty", rows, err)
	}
	var audit string
	if err := r.db.Super.QueryRow(t.Context(),
		"select coalesce(jsonb_agg(to_jsonb(a)), '[]'::jsonb)::text from audit_log a").Scan(&audit); err != nil {
		t.Fatalf("read audit_log: %v", err)
	}
	var auditRows int
	if err := r.db.Super.QueryRow(t.Context(), `select count(*) from audit_log`).Scan(&auditRows); err != nil {
		t.Fatalf("count audit_log: %v", err)
	}
	if auditRows < 1 {
		t.Fatal("audit_log is empty, so scanning it would prove nothing")
	}
	output := logs.String()
	if !strings.Contains(output, deviceEventsPath) || strings.Count(output, "\n") < 50 {
		t.Fatalf("the log capture looks empty, so the check would prove nothing:\n%.2000s", output)
	}
	errBodies = append(errBodies, r.tap.errors...)
	if len(errBodies) < 3 {
		t.Fatalf("only %d error bodies were captured", len(errBodies))
	}

	// Card values are stored keyed by CARD_UID_PEPPER, as the prefix of their
	// card_uid_hash: proves the pepper is wired into the redaction.
	pepper, _ := base64.StdEncoding.DecodeString(config.DevCardUIDPepper)
	keyed := 0
	for card := range r.tap.cards {
		if strings.Contains(events, "hmac:"+hex.EncodeToString(gate.HashCardUID(pepper, card))[:12]) {
			keyed++
		}
	}
	if keyed == 0 {
		t.Error("no card value is stored as its pepper-keyed hash")
	}

	surfaces := map[string]string{
		"logs": output, "device_events": events, "audit_log": audit, "error bodies": strings.Join(errBodies, "\n"),
	}
	for surface, text := range surfaces {
		lower := strings.ToLower(text)
		for label, secretValue := range secrets {
			if strings.Contains(lower, strings.ToLower(secretValue)) {
				t.Errorf("%s contain the %s", surface, label)
			}
		}
	}
}

// colonHex renders a hex string as the byte pairs a card reader sends.
func colonHex(h string) string {
	var parts []string
	for i := 0; i+2 <= len(h); i += 2 {
		parts = append(parts, h[i:i+2])
	}
	return strings.Join(parts, ":")
}
