package devices_test

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/XYZLZ/MyPark/apps/server/internal/devicesig"
	"github.com/XYZLZ/MyPark/apps/server/internal/domain/authz"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/devices"
	devpg "github.com/XYZLZ/MyPark/apps/server/internal/features/devices/postgres"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/ratelimit"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/secretbox"
	"github.com/XYZLZ/MyPark/apps/server/internal/testsupport"
	"github.com/XYZLZ/MyPark/apps/server/internal/transport/http/middleware"
	"github.com/XYZLZ/MyPark/apps/server/internal/transport/http/reqctx"
	"github.com/XYZLZ/MyPark/apps/server/internal/transport/http/router"
)

const eventsURI = "/v1/devices/events"

var (
	t0   = time.Date(2026, 11, 24, 14, 3, 11, 0, time.UTC)
	body = []byte(`{"event_id":"019a6d00-0000-7000-8000-0000000000aa","type":"heartbeat","occurred_at":"2026-11-24T14:03:10Z","payload":{}}`)
)

// fakeRegistry is an in-memory devices.Registry that counts lookups.
type fakeRegistry struct {
	mu       sync.Mutex
	recs     map[uuid.UUID]devices.Record
	lookups  int
	touches  []time.Time
	touchErr error
}

func (f *fakeRegistry) DeviceByPublicID(_ context.Context, id uuid.UUID) (devices.Record, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lookups++
	rec, ok := f.recs[id]
	if !ok {
		return devices.Record{}, devices.ErrDeviceNotFound
	}
	return rec, nil
}

func (f *fakeRegistry) TouchLastSeen(_ context.Context, _ int64, now time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.touches = append(f.touches, now)
	return f.touchErr
}

// addDevice registers a device with a random secret sealed by s.
func (f *fakeRegistry) addDevice(t *testing.T, s *secretbox.Sealer, kind authz.DeviceKind, revoked bool) testsupport.TestDevice {
	t.Helper()
	d := testsupport.TestDevice{ID: int64(len(f.recs) + 1), LotID: 1, PublicID: uuid.New(), Kind: kind, Secret: bytes.Repeat([]byte{byte(len(f.recs) + 1)}, 32)}
	sealed, keyID, err := s.Seal(d.Secret)
	if err != nil {
		t.Fatal(err)
	}
	if f.recs == nil {
		f.recs = map[uuid.UUID]devices.Record{}
	}
	f.recs[d.PublicID] = devices.Record{ID: d.ID, LotID: d.LotID, PublicID: d.PublicID, Kind: kind, Revoked: revoked, SecretCiphertext: sealed, SecretKeyID: keyID}
	return d
}

// seen is what the wrapped handler observed.
type seen struct {
	called   bool
	device   authz.Device
	hasDev   bool
	hasUser  bool
	logDevID string
	body     []byte
}

type fixture struct {
	auth  *devices.Authenticator
	clock *testsupport.FakeClock
	logs  *testsupport.SyncBuffer
	seen  *seen
}

func newFixture(t *testing.T, reg devices.Registry, s *secretbox.Sealer) *fixture {
	t.Helper()
	f := &fixture{clock: testsupport.NewFakeClock(t0), logs: &testsupport.SyncBuffer{}, seen: &seen{}}
	f.auth = devices.NewAuthenticator(reg, s, f.clock, slog.New(slog.NewJSONHandler(f.logs, nil)))
	return f
}

func (f *fixture) handler() http.Handler {
	return f.auth.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.seen.called = true
		f.seen.device, f.seen.hasDev = authz.DeviceFrom(r.Context())
		_, f.seen.hasUser = authz.PrincipalFrom(r.Context())
		f.seen.logDevID = reqctx.Fields(r.Context()).DeviceID
		f.seen.body, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
}

// serve runs req through the middleware with a request id and log fields, as
// the global chain would.
func (f *fixture) serve(req *http.Request) *httptest.ResponseRecorder {
	*f.seen = seen{}
	ctx, _ := reqctx.WithLogFields(reqctx.WithRequestID(req.Context(), uuid.NewString()))
	rec := httptest.NewRecorder()
	f.handler().ServeHTTP(rec, req.WithContext(ctx))
	return rec
}

func errorCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var env struct {
		Error struct{ Code string } `json:"error"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &env)
	return env.Error.Code
}

func assertRefused(t *testing.T, f *fixture, rec *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if rec.Code != status || errorCode(t, rec) != code {
		t.Errorf("response = %d %s, want %d %s", rec.Code, rec.Body, status, code)
	}
	if f.seen.called {
		t.Error("the handler ran for a refused request")
	}
}

// withoutRequestID returns the envelope with meta.request_id removed.
func withoutRequestID(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatal(err)
	}
	delete(m["meta"].(map[string]any), "request_id")
	out, _ := json.Marshal(m)
	return string(out)
}

func TestDeviceAuth_Valid(t *testing.T) {
	db := testsupport.NewTestDB(t)
	lotID := testsupport.SeedPilotLot(t, db)
	s := testsupport.TestSealer(t)
	dev := testsupport.NewTestDevice(t, db, s, lotID, authz.DevicePayStation)
	f := newFixture(t, devpg.NewRepo(db.App, s), s)

	rec := f.serve(dev.Request(t, eventsURI+"?trace=1", body, t0))
	if rec.Code != http.StatusOK || !f.seen.called {
		t.Fatalf("status = %d %s", rec.Code, rec.Body)
	}
	want := authz.Device{ID: dev.ID, PublicID: dev.PublicID, LotID: lotID, Kind: authz.DevicePayStation}
	if !f.seen.hasDev || f.seen.device != want {
		t.Errorf("DeviceFrom = %+v, %v; want %+v", f.seen.device, f.seen.hasDev, want)
	}
	if f.seen.hasUser {
		t.Error("a device request carries a user principal")
	}
	if f.seen.logDevID != dev.PublicID.String() {
		t.Errorf("LogFields.DeviceID = %q", f.seen.logDevID)
	}
	if !bytes.Equal(f.seen.body, body) {
		t.Errorf("handler body = %q, want the full signed body", f.seen.body)
	}
}

func TestDeviceAuth_MissingOrMalformedHeaders(t *testing.T) {
	s := testsupport.TestSealer(t)
	reg := &fakeRegistry{}
	dev := reg.addDevice(t, s, authz.DevicePodium, false)
	f := newFixture(t, reg, s)
	sig := func(r *http.Request) string { return r.Header.Get("X-Signature") }

	tests := []struct {
		name string
		mod  func(r *http.Request)
	}{
		{"no X-Device-Id", func(r *http.Request) { r.Header.Del("X-Device-Id") }},
		{"no X-Timestamp", func(r *http.Request) { r.Header.Del("X-Timestamp") }},
		{"no X-Signature", func(r *http.Request) { r.Header.Del("X-Signature") }},
		{"signature 63 hex", func(r *http.Request) { r.Header.Set("X-Signature", sig(r)[:63]) }},
		{"signature uppercase", func(r *http.Request) { r.Header.Set("X-Signature", strings.ToUpper(sig(r))) }},
		{"non-uuid id", func(r *http.Request) { r.Header.Set("X-Device-Id", "pay-station-1") }},
		{"urn uuid id", func(r *http.Request) { r.Header.Set("X-Device-Id", "urn:uuid:"+dev.PublicID.String()) }},
		{"fractional timestamp", func(r *http.Request) { r.Header.Set("X-Timestamp", "2026-11-24T14:03:11.000Z") }},
		{"offset timestamp", func(r *http.Request) { r.Header.Set("X-Timestamp", "2026-11-24T10:03:11-04:00") }},
		{"user bearer only", func(r *http.Request) {
			r.Header = http.Header{"Authorization": {"Bearer eyJhbGciOiJFZERTQSJ9.e30.sig"}}
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := dev.Request(t, eventsURI, body, t0)
			tc.mod(req)
			rec := f.serve(req)
			assertRefused(t, f, rec, http.StatusUnauthorized, "device.unauthenticated")
			if f.seen.hasUser {
				t.Error("bearer produced a principal")
			}
		})
	}
	if reg.lookups != 0 {
		t.Errorf("registry looked up %d times for malformed headers", reg.lookups)
	}
}

func TestDeviceAuth_WindowBoundaries(t *testing.T) {
	s := testsupport.TestSealer(t)
	reg := &fakeRegistry{}
	dev := reg.addDevice(t, s, authz.DevicePodium, false)
	f := newFixture(t, reg, s)

	for _, skew := range []time.Duration{-60 * time.Second, 60 * time.Second} {
		if rec := f.serve(dev.Request(t, eventsURI, body, t0.Add(skew))); rec.Code != http.StatusOK {
			t.Errorf("skew %v: status = %d %s, want 200", skew, rec.Code, rec.Body)
		}
	}
	before := reg.lookups
	for _, skew := range []time.Duration{-61 * time.Second, 61 * time.Second} {
		rec := f.serve(dev.Request(t, eventsURI, body, t0.Add(skew)))
		assertRefused(t, f, rec, http.StatusUnauthorized, "device.timestamp_out_of_window")
	}
	if reg.lookups != before {
		t.Errorf("registry called %d times for out-of-window requests", reg.lookups-before)
	}
}

func TestDeviceAuth_UnknownAndRevokedIdenticalBodies(t *testing.T) {
	s := testsupport.TestSealer(t)
	reg := &fakeRegistry{}
	revoked := reg.addDevice(t, s, authz.DeviceDispenser, true)
	f := newFixture(t, reg, s)
	var dummyMACs int
	devices.SetVerifyHook(f.auth, func(secret, canonical []byte, sig string) bool {
		if bytes.Equal(secret, devices.DummyKey()) {
			dummyMACs++
		}
		return devicesig.Verify(secret, canonical, sig)
	})

	unknown := testsupport.TestDevice{PublicID: uuid.New(), Secret: bytes.Repeat([]byte{9}, 32)}
	recUnknown := f.serve(unknown.Request(t, eventsURI, body, t0))
	assertRefused(t, f, recUnknown, http.StatusUnauthorized, "device.unauthenticated")
	// The revoked device signs with its real secret and is still refused.
	recRevoked := f.serve(revoked.Request(t, eventsURI, body, t0))
	assertRefused(t, f, recRevoked, http.StatusUnauthorized, "device.unauthenticated")

	if a, b := withoutRequestID(t, recUnknown), withoutRequestID(t, recRevoked); a != b {
		t.Errorf("bodies differ:\nunknown %s\nrevoked %s", a, b)
	}
	if dummyMACs != 2 {
		t.Errorf("dummy-key HMACs = %d, want 2 (equal work for unknown and revoked)", dummyMACs)
	}

	// A request signed with the dummy key itself never authenticates.
	forged := testsupport.TestDevice{PublicID: uuid.New(), Secret: devices.DummyKey()}
	assertRefused(t, f, f.serve(forged.Request(t, eventsURI, body, t0)), http.StatusUnauthorized, "device.unauthenticated")
}

func TestDeviceAuth_TamperedQueryOrBody(t *testing.T) {
	s := testsupport.TestSealer(t)
	reg := &fakeRegistry{}
	dev := reg.addDevice(t, s, authz.DevicePodium, false)
	f := newFixture(t, reg, s)

	signed := dev.Request(t, eventsURI, body, t0)
	req := httptest.NewRequest(http.MethodPost, eventsURI+"?x=1", bytes.NewReader(body))
	req.Header = signed.Header.Clone()
	assertRefused(t, f, f.serve(req), http.StatusUnauthorized, "device.unauthenticated")

	flipped := bytes.Clone(body)
	flipped[len(flipped)-2] ^= 1
	req = httptest.NewRequest(http.MethodPost, eventsURI, bytes.NewReader(flipped))
	req.Header = signed.Header.Clone()
	assertRefused(t, f, f.serve(req), http.StatusUnauthorized, "device.unauthenticated")
}

func TestDeviceAuth_BodyCap(t *testing.T) {
	s := testsupport.TestSealer(t)
	reg := &fakeRegistry{}
	dev := reg.addDevice(t, s, authz.DevicePodium, false)
	f := newFixture(t, reg, s)

	if rec := f.serve(dev.Request(t, eventsURI, bytes.Repeat([]byte{' '}, devices.MaxEventBytes), t0)); rec.Code != http.StatusOK {
		t.Errorf("exactly 16 KiB: status = %d %s, want 200", rec.Code, rec.Body)
	}
	if len(f.seen.body) != devices.MaxEventBytes {
		t.Errorf("handler read %d bytes, want %d", len(f.seen.body), devices.MaxEventBytes)
	}
	rec := f.serve(dev.Request(t, eventsURI, bytes.Repeat([]byte{' '}, devices.MaxEventBytes+1), t0))
	assertRefused(t, f, rec, http.StatusRequestEntityTooLarge, "http.payload_too_large")
}

func TestDeviceAuth_TouchLastSeenThrottled(t *testing.T) {
	db := testsupport.NewTestDB(t)
	lotID := testsupport.SeedPilotLot(t, db)
	s := testsupport.TestSealer(t)
	dev := testsupport.NewTestDevice(t, db, s, lotID, authz.DeviceSensorGateway)
	f := newFixture(t, devpg.NewRepo(db.App, s), s)
	lastSeen := func() time.Time {
		var ts time.Time
		if err := db.App.QueryRow(t.Context(), "select last_seen_at from devices where id = $1", dev.ID).Scan(&ts); err != nil {
			t.Fatal(err)
		}
		return ts
	}

	for _, step := range []struct{ at, want time.Duration }{
		{0, 0}, {10 * time.Second, 0}, {31 * time.Second, 31 * time.Second},
	} {
		f.clock.Set(t0.Add(step.at))
		if rec := f.serve(dev.Request(t, eventsURI, body, t0.Add(step.at))); rec.Code != http.StatusOK {
			t.Fatalf("+%v: status = %d %s", step.at, rec.Code, rec.Body)
		}
		if got := lastSeen(); !got.Equal(t0.Add(step.want)) {
			t.Errorf("after request at +%v last_seen_at = %v, want +%v", step.at, got, step.want)
		}
	}
}

func TestDeviceAuth_TouchFailureIsBestEffort(t *testing.T) {
	s := testsupport.TestSealer(t)
	reg := &fakeRegistry{touchErr: errors.New("db down")}
	dev := reg.addDevice(t, s, authz.DevicePodium, false)
	f := newFixture(t, reg, s)
	if rec := f.serve(dev.Request(t, eventsURI, body, t0)); rec.Code != http.StatusOK || len(reg.touches) != 1 {
		t.Errorf("status = %d, touches = %d; want 200 after one failed touch", rec.Code, len(reg.touches))
	}
	if !strings.Contains(f.logs.String(), "last seen") {
		t.Errorf("touch failure not logged: %s", f.logs)
	}
}

func TestDeviceAuth_UnopenableSecretIs500(t *testing.T) {
	s := testsupport.TestSealer(t)
	reg := &fakeRegistry{}
	dev := reg.addDevice(t, s, authz.DevicePodium, false)
	rec := reg.recs[dev.PublicID]
	rec.SecretKeyID = "retired"
	reg.recs[dev.PublicID] = rec
	f := newFixture(t, reg, s)

	assertRefused(t, f, f.serve(dev.Request(t, eventsURI, body, t0)), http.StatusInternalServerError, "internal.unexpected")
	if strings.Contains(f.logs.String(), hex.EncodeToString(dev.Secret)) {
		t.Error("log leaks the device secret")
	}
}

func TestDeviceAuth_LogsReasonNotSecret(t *testing.T) {
	s := testsupport.TestSealer(t)
	reg := &fakeRegistry{}
	dev := reg.addDevice(t, s, authz.DevicePodium, false)
	revoked := reg.addDevice(t, s, authz.DevicePodium, true)
	unknown := testsupport.TestDevice{PublicID: uuid.New(), Secret: bytes.Repeat([]byte{9}, 32)}
	var all strings.Builder

	badSig := dev.Request(t, eventsURI, body, t0)
	expected := badSig.Header.Get("X-Signature")
	badSig.Header.Set("X-Signature", strings.Repeat("0", 64))
	noSig := dev.Request(t, eventsURI, body, t0)
	noSig.Header.Del("X-Signature")

	tests := []struct {
		reason  string
		req     *http.Request
		claimed string
	}{
		{"signature", badSig, dev.PublicID.String()},
		{"headers", noSig, dev.PublicID.String()},
		{"skew", dev.Request(t, eventsURI, body, t0.Add(2*time.Minute)), dev.PublicID.String()},
		{"unknown", unknown.Request(t, eventsURI, body, t0), unknown.PublicID.String()},
		{"revoked", revoked.Request(t, eventsURI, body, t0), revoked.PublicID.String()},
	}
	for _, tc := range tests {
		t.Run(tc.reason, func(t *testing.T) {
			f := newFixture(t, reg, s)
			f.serve(tc.req.WithContext(reqctx.WithClientIP(tc.req.Context(), netip.MustParseAddr("198.51.100.7"))))
			all.WriteString(f.logs.String())

			var line map[string]any
			if err := json.Unmarshal(bytes.TrimSpace([]byte(f.logs.String())), &line); err != nil {
				t.Fatalf("want one log line, got %q", f.logs)
			}
			if line["msg"] != "device auth failed" || line["level"] != "WARN" || line["reason"] != tc.reason ||
				line["claimed_device_id"] != tc.claimed || line["client_ip"] != "198.51.100.7" {
				t.Errorf("log = %v", line)
			}
		})
	}
	out := all.String()
	for _, leak := range []string{hex.EncodeToString(dev.Secret), expected} {
		if strings.Contains(out, leak) {
			t.Errorf("log leaks %q", leak)
		}
	}
}

func TestRateLimit_DeviceKeyedAfterAuth(t *testing.T) {
	s := testsupport.TestSealer(t)
	reg := &fakeRegistry{}
	a := reg.addDevice(t, s, authz.DevicePodium, false)
	b := reg.addDevice(t, s, authz.DevicePodium, false)
	f := newFixture(t, reg, s)
	log := slog.New(slog.NewJSONHandler(io.Discard, nil))

	mux := http.NewServeMux()
	rt := router.New(mux, router.Middlewares{
		ClientIP:    middleware.ClientIP(nil),
		RateLimit:   middleware.RateLimit(ratelimit.NewMemory(f.clock), log),
		DeviceAuth:  f.auth.Middleware,
		DeviceLimit: ratelimit.DeviceRule(2),
	})
	rt.Handle("POST "+eventsURI, router.Policy{Device: true}, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	send := func(r *http.Request) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, r)
		return rec
	}

	// Unsigned requests claiming device A are refused before they spend A's budget.
	for range 5 {
		spoof := httptest.NewRequest(http.MethodPost, eventsURI, bytes.NewReader(body))
		spoof.Header.Set("X-Device-Id", a.PublicID.String())
		spoof.Header.Set("X-Timestamp", t0.Format(devicesig.TimestampLayout))
		spoof.Header.Set("X-Signature", strings.Repeat("0", 64))
		if rec := send(spoof); rec.Code != http.StatusUnauthorized {
			t.Fatalf("spoofed request: status = %d, want 401", rec.Code)
		}
	}
	for i := range 2 {
		if rec := send(a.Request(t, eventsURI, body, t0)); rec.Code != http.StatusOK {
			t.Fatalf("device A request %d: status = %d %s", i+1, rec.Code, rec.Body)
		}
	}
	rec := send(a.Request(t, eventsURI, body, t0))
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") != "1" {
		t.Errorf("device A third request: %d Retry-After %q, want 429 and 1", rec.Code, rec.Header().Get("Retry-After"))
	}
	if rec := send(b.Request(t, eventsURI, body, t0)); rec.Code != http.StatusOK {
		t.Errorf("device B: status = %d, want 200 (separate budget)", rec.Code)
	}
}
