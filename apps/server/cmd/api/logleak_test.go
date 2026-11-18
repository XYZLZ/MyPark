package main

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/XYZLZ/MyPark/apps/server/internal/config"
	"github.com/XYZLZ/MyPark/apps/server/internal/devicefleet"
	"github.com/XYZLZ/MyPark/apps/server/internal/domain/authz"
	"github.com/XYZLZ/MyPark/apps/server/internal/domain/secret"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/gate"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/legal"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/notifications"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/email"
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
		"RATE_LIMIT_PEPPER": config.DevRateLimitPepper,
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
		"RATE_LIMIT_PEPPER": config.DevRateLimitPepper,
		"client IP":         clientIP,
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

// capturedMail is an email.Sender that keeps what it was given, so the test
// can scan the delivered messages and the outbox rows after they were sent.
type capturedMail struct {
	mu   sync.Mutex
	sent []email.Message
}

func (c *capturedMail) Send(_ context.Context, m email.Message) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sent = append(c.sent, m)
	return nil
}

// Spec 15.4 and M3B-12 for the reservation and payment surface. One run drives
// the production handler, the worker's M3 jobs, the mail dispatcher and the
// simulator's driver client with debug logging: card payments (success,
// decline, timeout and a malformed number), QR fetch and QR entry at the
// barrier, cancel and refund, the no-show job, the reminder and every email.
// None of the logs, error bodies, stored rows, delivered mail or simulator
// events holds a card number (with or without spaces), a CVC, a QR token,
// QR_HMAC_KEY, the fleet secret or a simulated driver's password.
func TestNoSecretsInLogs_M3(t *testing.T) {
	var logs testsupport.SyncBuffer
	log := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	start := weekdayStart.Add(2 * time.Minute) // 07:02 in the lot's zone
	r := newScenarioRig(t, start, func(d *deps) { d.log = log })
	r.withDrivers()
	r.start(simulator.ScenarioIdle, 42)
	var errBodies []string

	const cvc = "737"
	pans := []string{
		"4242424242424242", "4000000000000002", "4000000000009995", "4242424242424243", // the last one fails Luhn
	}
	spaced := func(pan string) string { return pan[0:4] + " " + pan[4:8] + " " + pan[8:12] + " " + pan[12:] }
	card := func(pan string) map[string]any {
		return map[string]any{"card_number": pan, "exp_month": 12, "exp_year": 2030, "cvc": cvc}
	}
	call := func(method, path, token, key string, body any) (int, map[string]any) {
		t.Helper()
		req := jsonReq(method, path, body)
		if key != "" {
			req.Header.Set("Idempotency-Key", key)
		}
		rec := r.serve(req, token)
		if rec.Code >= http.StatusBadRequest {
			errBodies = append(errBodies, rec.Body.String())
		}
		return rec.Code, decodeBody(t, rec)
	}
	login := func(n int, password string) (int, string) {
		code, body := call("POST", "/v1/auth/login", "", "", map[string]any{
			"email": devicefleet.SimDriverEmail(n), "password": password, "client_kind": "web"})
		tok, _ := field(body, "data.access_token").(string)
		return code, tok
	}
	reserve := func(n int, tok string, arrival time.Time) string {
		t.Helper()
		if err := r.drivers.Login(t.Context(), n); err != nil {
			t.Fatalf("driver client login %d: %v", n, err)
		}
		vehicle, err := r.drivers.Vehicle(t.Context(), n)
		if err != nil {
			t.Fatalf("vehicle %d: %v", n, err)
		}
		code, body := call("POST", "/v1/reservations", tok, "", map[string]any{
			"lot_id": r.publicID.String(), "vehicle_id": vehicle.String(), "arrival_at": arrival.Format(time.RFC3339)})
		if code != http.StatusCreated {
			t.Fatalf("reserve %d = %d %v", n, code, body)
		}
		return field(body, "data.id").(string)
	}
	qrOf := func(tok, id string) string {
		t.Helper()
		code, body := call("GET", "/v1/reservations/"+id+"/qr", tok, "", nil)
		token, _ := field(body, "data.token").(string)
		if code != http.StatusOK || len(token) != 86 {
			t.Fatalf("qr = %d %v", code, body)
		}
		return token
	}

	// A wrong password is an error body that must not echo the password.
	if code, _ := login(2, "wrong-"+devicefleet.SimDriverPassword(simFleet, devicefleet.SimDriverEmail(2)).Reveal()); code != http.StatusUnauthorized {
		t.Fatalf("wrong password login = %d, want 401", code)
	}

	// Driver 1 goes through the simulator's own client and its QR car.
	arrivalA := start.Truncate(5 * time.Minute).Add(20 * time.Minute) // 07:20, window opens 07:05
	if err := r.drivers.Login(t.Context(), 1); err != nil {
		t.Fatalf("login: %v", err)
	}
	vehicle, err := r.drivers.Vehicle(t.Context(), 1)
	if err != nil {
		t.Fatal(err)
	}
	rsvA, err := r.drivers.Create(t.Context(), 1, arrivalA, vehicle)
	if err != nil {
		t.Fatalf("create A: %v", err)
	}
	if status, err := r.drivers.Pay(t.Context(), 1, rsvA.ID, uuid.NewString()); err != nil || status != "confirmed" {
		t.Fatalf("pay A = %q, %v", status, err)
	}
	qrA, err := r.drivers.QR(t.Context(), 1, rsvA.ID)
	if err != nil {
		t.Fatalf("qr A: %v", err)
	}

	// Driver 2: a malformed number, a decline, a timeout that the reconcile job
	// confirms, then the QR and a cancel whose refund the refund job settles.
	code, tok2 := login(2, devicefleet.SimDriverPassword(simFleet, devicefleet.SimDriverEmail(2)).Reveal())
	if code != http.StatusOK {
		t.Fatalf("login 2 = %d", code)
	}
	idB := reserve(2, tok2, start.Add(48*time.Minute).Truncate(5*time.Minute))
	if code, _ := call("POST", "/v1/reservations/"+idB+"/pay", tok2, "key-leak-bad", card(spaced(pans[3]))); code != http.StatusUnprocessableEntity {
		t.Fatalf("malformed number = %d, want 422", code)
	}
	if code, _ := call("POST", "/v1/reservations/"+idB+"/pay", tok2, "key-leak-decl", card(spaced(pans[1]))); code != http.StatusPaymentRequired {
		t.Fatalf("decline = %d, want 402", code)
	}
	if code, _ := call("POST", "/v1/reservations/"+idB+"/pay", tok2, "key-leak-time", card(pans[2])); code != http.StatusAccepted {
		t.Fatalf("timeout = %d, want 202", code)
	}

	// Driver 3: a good card, a QR that nobody presents, so the reminder and the
	// no-show job both run for it.
	code, tok3 := login(3, devicefleet.SimDriverPassword(simFleet, devicefleet.SimDriverEmail(3)).Reveal())
	if code != http.StatusOK {
		t.Fatalf("login 3 = %d", code)
	}
	arrivalC := start.Add(88 * time.Minute).Truncate(5 * time.Minute)
	idC := reserve(3, tok3, arrivalC)
	if code, body := call("POST", "/v1/reservations/"+idC+"/pay", tok3, "key-leak-good", card(pans[0])); code != http.StatusOK {
		t.Fatalf("good card = %d %v", code, body)
	}
	qrC := qrOf(tok3, idC)

	// The reconcile tick confirms driver 2; the QR car of driver 1 enters.
	r.pace.runTo(t, arrivalA.Add(-15*time.Minute))
	if _, err := r.runner.Inject(simulator.InjectParams{Cred: simulator.CredReservationQR, QR: qrA, DwellMinutes: 30}); err != nil {
		t.Fatalf("inject: %v", err)
	}
	r.pace.runTo(t, arrivalA.Add(-10*time.Minute))
	qrB := qrOf(tok2, idB)
	if code, body := call("POST", "/v1/reservations/"+idB+"/cancel", tok2, "", nil); code != http.StatusOK {
		t.Fatalf("cancel B = %d %v", code, body)
	}
	// A QR of a cancelled reservation is refused.
	if code, _ := call("GET", "/v1/reservations/"+idB+"/qr", tok2, "", nil); code != http.StatusConflict {
		t.Fatalf("qr of a cancelled reservation = %d, want 409", code)
	}
	r.pace.runTo(t, arrivalA.Add(-5*time.Minute)) // the refund job settles it

	// The reminder window of driver 3, then past her window for the no-show.
	r.pace.runTo(t, arrivalC.Add(-25*time.Minute))
	if n, err := r.jobs.expiry.Reminders(t.Context(), r.clk.Now()); err != nil || n != 1 {
		t.Fatalf("Reminders = %d, %v; want 1", n, err)
	}
	r.pace.runTo(t, arrivalC.Add(time.Hour))
	r.drain()

	mail := &capturedMail{}
	dispatcher := notifications.NewDispatcher(r.d.pool, mail, r.clk, "noreply@mypark.test", log)
	for range 5 {
		if n, err := dispatcher.Tick(t.Context()); err != nil || n == 0 {
			break
		}
	}

	// The run must have covered the flows the check is about.
	for query, want := range map[string]int64{
		`select count(*) from payments where status = 'succeeded'`:                                          3, // A, B, C
		`select count(*) from payments where status = 'failed' and failure_code = 'card_declined'`:          1,
		`select count(*) from refunds where status = 'succeeded'`:                                           1,
		`select count(*) from sim_gateway_charges`:                                                          4, // A, decline, timeout, C
		`select count(*) from outbox_messages where status = 'sent' and template = 'reservation_reminder'`:  1,
		`select count(*) from outbox_messages where status = 'sent' and template = 'reservation_confirmed'`: 3,
		`select count(*) from reservations where status = 'expired_no_show'`:                                1,
	} {
		if got := r.count(query); got != want {
			t.Fatalf("%q = %d, want %d: the run would prove nothing", query, got, want)
		}
	}
	if open, _ := r.qrEntries(); open != 1 {
		t.Fatalf("QR entries open = %d, want 1", open)
	}
	if got := len(mail.sent); got < 6 {
		t.Fatalf("only %d emails were delivered", got)
	}
	output := logs.String()
	if !strings.Contains(output, "/v1/reservations") || strings.Count(output, "\n") < 100 {
		t.Fatalf("the log capture looks empty:\n%.2000s", output)
	}
	if len(errBodies) < 4 {
		t.Fatalf("only %d error bodies were captured", len(errBodies))
	}

	secrets := map[string]string{
		"SIMULATOR_FLEET_SECRET": config.DevSimulatorFleetSecret, "QR_HMAC_KEY": config.DevQRHMACKey,
		"JWT_SIGNING_KEY": config.DevJWTSigningKey,
		"QR token A":      qrA.Reveal(), "QR token B": qrB, "QR token C": qrC,
	}
	addBytes := func(label string, raw []byte) {
		secrets[label+" (hex)"] = hex.EncodeToString(raw)
		secrets[label+" (base64)"] = base64.StdEncoding.EncodeToString(raw)
		secrets[label+" (base64url)"] = base64.RawURLEncoding.EncodeToString(raw)
	}
	addBytes("fleet secret", simFleet)
	qrKey, err := base64.StdEncoding.DecodeString(config.DevQRHMACKey)
	if err != nil {
		t.Fatal(err)
	}
	addBytes("QR_HMAC_KEY", qrKey)
	for n := 1; n <= devicefleet.SimDriverCount; n++ {
		email := devicefleet.SimDriverEmail(n)
		secrets["derived password of driver "+email] = devicefleet.SimDriverPassword(simFleet, email).Reveal()
	}
	for _, pan := range pans {
		secrets["card number "+pan] = pan
		secrets["card number "+spaced(pan)] = spaced(pan)
	}
	// A CVC is three digits, so it only counts next to a cvc key or as a lone
	// JSON string: bare "737" also occurs inside uuids and timestamps.
	cvcPattern := regexp.MustCompile(`(?i)(cvc|cvv|security_?code)[^a-z0-9]{0,6}` + cvc + `|"` + cvc + `"`)

	rows := func(table string) string {
		t.Helper()
		var out string
		if err := r.db.Super.QueryRow(t.Context(),
			`select coalesce(string_agg(to_jsonb(x)::text, E'\n'), '') from `+table+` x`).Scan(&out); err != nil {
			t.Fatalf("read %s: %v", table, err)
		}
		return out
	}
	events, err := json.Marshal(r.log.After(0))
	if err != nil || len(events) < 1000 {
		t.Fatalf("simulator event log: %d bytes, %v; the capture looks empty", len(events), err)
	}
	var delivered strings.Builder
	for _, m := range mail.sent {
		delivered.WriteString(m.Subject + "\n" + m.Text + "\n" + m.HTML + "\n")
	}
	surfaces := map[string]string{
		"logs": output, "error bodies": strings.Join(errBodies, "\n"), "device error bodies": strings.Join(r.tap.errorsSeen(), "\n"),
		"payments": rows("payments"), "refunds": rows("refunds"), "sim_gateway_charges": rows("sim_gateway_charges"),
		"device_events": rows("device_events"), "outbox_messages": rows("outbox_messages"), "audit_log": rows("audit_log"),
		"reconciliation_alerts": rows("reconciliation_alerts"), "reservations": rows("reservations"),
		"simulator event log": string(events), "delivered emails": delivered.String(),
	}
	for surface, text := range surfaces {
		lower := strings.ToLower(text)
		for label, secretValue := range secrets {
			if strings.Contains(lower, strings.ToLower(secretValue)) {
				t.Errorf("%s contain the %s", surface, label)
			}
		}
		if cvcPattern.MatchString(text) {
			t.Errorf("%s contain a CVC", surface)
		}
	}
	// Queued mail keeps its token only until it is sent (design 11.2).
	var tokensLeft int
	if err := r.db.Super.QueryRow(t.Context(),
		`select count(*) from outbox_messages where status = 'sent' and payload ? 'qr_token'`).Scan(&tokensLeft); err != nil || tokensLeft != 0 {
		t.Errorf("sent outbox rows still holding a qr_token = %d, %v; want 0", tokensLeft, err)
	}
}
