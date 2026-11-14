package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/XYZLZ/MyPark/apps/server/internal/domain/authz"
	"github.com/XYZLZ/MyPark/apps/server/internal/testsupport"
)

const deviceEventsPath = "/v1/devices/events"

// deviceEnv is the production handler over a database holding the pilot lot.
type deviceEnv struct {
	*apiEnv
	lotID int64
}

func newDeviceEnv(t *testing.T, mods ...func(*deps)) *deviceEnv {
	t.Helper()
	e := newAPIEnv(t, mods...)
	return &deviceEnv{apiEnv: e, lotID: testsupport.SeedPilotLot(t, e.db)}
}

func (e *deviceEnv) device(kind authz.DeviceKind) testsupport.TestDevice {
	e.t.Helper()
	return testsupport.NewTestDevice(e.t, e.db, e.d.sealer, e.lotID, kind)
}

// eventBody is a device event body with occurred_at one second before now.
func (e *deviceEnv) eventBody(id uuid.UUID, typ string, payload any) []byte {
	e.t.Helper()
	raw, err := json.Marshal(map[string]any{
		"event_id": id, "type": typ,
		"occurred_at": e.clk.Now().Add(-time.Second).Format(time.RFC3339),
		"payload":     payload,
	})
	if err != nil {
		e.t.Fatal(err)
	}
	return raw
}

func sensorPayload(code string, occupied bool) map[string]any {
	return map[string]any{"space_code": code, "occupied": occupied}
}

// send signs body as d at the current fake time and serves it.
func (e *deviceEnv) send(d testsupport.TestDevice, body []byte) *httptest.ResponseRecorder {
	e.t.Helper()
	return e.serve(d.Request(e.t, deviceEventsPath, body, e.clk.Now()), "")
}

func (e *deviceEnv) count(sql string, args ...any) int64 {
	e.t.Helper()
	var n int64
	if err := e.db.Super.QueryRow(e.t.Context(), sql, args...).Scan(&n); err != nil {
		e.t.Fatalf("count %q: %v", sql, err)
	}
	return n
}

func (e *deviceEnv) eventRows() int64 { return e.count(`select count(*) from device_events`) }

func (e *deviceEnv) spaceStatus(code string) string {
	e.t.Helper()
	var s string
	if err := e.db.Super.QueryRow(e.t.Context(),
		`select status from spaces where lot_id = $1 and code = $2`, e.lotID, code).Scan(&s); err != nil {
		e.t.Fatalf("space %s: %v", code, err)
	}
	return s
}

// dataOf returns the raw data member of a 200 envelope.
func dataOf(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body)
	}
	var env struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("body %s: %v", rec.Body, err)
	}
	return string(env.Data)
}

func wantError(t *testing.T, rec *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if rec.Code != status || errCode(decodeBody(t, rec)) != code {
		t.Fatalf("got %d %s, want %d %s", rec.Code, rec.Body, status, code)
	}
}

// spaceListener is a dedicated LISTEN space_changed connection.
type spaceListener struct {
	t    *testing.T
	e    *deviceEnv
	conn *pgx.Conn
}

func (e *deviceEnv) listenSpaces() *spaceListener {
	e.t.Helper()
	pc, err := e.db.Super.Acquire(e.t.Context())
	if err != nil {
		e.t.Fatal(err)
	}
	conn := pc.Hijack()
	e.t.Cleanup(func() { _ = conn.Close(context.Background()) })
	if _, err := conn.Exec(e.t.Context(), `listen space_changed`); err != nil {
		e.t.Fatal(err)
	}
	return &spaceListener{t: e.t, e: e, conn: conn}
}

func (l *spaceListener) next() string {
	l.t.Helper()
	ctx, cancel := context.WithTimeout(l.t.Context(), 10*time.Second)
	defer cancel()
	n, err := l.conn.WaitForNotification(ctx)
	if err != nil {
		l.t.Fatalf("wait for notification: %v", err)
	}
	return n.Payload
}

// drain returns every notification delivered before a sentinel sent now.
// Notifications arrive in commit order, so nothing is missed and no wait on
// silence is needed.
func (l *spaceListener) drain() []string {
	l.t.Helper()
	if _, err := l.e.db.Super.Exec(l.t.Context(), `select pg_notify('space_changed', 'sentinel')`); err != nil {
		l.t.Fatal(err)
	}
	var got []string
	for {
		p := l.next()
		if p == "sentinel" {
			return got
		}
		got = append(got, p)
	}
}

func TestDeviceEvents_HeartbeatAck(t *testing.T) {
	e := newDeviceEnv(t)
	d := e.device(authz.DevicePodium)
	body := e.eventBody(uuid.New(), "heartbeat", map[string]any{})

	rec := e.send(d, body)
	if got := dataOf(t, rec); got != `{"result":"ack"}` {
		t.Fatalf("data = %s", got)
	}
	if rec.Header().Get("Idempotent-Replayed") != "" {
		t.Error("first answer is marked replayed")
	}

	var lotID, deviceID int64
	var sum []byte
	var typ, payload, response string
	err := e.db.Super.QueryRow(t.Context(), `
		select lot_id, device_id, body_sha256, type, payload::text, response::text from device_events`).
		Scan(&lotID, &deviceID, &sum, &typ, &payload, &response)
	if err != nil {
		t.Fatalf("read event: %v", err)
	}
	want := sha256.Sum256(body)
	if !bytes.Equal(sum, want[:]) {
		t.Error("body_sha256 is not sha256 of the raw body")
	}
	if lotID != d.LotID || deviceID != d.ID || typ != "heartbeat" || payload != `{}` || response != `{"result": "ack"}` {
		t.Errorf("row = lot %d device %d type %s payload %s response %s", lotID, deviceID, typ, payload, response)
	}
	if n := e.eventRows(); n != 1 {
		t.Errorf("rows = %d, want 1", n)
	}
}

func TestDeviceEvents_ReplayReturnsStoredResponse(t *testing.T) {
	e := newDeviceEnv(t)
	d := e.device(authz.DeviceSensorGateway)
	body := e.eventBody(uuid.New(), "sensor.space_changed", sensorPayload("E999", true))

	first := e.send(d, body)
	second := e.send(d, body)
	if a, b := dataOf(t, first), dataOf(t, second); a != b {
		t.Fatalf("replay data %s differs from %s", b, a)
	}
	if got := second.Header().Get("Idempotent-Replayed"); got != "true" {
		t.Errorf("Idempotent-Replayed = %q, want true", got)
	}
	if n := e.eventRows(); n != 1 {
		t.Errorf("rows = %d, want 1", n)
	}
}

func TestDeviceEvents_ReplayAfter90sFreshSignature(t *testing.T) {
	e := newDeviceEnv(t)
	d := e.device(authz.DevicePodium)
	body := e.eventBody(uuid.New(), "podium.vehicle_detected", map[string]any{})

	first := dataOf(t, e.send(d, body))
	e.clk.Advance(90 * time.Second)
	rec := e.send(d, body) // re-signed with the new X-Timestamp, same body
	if got := dataOf(t, rec); got != first {
		t.Fatalf("data = %s, want %s", got, first)
	}
	if rec.Header().Get("Idempotent-Replayed") != "true" {
		t.Error("late retry is not marked replayed")
	}
	if n := e.eventRows(); n != 1 {
		t.Errorf("rows = %d, want 1", n)
	}
}

func TestDeviceEvents_SameIDDifferentBody409(t *testing.T) {
	e := newDeviceEnv(t)
	d := e.device(authz.DeviceSensorGateway)
	id := uuid.New()
	dataOf(t, e.send(d, e.eventBody(id, "sensor.space_changed", sensorPayload("E001", true))))

	rec := e.send(d, e.eventBody(id, "sensor.space_changed", sensorPayload("E001", false)))
	wantError(t, rec, http.StatusConflict, "device.event_conflict")
	if n := e.eventRows(); n != 1 {
		t.Errorf("rows = %d, want 1", n)
	}
	if s := e.spaceStatus("E001"); s != "occupied" {
		t.Errorf("E001 = %s, want occupied (the conflict must not apply)", s)
	}
}

// S4 carry: a signature replays for 60 s, so concurrent duplicates must
// resolve to exactly one processing.
func TestDeviceEvents_ConcurrentDuplicates(t *testing.T) {
	e := newDeviceEnv(t)
	d := e.device(authz.DeviceSensorGateway)
	l := e.listenSpaces()
	body := e.eventBody(uuid.New(), "sensor.space_changed", sensorPayload("E001", true))

	const n = 20
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	recs := make([]*httptest.ResponseRecorder, n)
	var wg sync.WaitGroup
	for i := range n {
		req := d.Request(t, deviceEventsPath, body, e.clk.Now()).WithContext(ctx)
		req.RemoteAddr = "203.0.113.7:5555"
		wg.Go(func() {
			recs[i] = httptest.NewRecorder()
			e.h.ServeHTTP(recs[i], req)
		})
	}
	wg.Wait()
	if ctx.Err() != nil {
		t.Fatal("requests did not finish within 10 s (deadlock?)")
	}

	fresh := 0
	for i, rec := range recs {
		if got := dataOf(t, rec); got != `{"result":"ack"}` {
			t.Errorf("request %d: data = %s", i, got)
		}
		if rec.Header().Get("Idempotent-Replayed") == "" {
			fresh++
		}
	}
	if fresh != 1 {
		t.Errorf("%d answers were processed, want exactly 1 (the rest replayed)", fresh)
	}
	if rows := e.eventRows(); rows != 1 {
		t.Errorf("rows = %d, want 1", rows)
	}
	if got := l.drain(); len(got) != 1 || got[0] != fmt.Sprintf("%d:E001:occupied", e.lotID) {
		t.Errorf("notifications = %v, want exactly one space update", got)
	}
}

// Racing duplicates with two different bodies: a loser whose body flips the
// space gets past the sensor's WHERE clause and writes and notifies before
// its insert hits 23505, so only its rollback keeps the effect single. Every
// loser answers the winner's stored response (same body) or 409 (other body).
func TestDeviceEvents_ConcurrentConflictingBodies(t *testing.T) {
	e := newDeviceEnv(t)
	d := e.device(authz.DeviceSensorGateway)
	l := e.listenSpaces()
	id := uuid.New()
	bodies := [2][]byte{
		e.eventBody(id, "sensor.space_changed", sensorPayload("E001", true)),
		e.eventBody(id, "sensor.space_changed", sensorPayload("E001", false)),
	}

	const n = 20
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	recs := make([]*httptest.ResponseRecorder, n)
	var wg sync.WaitGroup
	for i := range n {
		req := d.Request(t, deviceEventsPath, bodies[i%2], e.clk.Now()).WithContext(ctx)
		req.RemoteAddr = "203.0.113.7:5555"
		wg.Go(func() {
			recs[i] = httptest.NewRecorder()
			e.h.ServeHTTP(recs[i], req)
		})
	}
	wg.Wait()
	if ctx.Err() != nil {
		t.Fatal("requests did not finish within 10 s (deadlock?)")
	}

	winner := -1
	for i, rec := range recs {
		if rec.Code == http.StatusOK && rec.Header().Get("Idempotent-Replayed") == "" {
			if winner >= 0 {
				t.Fatalf("requests %d and %d were both processed", winner, i)
			}
			winner = i % 2
		}
	}
	if winner < 0 {
		t.Fatal("no request was processed")
	}
	for i, rec := range recs {
		switch {
		case i%2 == winner && rec.Code == http.StatusOK:
		case i%2 != winner && rec.Code == http.StatusConflict && errCode(decodeBody(t, rec)) == "device.event_conflict":
		default:
			t.Errorf("request %d (body %d, winner %d): got %d %s", i, i%2, winner, rec.Code, rec.Body)
		}
	}

	var sum []byte
	if err := e.db.Super.QueryRow(t.Context(), `select body_sha256 from device_events`).Scan(&sum); err != nil {
		t.Fatalf("read event: %v", err)
	}
	if want := sha256.Sum256(bodies[winner]); !bytes.Equal(sum, want[:]) || e.eventRows() != 1 {
		t.Errorf("stored %d rows, hash of the winner = %v", e.eventRows(), bytes.Equal(sum, want[:]))
	}
	wantState, wantNotes := "occupied", []string{fmt.Sprintf("%d:E001:occupied", e.lotID)}
	if winner == 1 { // free over a free space: nothing changes
		wantState, wantNotes = "free", nil
	}
	if s := e.spaceStatus("E001"); s != wantState {
		t.Errorf("E001 = %s, want %s", s, wantState)
	}
	if got := l.drain(); fmt.Sprint(got) != fmt.Sprint(wantNotes) {
		t.Errorf("notifications = %v, want %v", got, wantNotes)
	}
}

func TestDeviceEvents_KindMatrix403StoresNothing(t *testing.T) {
	e := newDeviceEnv(t)
	d := e.device(authz.DeviceSensorGateway)
	body := e.eventBody(uuid.New(), "paystation.scan",
		map[string]any{"credential": map[string]any{"kind": "ticket", "value": "ABCD1234EFGH5678"}})

	wantError(t, e.send(d, body), http.StatusForbidden, "device.event_not_allowed")
	if n := e.eventRows(); n != 0 {
		t.Errorf("rows = %d, want 0", n)
	}
}

func TestDeviceEvents_ValidationStoresNothing(t *testing.T) {
	e := newDeviceEnv(t)
	d := e.device(authz.DeviceSensorGateway)
	body := e.eventBody(uuid.New(), "sensor.space_changed", sensorPayload("X1", true))

	first := e.send(d, body)
	wantError(t, first, http.StatusUnprocessableEntity, "validation.failed")
	second := e.send(d, body)
	wantError(t, second, http.StatusUnprocessableEntity, "validation.failed")
	strip := func(r *httptest.ResponseRecorder) any { b := decodeBody(t, r); return b["error"] }
	if a, b := fmt.Sprint(strip(first)), fmt.Sprint(strip(second)); a != b {
		t.Errorf("repeat answered %s, first %s", b, a)
	}
	if n := e.eventRows(); n != 0 {
		t.Errorf("rows = %d, want 0", n)
	}
}

// S5.1 carry: a body that is not valid JSON stays 400 even when an unknown key
// comes before the syntax error.
func TestDeviceEvents_MalformedJSONIs400(t *testing.T) {
	e := newDeviceEnv(t)
	d := e.device(authz.DevicePodium)
	wantError(t, e.send(d, []byte(`{"zzz":1,"event_id":`)), http.StatusBadRequest, "http.invalid_json")
	if n := e.eventRows(); n != 0 {
		t.Errorf("rows = %d, want 0", n)
	}
}

// PF7: a device body must be application/json (a charset parameter is fine).
func TestDeviceEvents_ContentType415(t *testing.T) {
	e := newDeviceEnv(t)
	d := e.device(authz.DevicePodium)
	for _, ct := range []string{"text/plain", "", "application/jsonx", "multipart/form-data"} {
		req := d.Request(t, deviceEventsPath, e.eventBody(uuid.New(), "heartbeat", map[string]any{}), e.clk.Now())
		if ct == "" {
			req.Header.Del("Content-Type")
		} else {
			req.Header.Set("Content-Type", ct)
		}
		rec := e.serve(req, "")
		if rec.Code != http.StatusUnsupportedMediaType || errCode(decodeBody(t, rec)) != "http.unsupported_media_type" {
			t.Errorf("Content-Type %q: got %d %s, want 415", ct, rec.Code, rec.Body)
		}
	}
	if n := e.eventRows(); n != 0 {
		t.Errorf("rows = %d, want 0", n)
	}
	req := d.Request(t, deviceEventsPath, e.eventBody(uuid.New(), "heartbeat", map[string]any{}), e.clk.Now())
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	dataOf(t, e.serve(req, ""))
}

// Event types whose slice has not landed answer 500 and store nothing, so the
// device retries once the handler exists.
func TestDeviceEvents_UnhandledTypeIs500StoresNothing(t *testing.T) {
	e := newDeviceEnv(t)
	d := e.device(authz.DeviceDispenser)
	body := e.eventBody(uuid.New(), "entry.credential_presented", map[string]any{"kind": "ticket_request"})
	wantError(t, e.send(d, body), http.StatusInternalServerError, "internal.unexpected")
	if n := e.eventRows(); n != 0 {
		t.Errorf("rows = %d, want 0", n)
	}
}

// S3 carry: the stored lot is the authenticated device's, and a body cannot
// name one.
func TestDeviceEvents_LotComesFromDevice(t *testing.T) {
	e := newDeviceEnv(t)
	d := e.device(authz.DevicePodium)
	raw := []byte(fmt.Sprintf(`{"event_id":%q,"type":"heartbeat","occurred_at":%q,"payload":{},"lot_id":999}`,
		uuid.NewString(), e.clk.Now().Format(time.RFC3339)))
	wantError(t, e.send(d, raw), http.StatusUnprocessableEntity, "validation.failed")
	dataOf(t, e.send(d, e.eventBody(uuid.New(), "heartbeat", map[string]any{})))
	if n := e.count(`select count(*) from device_events where lot_id = $1`, d.LotID); n != 1 {
		t.Errorf("rows in the device's lot = %d, want 1", n)
	}
}

func TestSensor_ChangesAndNotifies(t *testing.T) {
	e := newDeviceEnv(t)
	d := e.device(authz.DeviceSensorGateway)
	l := e.listenSpaces()

	rec := e.send(d, e.eventBody(uuid.New(), "sensor.space_changed", sensorPayload("E001", true)))
	if got := dataOf(t, rec); got != `{"result":"ack"}` {
		t.Fatalf("data = %s", got)
	}
	if s := e.spaceStatus("E001"); s != "occupied" {
		t.Errorf("E001 = %s, want occupied", s)
	}
	if got := l.drain(); len(got) != 1 || got[0] != fmt.Sprintf("%d:E001:occupied", e.lotID) {
		t.Errorf("notifications = %v", got)
	}
	var at time.Time
	if err := e.db.Super.QueryRow(t.Context(),
		`select sensor_updated_at from spaces where lot_id = $1 and code = 'E001'`, e.lotID).Scan(&at); err != nil {
		t.Fatal(err)
	}
	if !at.Equal(e.clk.Now()) {
		t.Errorf("sensor_updated_at = %v, want the server clock %v", at, e.clk.Now())
	}

	// The same state again, under a new event id: ack, nothing changes.
	rec = e.send(d, e.eventBody(uuid.New(), "sensor.space_changed", sensorPayload("E001", true)))
	if got := dataOf(t, rec); got != `{"result":"ack"}` {
		t.Fatalf("repeat data = %s", got)
	}
	if got := l.drain(); len(got) != 0 {
		t.Errorf("unchanged space notified %v", got)
	}

	// And back to free.
	dataOf(t, e.send(d, e.eventBody(uuid.New(), "sensor.space_changed", sensorPayload("E001", false))))
	if got := l.drain(); len(got) != 1 || got[0] != fmt.Sprintf("%d:E001:free", e.lotID) {
		t.Errorf("notifications = %v", got)
	}
}

func TestSensor_UnknownCodeRejected(t *testing.T) {
	e := newDeviceEnv(t)
	d := e.device(authz.DeviceSensorGateway)
	rec := e.send(d, e.eventBody(uuid.New(), "sensor.space_changed", sensorPayload("E999", true)))
	if got := dataOf(t, rec); got != `{"result":"rejected","reason":"lot.space_unknown"}` {
		t.Fatalf("data = %s", got)
	}
	if n := e.eventRows(); n != 1 {
		t.Errorf("rows = %d, want 1 (a business refusal is a stored 200)", n)
	}
}

func TestSensor_NeverOverwritesOutOfService(t *testing.T) {
	e := newDeviceEnv(t)
	d := e.device(authz.DeviceSensorGateway)
	if _, err := e.db.Super.Exec(t.Context(),
		`update spaces set status = 'out_of_service' where lot_id = $1 and code = 'E002'`, e.lotID); err != nil {
		t.Fatal(err)
	}
	l := e.listenSpaces()
	rec := e.send(d, e.eventBody(uuid.New(), "sensor.space_changed", sensorPayload("E002", true)))
	if got := dataOf(t, rec); got != `{"result":"ack"}` {
		t.Fatalf("data = %s", got)
	}
	if s := e.spaceStatus("E002"); s != "out_of_service" {
		t.Errorf("E002 = %s, want out_of_service", s)
	}
	if got := l.drain(); len(got) != 0 {
		t.Errorf("notifications = %v, want none", got)
	}
}

func TestSensor_OccupancyVersionUntouched(t *testing.T) {
	e := newDeviceEnv(t)
	d := e.device(authz.DeviceSensorGateway)
	before := e.count(`select version from lot_occupancy where lot_id = $1`, e.lotID)
	for i := range 5 {
		dataOf(t, e.send(d, e.eventBody(uuid.New(), "sensor.space_changed", sensorPayload("E003", i%2 == 0))))
	}
	if after := e.count(`select version from lot_occupancy where lot_id = $1`, e.lotID); after != before {
		t.Errorf("lot_occupancy.version %d -> %d, want unchanged", before, after)
	}
}
