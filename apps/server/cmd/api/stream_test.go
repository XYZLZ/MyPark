package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/XYZLZ/MyPark/apps/server/internal/config"
	"github.com/XYZLZ/MyPark/apps/server/internal/domain/authz"
	"github.com/XYZLZ/MyPark/apps/server/internal/domain/errcodes"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/availability"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/clock"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/pubsub"
	"github.com/XYZLZ/MyPark/apps/server/internal/testsupport"
	"github.com/XYZLZ/MyPark/apps/server/internal/transport/http/httpserver"
)

// frameWait bounds every wait for an SSE frame.
const frameWait = 5 * time.Second

// streamEnv is the production handler over the pilot lot, with a hub fed by
// a real LISTEN connection. base is empty until serve is called.
type streamEnv struct {
	*deviceEnv
	publicID string
	hub      *availability.Hub
	base     string
}

// withConfig reloads the configuration with extra variables (the SSE caps).
func withConfig(t *testing.T, extra map[string]string) func(*deps) {
	return func(d *deps) {
		env := testEnv(extra)
		env["DATABASE_URL"] = d.cfg.DatabaseURL()
		cfg, err := config.Load(func(k string) string { return env[k] })
		if err != nil {
			t.Fatalf("config: %v", err)
		}
		d.cfg = cfg
	}
}

// withTimers runs the hub and the streams on t (the fake clock by default).
func withTimers(t clock.Timers) func(*deps) {
	return func(d *deps) { d.timers = t }
}

// newStreamEnv builds the env, then starts the listener and waits for it, so
// its first-connect resync happens before any stream exists.
func newStreamEnv(t *testing.T, mods ...func(*deps)) *streamEnv {
	t.Helper()
	mods = append(mods, func(d *deps) {
		d.avail = newAvailabilityService(d.pool, d.clock)
		d.hub = availability.NewHub(d.avail, d.timers, d.log)
	})
	de := newDeviceEnv(t, mods...)
	var pub uuid.UUID
	if err := de.db.App.QueryRow(t.Context(), "select public_id from lots where id = $1", de.lotID).Scan(&pub); err != nil {
		t.Fatal(err)
	}
	return &streamEnv{deviceEnv: de, publicID: pub.String(), hub: de.d.hub}
}

// listen starts the LISTEN connection. Call it after the server's cleanup is
// registered: cleanups run last first, so the hub then closes the streams
// before the server waits on them.
func (e *streamEnv) listen() {
	e.t.Helper()
	l := pubsub.New(e.db.App, e.d.log, pubsub.Handlers{
		LotChanged: e.hub.LotChanged, SpaceChanged: e.hub.SpaceChanged, Reconnected: e.hub.Resync,
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); _ = l.Run(ctx) }()
	e.t.Cleanup(func() { cancel(); <-done; e.hub.Close() })
	deadline := time.Now().Add(frameWait)
	for !l.Connected() {
		if time.Now().After(deadline) {
			e.t.Fatal("listener did not connect")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// serve starts an httptest server on the handler, then the listener.
func (e *streamEnv) serve() {
	e.t.Helper()
	srv := httptest.NewServer(e.h)
	e.t.Cleanup(srv.Close)
	e.base = srv.URL
	e.listen()
}

func (e *streamEnv) streamURL(query string) string {
	return e.base + "/v1/lots/" + e.publicID + "/availability/stream" + query
}

// open sends a stream request; it ends with the test. The caller reads the
// body (ReadSSE for a stream, a JSON decode for an error).
func (e *streamEnv) open(url string, header map[string]string) *http.Response {
	e.t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	e.t.Cleanup(cancel)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		e.t.Fatal(err)
	}
	for k, v := range header {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatalf("GET %s: %v", url, err)
	}
	e.t.Cleanup(func() { resp.Body.Close() })
	return resp
}

// openOK opens a stream, checks the 200 headers and the first two frames
// (retry, then the availability snapshot) and returns the snapshot.
func (e *streamEnv) openOK(url string, header map[string]string) (availability.Snapshot, <-chan testsupport.SSEFrame) {
	e.t.Helper()
	resp := e.open(url, header)
	frames := testsupport.ReadSSE(resp.Body)
	if resp.StatusCode != http.StatusOK {
		e.t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	for k, v := range map[string]string{
		"Content-Type": "text/event-stream", "Cache-Control": "no-store", "X-Accel-Buffering": "no",
	} {
		if got := resp.Header.Get(k); got != v {
			e.t.Errorf("%s = %q, want %q", k, got, v)
		}
	}
	if f := testsupport.NextSSE(e.t, frames, frameWait); f.Retry != "3000" || f.Event != "" {
		e.t.Fatalf("first frame = %+v, want retry: 3000", f)
	}
	return snapshotOf(e.t, testsupport.NextSSE(e.t, frames, frameWait)), frames
}

// snapshotOf decodes an availability frame and checks its id is its version.
func snapshotOf(t *testing.T, f testsupport.SSEFrame) availability.Snapshot {
	t.Helper()
	if f.Event != "availability" {
		t.Fatalf("frame = %+v, want an availability event", f)
	}
	var snap availability.Snapshot
	if err := json.Unmarshal([]byte(f.Data), &snap); err != nil {
		t.Fatalf("availability data %q: %v", f.Data, err)
	}
	if f.ID != strconv.FormatInt(snap.Version, 10) {
		t.Fatalf("id = %q, want the version %d", f.ID, snap.Version)
	}
	return snap
}

// heartbeat advances fake time by one heartbeat period and expects the
// heartbeat, proving no other frame was written before it.
func (e *streamEnv) heartbeat(frames <-chan testsupport.SSEFrame) {
	e.t.Helper()
	e.clk.Advance(availability.HeartbeatEvery)
	want := fmt.Sprintf(`{"as_of":%q}`, e.clk.Now().Format(time.RFC3339))
	if f := testsupport.NextSSE(e.t, frames, frameWait); f.Event != "heartbeat" || f.Data != want || f.ID != "" {
		e.t.Fatalf("frame = %+v, want heartbeat %s", f, want)
	}
}

func TestStream_SnapshotFirst(t *testing.T) {
	e := newStreamEnv(t)
	e.serve()
	_, version := e.occupancy()
	snap, _ := e.openOK(e.streamURL(""), nil)
	if snap.Version != version || snap.Capacity != 280 || snap.LotID.String() != e.publicID {
		t.Fatalf("snapshot = %+v, want version %d of lot %s", snap, version, e.publicID)
	}
}

func TestStream_IncludeSpacesFullThenDeltas(t *testing.T) {
	e := newStreamEnv(t)
	e.serve()
	_, frames := e.openOK(e.streamURL("?include=spaces"), nil)
	f := testsupport.NextSSE(t, frames, frameWait)
	var full availability.SpacesMsg
	if err := json.Unmarshal([]byte(f.Data), &full); err != nil || f.Event != "spaces" || f.ID != "" ||
		!full.Full || len(full.Spaces) != 280 {
		t.Fatalf("frame = %+v (%v), want a full spaces event with 280 spaces and no id", f, err)
	}

	gw := e.device(authz.DeviceSensorGateway)
	dataOf(t, e.send(gw, e.eventBody(uuid.New(), "sensor.space_changed", sensorPayload("E117", true))))
	f = testsupport.NextSSE(t, frames, frameWait)
	if want := `{"full":false,"spaces":[{"code":"E117","state":"occupied"}]}`; f.Event != "spaces" || f.Data != want || f.ID != "" {
		t.Fatalf("frame = %+v, want spaces %s with no id", f, want)
	}
}

func TestStream_PassPushesAvailability(t *testing.T) {
	e := newStreamEnv(t)
	e.serve()
	first, frames := e.openOK(e.streamURL(""), nil)
	id := e.openStay(e.device(authz.DeviceDispenser), "")
	// Past the coalescing window, so the pass is a leading-edge push.
	e.clk.Advance(time.Second)
	occ, ver := e.occupancy()
	if got := e.pass(e.device(authz.DeviceEntryBarrier), id); got.Result != "ack" {
		t.Fatalf("pass = %+v, want ack", got)
	}
	for {
		snap := snapshotOf(t, testsupport.NextSSE(t, frames, frameWait))
		if snap.Version <= ver {
			continue // a push for the entry, if it changed the version
		}
		if snap.Version != ver+1 || int64(snap.Occupied) != occ+1 || snap.Occupied != first.Occupied+1 {
			t.Fatalf("snapshot = %+v, want version %d with occupied %d", snap, ver+1, occ+1)
		}
		return
	}
}

func TestStream_HeartbeatOnFakeTime(t *testing.T) {
	e := newStreamEnv(t)
	e.serve()
	_, frames := e.openOK(e.streamURL(""), nil)
	e.heartbeat(frames)
}

// Review focus 5: garbage Last-Event-ID and include values never fail the
// stream, and only exactly include=spaces adds the spaces event.
func TestStream_GarbageLastEventIDAndInclude(t *testing.T) {
	e := newStreamEnv(t)
	e.serve()
	cases := []struct {
		name, query, lastEventID string
	}{
		{"last-event-id abc", "", "abc"},
		{"last-event-id -1", "", "-1"},
		{"last-event-id overflow", "", "99999999999999999999"},
		{"include=foo", "?include=foo", ""},
		{"include=spaces,spaces", "?include=spaces,spaces", ""},
		{"include twice", "?include=spaces&include=spaces", ""},
	}
	// No subtests: the env helpers report on the parent test.
	for _, c := range cases {
		t.Logf("case %s", c.name)
		var header map[string]string
		if c.lastEventID != "" {
			header = map[string]string{"Last-Event-ID": c.lastEventID}
		}
		_, frames := e.openOK(e.streamURL(c.query), header)
		e.heartbeat(frames) // no spaces event came before it
	}
}

func TestStream_UnknownLot404BeforeStream(t *testing.T) {
	e := newStreamEnv(t)
	e.serve()
	for _, id := range []string{uuid.NewString(), "zona-colonial"} {
		resp := e.open(e.base+"/v1/lots/"+id+"/availability/stream", nil)
		var body map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatalf("%s: decode: %v", id, err)
		}
		if resp.StatusCode != http.StatusNotFound || errCode(body) != string(errcodes.LotNotFound) ||
			resp.Header.Get("Content-Type") != "application/json" {
			t.Fatalf("%s: got %d %s %v, want a 404 lot.not_found JSON envelope",
				id, resp.StatusCode, resp.Header.Get("Content-Type"), body)
		}
	}
}

func TestStream_CapsReturn429(t *testing.T) {
	e := newStreamEnv(t, withConfig(t, map[string]string{"SSE_MAX_PER_IP": "1"}))
	e.serve()
	e.openOK(e.streamURL(""), nil) // open while the second one is tried

	resp := e.open(e.streamURL(""), nil)
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.StatusCode != http.StatusTooManyRequests || errCode(body) != string(errcodes.RateLimitExceeded) ||
		resp.Header.Get("Retry-After") != "10" || resp.Header.Get("Content-Type") != "application/json" {
		t.Fatalf("second stream = %d (Retry-After %q) %v, want 429 rate_limit.exceeded with Retry-After 10",
			resp.StatusCode, resp.Header.Get("Retry-After"), body)
	}
}

// Review focus 1: the server WriteTimeout covers one write, not the stream.
// The heartbeats are written well after 300 ms of real time have passed.
func TestStream_OutlivesServerWriteTimeout(t *testing.T) {
	e := newStreamEnv(t)
	srv := httpserver.New("127.0.0.1:0", e.h, e.d.log)
	srv.WriteTimeout = 300 * time.Millisecond
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { srv.Close() })
	e.base = "http://" + ln.Addr().String()
	e.listen()

	_, frames := e.openOK(e.streamURL(""), nil)
	start, t0 := time.Now(), e.clk.Now()
	// Real time passes between heartbeats (no sleep in the test goroutine).
	for i := range 3 {
		time.AfterFunc(time.Duration(i+1)*400*time.Millisecond, func() { e.clk.Advance(availability.HeartbeatEvery) })
	}
	for i := range 3 {
		f := testsupport.NextSSE(t, frames, frameWait)
		want := fmt.Sprintf(`{"as_of":%q}`, t0.Add(time.Duration(i+1)*availability.HeartbeatEvery).Format(time.RFC3339))
		if f.Event != "heartbeat" || f.Data != want {
			t.Fatalf("frame %d = %+v, want heartbeat %s", i+1, f, want)
		}
	}
	if elapsed := time.Since(start); elapsed < time.Second {
		t.Fatalf("only %v of real time passed, want at least 1s", elapsed)
	}
}

// Design §11.4: shutdown closes the hub first, so open streams end at once
// and Serve does not wait out its shutdown timeout.
func TestStream_EndsOnShutdown(t *testing.T) {
	e := newStreamEnv(t)
	stop := make(chan struct{})
	returned := make(chan time.Time, 1)
	e.base = testsupport.StartServer(t, func(ctx context.Context, _ func(string) string, stdout io.Writer) error {
		ctx, cancel := context.WithCancel(ctx)
		defer cancel()
		go func() {
			select {
			case <-stop:
			case <-ctx.Done():
			}
			cancel()
		}()
		log := slog.New(slog.NewJSONHandler(stdout, nil))
		err := httpserver.Serve(ctx, log, newServer("127.0.0.1:0", e.h, log, e.hub), "api")
		returned <- time.Now()
		return err
	}, nil, "api listening")
	e.listen()

	_, frames := e.openOK(e.streamURL(""), nil)
	shutdownAt := time.Now()
	close(stop)
	select {
	case f, ok := <-frames:
		if ok {
			t.Fatalf("got frame %+v, want the stream to end", f)
		}
	case <-time.After(time.Second):
		t.Fatal("the stream did not end within 1s of shutdown")
	}
	select {
	case at := <-returned:
		if d := at.Sub(shutdownAt); d > time.Second {
			t.Fatalf("Serve returned %v after shutdown, want within 1s", d)
		}
	case <-time.After(time.Second):
		t.Fatal("Serve did not return within 1s of shutdown")
	}
}

// Design §11.6: /readyz reports the listener, 503 while it is down.
func TestReadyz_ListenerDown(t *testing.T) {
	var up atomic.Bool
	d := testDeps(readyHealth(), nil)
	d.listenerUp = up.Load
	h := newHandler(d)
	get := func() (int, map[string]any) {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
		return rec.Code, decodeBody(t, rec)
	}

	code, body := get()
	errObj, _ := body["error"].(map[string]any)
	details, _ := errObj["details"].(map[string]any)
	if code != http.StatusServiceUnavailable || errCode(body) != string(errcodes.HealthNotReady) || details["listener"] != "fail" {
		t.Fatalf("listener down: /readyz = %d %v, want 503 health.not_ready listener=fail", code, body)
	}

	up.Store(true)
	code, body = get()
	data, _ := body["data"].(map[string]any)
	checks, _ := data["checks"].(map[string]any)
	if code != http.StatusOK || checks["listener"] != "ok" {
		t.Fatalf("listener up: /readyz = %d %v, want 200 listener=ok", code, body)
	}
}

// The listener's connection is hijacked out of the pool, so the pool keeps
// one connection less for requests, never fewer than one.
func TestRequestPoolConns(t *testing.T) {
	for _, c := range []struct{ max, want int32 }{{10, 9}, {2, 1}, {1, 1}} {
		if got := requestPoolConns(c.max); got != c.want {
			t.Errorf("requestPoolConns(%d) = %d, want %d", c.max, got, c.want)
		}
	}
}

// NFR-PERF-1 evidence: real clock, real hub, real Postgres. p95 of the time
// from a pass's commit until every one of 50 clients has an availability
// event at that version or later stays under 2 s.
func TestAvailability_PassToSSELatency(t *testing.T) {
	const clients, passes = 50, 200
	e := newStreamEnv(t, withTimers(clock.System{}), withConfig(t, map[string]string{"SSE_MAX_PER_IP": "100"}))
	e.serve()

	type recv struct {
		version int64
		at      time.Time
	}
	var (
		mu       sync.Mutex
		received = make([][]recv, clients)
	)
	for c := range clients {
		_, frames := e.openOK(e.streamURL(""), nil)
		go func() {
			for f := range frames {
				if f.Event != "availability" {
					continue
				}
				var snap availability.Snapshot
				if json.Unmarshal([]byte(f.Data), &snap) != nil {
					continue
				}
				mu.Lock()
				received[c] = append(received[c], recv{snap.Version, time.Now()})
				mu.Unlock()
			}
		}()
	}

	entry, dispenser := e.device(authz.DeviceEntryBarrier), e.device(authz.DeviceDispenser)
	committed := make(map[int64]time.Time, passes)
	var last int64
	for range passes {
		id := e.openStay(dispenser, "")
		if got := e.pass(entry, id); got.Result != "ack" {
			t.Fatalf("pass = %+v, want ack", got)
		}
		at := time.Now() // the answer comes after the commit
		_, last = e.occupancy()
		committed[last] = at
	}

	// Wait until every client has the final version.
	deadline := time.Now().Add(10 * time.Second)
	for {
		mu.Lock()
		done := true
		for _, r := range received {
			if len(r) == 0 || r[len(r)-1].version < last {
				done = false
			}
		}
		mu.Unlock()
		if done {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("not every client received version %d within 10s", last)
		}
		time.Sleep(10 * time.Millisecond)
	}

	mu.Lock()
	defer mu.Unlock()
	var lat []time.Duration
	for v, at := range committed {
		var worst time.Duration
		for _, r := range received {
			i := slices.IndexFunc(r, func(x recv) bool { return x.version >= v })
			worst = max(worst, r[i].at.Sub(at))
		}
		lat = append(lat, worst)
	}
	slices.Sort(lat)
	p50, p95 := lat[len(lat)/2], lat[len(lat)*95/100]
	t.Logf("pass → SSE latency over %d passes × %d clients: p50 %v, p95 %v, max %v",
		passes, clients, p50, p95, lat[len(lat)-1])
	if p95 >= 2*time.Second {
		t.Fatalf("p95 = %v, want < 2s", p95)
	}
}
