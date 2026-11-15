package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/XYZLZ/MyPark/apps/server/internal/devicefleet"
	"github.com/XYZLZ/MyPark/apps/server/internal/domain/authz"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/devices"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/gate"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/database/dbgen"
	"github.com/XYZLZ/MyPark/apps/server/internal/simulator"
	"github.com/XYZLZ/MyPark/apps/server/internal/testsupport"
)

// The scenario tests run the simulator over the real handler and database on
// one shared FakeClock (M2C-14): it is the API clock, the simulator's signing
// clock and the void job's now. Nothing sleeps: virtual time jumps with the
// step clock, a pacer parks the engine between two steps at a virtual limit
// (so a run is never cut mid-request), and waits are driven by signals.

const (
	voidTick = 15 * time.Second
	// settleWait only bounds a hung test; it is never a pause.
	settleWait = 2 * time.Minute
)

// weekdayStart is Friday 2026-11-20 07:00 in the lot's zone (UTC-4).
var weekdayStart = time.Date(2026, 11, 20, 11, 0, 0, 0, time.UTC)

// broadcast is a broadcast: wait returns a channel that the next fire closes.
type broadcast struct {
	mu sync.Mutex
	ch chan struct{}
}

func (s *broadcast) wait() <-chan struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ch == nil {
		s.ch = make(chan struct{})
	}
	return s.ch
}

func (s *broadcast) fire() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ch != nil {
		close(s.ch)
		s.ch = nil
	}
}

// pacer is the run's virtual clock. Before each engine step it moves the
// shared FakeClock to the step's time, runs the void job on every 15 virtual
// seconds it crosses, and checks the occupancy invariant. It parks the
// engine, between steps, at the limit set by runTo.
type pacer struct {
	*simulator.StepClock
	e      *deviceEnv
	voidAt time.Time // next void tick
	steps  int       // engine goroutine only until parked, then read by the test
	moved  broadcast // fired after every step boundary and every park

	mu     sync.Mutex
	limit  time.Time
	parked bool
	lift   broadcast // fired when the limit changes
}

func newPacer(e *deviceEnv, start time.Time) *pacer {
	e.clk.Set(start)
	return &pacer{StepClock: simulator.NewStepClock(start), e: e, limit: start, voidAt: start.Add(voidTick)}
}

func (p *pacer) WaitUntil(ctx context.Context, at time.Time) error {
	for {
		p.mu.Lock()
		if !at.After(p.limit) {
			p.parked = false
			p.mu.Unlock()
			break
		}
		p.parked = true
		ch := p.lift.wait()
		p.mu.Unlock()
		p.moved.fire()
		select {
		case <-ch:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	for !p.voidAt.After(at) {
		if err := p.StepClock.WaitUntil(ctx, p.voidAt); err != nil {
			return err
		}
		p.e.clk.Set(p.voidAt)
		if _, err := gate.VoidUnpassedEntries(ctx, p.e.d.pool, p.voidAt); err != nil && ctx.Err() == nil {
			p.e.t.Errorf("void job at %s: %v", p.voidAt, err)
		}
		p.voidAt = p.voidAt.Add(voidTick)
	}
	if err := p.StepClock.WaitUntil(ctx, at); err != nil {
		return err
	}
	p.e.clk.Set(p.StepClock.Now())
	p.steps++
	p.checkInvariants(ctx)
	p.moved.fire()
	return nil
}

// checkInvariants asserts, between two steps: occupied never exceeds the
// effective capacity, and the counter equals the active and paid stays. It
// runs on the engine goroutine, so it reports with Errorf, never Fatalf.
func (p *pacer) checkInvariants(ctx context.Context) {
	if ctx.Err() != nil {
		return
	}
	var occupied, inside, capacity int64
	err := p.e.d.pool.QueryRow(ctx,
		`select o.occupied, (select count(*) from stays s where s.lot_id = o.lot_id and s.status in ('active', 'paid')),
		        l.capacity - (select count(*) from spaces s where s.lot_id = l.id and s.status = 'out_of_service')
		 from lot_occupancy o join lots l on l.id = o.lot_id where o.lot_id = $1`, p.e.lotID).Scan(&occupied, &inside, &capacity)
	if err != nil {
		if ctx.Err() == nil {
			p.e.t.Errorf("read occupancy: %v", err)
		}
		return
	}
	if occupied != inside {
		p.e.t.Errorf("step %d at %s: occupancy counter %d, active or paid stays %d", p.steps, p.StepClock.Now(), occupied, inside)
	}
	if occupied > capacity {
		p.e.t.Errorf("step %d at %s: occupied %d over effective capacity %d", p.steps, p.StepClock.Now(), occupied, capacity)
	}
}

// runTo lets the engine run up to virtual time t and returns once it is
// parked there, between two steps.
func (p *pacer) runTo(t *testing.T, to time.Time) {
	t.Helper()
	p.mu.Lock()
	p.limit, p.parked = to, false
	p.lift.fire()
	p.mu.Unlock()
	timeout := time.After(settleWait)
	for {
		ch := p.moved.wait()
		p.mu.Lock()
		done := p.parked && p.limit.Equal(to)
		p.mu.Unlock()
		if done {
			return
		}
		select {
		case <-ch:
		case <-timeout:
			t.Fatalf("the engine did not reach %s (virtual now %s)", to, p.StepClock.Now())
		case <-t.Context().Done():
			t.Fatal("test ended before the engine parked")
		}
	}
}

// release removes the limit for good: the run is stopping and must drain.
func (p *pacer) release() {
	p.mu.Lock()
	p.limit = time.Date(9999, 1, 1, 0, 0, 0, 0, time.UTC)
	p.lift.fire()
	p.mu.Unlock()
}

// traceTap records every device request and answer, scrubbed of what the
// server invents (event ids, stay ids, ticket codes), and tells the test when
// something moved.
type traceTap struct {
	next   http.RoundTripper
	names  map[string]string
	moved  broadcast
	mu     sync.Mutex
	lines  []string
	codes  map[string]struct{} // every ticket code issued
	cards  map[string]struct{} // every card uid presented
	errors []string            // bodies of answers >= 400
	count  map[string]int      // "<type> <result> <reason>" -> n
}

var uuidPattern = regexp.MustCompile(`[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`)

func newTraceTap(next http.RoundTripper) *traceTap {
	t := &traceTap{next: next, names: map[string]string{}, codes: map[string]struct{}{}, cards: map[string]struct{}{}, count: map[string]int{}}
	for _, d := range devicefleet.Devices() {
		t.names[d.PublicID.String()] = d.Name
	}
	return t
}

func (c *traceTap) RoundTrip(r *http.Request) (*http.Response, error) {
	raw, _ := io.ReadAll(r.Body)
	r.Body = io.NopCloser(bytes.NewReader(raw))
	res, err := c.next.RoundTrip(r)
	if err != nil {
		return res, err
	}
	reply, _ := io.ReadAll(res.Body)
	res.Body = io.NopCloser(bytes.NewReader(reply))

	var req struct {
		Type       string          `json:"type"`
		OccurredAt string          `json:"occurred_at"`
		Payload    json.RawMessage `json:"payload"`
	}
	var ans struct {
		Data struct {
			Result     string `json:"result"`
			Reason     string `json:"reason"`
			TicketCode string `json:"ticket_code"`
		} `json:"data"`
	}
	_ = json.Unmarshal(raw, &req)
	_ = json.Unmarshal(reply, &ans)

	c.mu.Lock()
	if ans.Data.TicketCode != "" {
		c.codes[ans.Data.TicketCode] = struct{}{}
	}
	var pl struct {
		Kind  string `json:"kind"`
		Value string `json:"value"`
	}
	if json.Unmarshal(req.Payload, &pl) == nil && pl.Kind == gate.KindEmployeeCard && pl.Value != "" {
		c.cards[pl.Value] = struct{}{}
	}
	payload := uuidPattern.ReplaceAllString(string(req.Payload), "<id>")
	for code := range c.codes {
		payload = strings.ReplaceAll(payload, code, "<code>")
	}
	c.lines = append(c.lines, strings.Join([]string{
		req.OccurredAt, c.names[r.Header.Get("X-Device-Id")], req.Type, payload,
		http.StatusText(res.StatusCode), ans.Data.Result, ans.Data.Reason,
	}, "|"))
	c.count[req.Type+" "+ans.Data.Result+" "+ans.Data.Reason]++
	if res.StatusCode >= http.StatusBadRequest {
		c.errors = append(c.errors, string(reply))
	}
	c.mu.Unlock()
	c.moved.fire()
	return res, nil
}

func (c *traceTap) trace() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.lines...)
}

// n counts the answers whose type, result and reason match the prefix.
func (c *traceTap) n(prefix string) (n int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for k, v := range c.count {
		if strings.HasPrefix(k, prefix) {
			n += v
		}
	}
	return n
}

// scenarioRig is the glue of every scenario test: the API handler, the
// seeded lot, fleet and cards, and a simulator runner on a paced step clock.
type scenarioRig struct {
	*deviceEnv
	runner   *simulator.Runner
	log      *simulator.EventLog
	pace     *pacer
	tap      *traceTap
	client   *simulator.DeviceClient
	publicID uuid.UUID
}

// newScenarioRig builds the rig with the shared clock at start. Unlike the
// brief's scenarioEnv tuple it also hands back the tap, pacer and event log
// the tests need.
func newScenarioRig(t *testing.T, start time.Time, mods ...func(*deps)) *scenarioRig {
	t.Helper()
	e := newDeviceEnv(t, mods...)
	q := dbgen.New(e.db.App)
	if _, err := devices.SeedFleet(t.Context(), q, e.d.sealer, e.lotID, simFleet); err != nil {
		t.Fatalf("seed fleet: %v", err)
	}
	if _, err := gate.SeedCards(t.Context(), q, e.lotID, simFleet, e.cfg.CardUIDPepper()); err != nil {
		t.Fatalf("seed cards: %v", err)
	}
	var publicID uuid.UUID
	if err := e.db.App.QueryRow(t.Context(), "select public_id from lots where id = $1", e.lotID).Scan(&publicID); err != nil {
		t.Fatal(err)
	}

	pace := newPacer(e, start)
	tap := newTraceTap(simulator.HandlerTransport(e.h))
	noSleep := func(ctx context.Context, _ time.Duration) error { return ctx.Err() }
	client := simulator.NewDeviceClient(simulator.ClientConfig{
		BaseURL: "http://api.test", Transport: tap, SignClock: e.clk, FleetSecret: simFleet, Sleep: noSleep,
	})
	log := simulator.NewEventLog(20000)
	runner := simulator.NewRunner(simulator.RunnerDeps{
		Client: client, FleetSecret: simFleet, Log: log,
		// The drain cap is a wall-clock bound: it must not move with virtual time.
		Wall:     testsupport.NewFakeClock(start),
		Spaces:   simulator.PublicSpaces(&http.Client{Transport: simulator.HandlerTransport(e.h)}, "http://api.test"),
		NewClock: func(time.Time, int) simulator.Clock { return pace },
	})
	t.Cleanup(func() {
		_, _ = runner.Stop(false)
		pace.release()
		runner.Wait()
	})
	return &scenarioRig{deviceEnv: e, runner: runner, log: log, pace: pace, tap: tap, client: client, publicID: publicID}
}

func (r *scenarioRig) start(sc simulator.Scenario, seed uint64) {
	r.t.Helper()
	if _, err := r.runner.Start(r.t.Context(), simulator.StartParams{
		Scenario: sc, Seed: seed, Speed: simulator.DefaultSpeed, StartAt: r.pace.Now(),
	}); err != nil {
		r.t.Fatalf("start %s: %v", sc, err)
	}
}

// waitFor returns once cond holds. cond is re-read after every request and
// step; recheck covers a state change that moves nothing (a flag set by the
// engine in a step that sends no request). Runner.Wait cannot replace it:
// fill_to_full does not end its run. After the lot is full the engine goes
// idle and the run stays open for injects and leaves, so Wait would never
// return until a stop.
func (r *scenarioRig) waitFor(what string, cond func() bool) {
	r.t.Helper()
	const recheck = 2 * time.Millisecond
	timeout := time.After(settleWait)
	for {
		tapMoved, stepMoved := r.tap.moved.wait(), r.pace.moved.wait()
		if cond() {
			return
		}
		select {
		case <-tapMoved:
		case <-stepMoved:
		case <-time.After(recheck):
		case <-timeout:
			r.t.Fatalf("timed out waiting for %s", what)
		}
	}
}

// availability reads the public availability document.
func (r *scenarioRig) availability() map[string]any {
	r.t.Helper()
	rec := r.serve(jsonReq("GET", "/v1/lots/"+r.publicID.String()+"/availability", nil), "")
	return decodeBody(r.t, rec)["data"].(map[string]any)
}

// TestScenario_FillToFull: fill_to_full reaches 0 available over the real API
// and database, the dispenser then denies with gate.lot_full, and the
// occupancy invariant holds (M2 exit criterion).
func TestScenario_FillToFull(t *testing.T) {
	r := newScenarioRig(t, time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC))
	r.pace.release() // no limit: the scenario ends itself
	r.start(simulator.ScenarioFillToFull, 42)

	// Settled: the lot is full and every car has reported its space, so the
	// checks below cut nothing mid-flight.
	r.waitFor("the lot to fill", func() bool {
		return r.runner.Status().ReachedFull && r.tap.n("sensor.space_changed ack") == 280
	})

	st := r.runner.Status()
	if !st.ReachedFull || st.CarsInside != 280 {
		t.Errorf("status = %+v, want ReachedFull with 280 cars", st)
	}
	if got := r.availability(); got["available"] != 0.0 || got["occupied"] != 280.0 {
		t.Errorf("availability = %v, want 0 available, 280 occupied", got)
	}
	if n := r.count(`select count(*) from stays where status = 'active'`); n != 280 {
		t.Errorf("active stays = %d, want 280", n)
	}
	rep, err := r.client.Send(t.Context(), devicefleet.ByKind(authz.DeviceDispenser), simulator.Outgoing{
		EventID: uuid.New(), Type: "entry.credential_presented", Payload: map[string]any{"kind": "ticket_request"},
	})
	if err != nil || rep.Result != "deny" || rep.Reason != "gate.lot_full" {
		t.Errorf("next ticket_request = %+v, %v; want deny gate.lot_full", rep, err)
	}
	for _, entry := range r.log.After(0) {
		if entry.Result == "error" || entry.Result == "rejected" {
			t.Fatalf("unexpected answer: %+v", entry)
		}
	}
	r.invariant()
}

// TestScenario_WeekdayInvariants: three virtual hours of weekday traffic from
// 07:00. The pacer checks the invariants after every engine step; a drain
// then empties the lot and the money is on the books.
func TestScenario_WeekdayInvariants(t *testing.T) {
	r := newScenarioRig(t, weekdayStart)
	r.start(simulator.ScenarioWeekday, 42)
	r.pace.runTo(t, weekdayStart.Add(3*time.Hour))

	if r.pace.steps < 100 {
		t.Fatalf("only %d engine steps ran; the invariant checks would prove little", r.pace.steps)
	}
	if got := r.availability(); got["occupied"].(float64) > got["effective_capacity"].(float64) {
		t.Errorf("availability = %v, occupied over effective capacity", got)
	}
	r.invariant()

	// Close the run first, then lift the limit so the drain can finish.
	if _, err := r.runner.StopAsync(true); err != nil {
		t.Fatalf("stop: %v", err)
	}
	r.pace.release()
	r.runner.Wait()

	if st := r.runner.Status(); st.CarsInside != 0 || st.Running {
		t.Errorf("after the drain: %+v, want no cars and not running", st)
	}
	if got := r.availability(); got["occupied"] != 0.0 {
		t.Errorf("after the drain availability = %v, want 0 occupied", got)
	}
	r.invariant()
	if n := r.count(`select count(*) from payments where method = 'cash' and amount_cents > 0`); n < 1 {
		t.Errorf("cash payments with an amount = %d, want at least 1", n)
	}
	if n := r.count(`select count(*) from payments where method = 'none'`); n < 1 {
		t.Errorf("zero-amount payments = %d, want at least 1", n)
	}
	for _, entry := range r.log.After(0) {
		if entry.Result == "error" || entry.Result == "rejected" {
			t.Fatalf("unexpected answer: %+v", entry)
		}
	}
}

// TestScenario_Deterministic: one seed, two fresh databases, one request
// trace (FR-SIM-7). Server-invented values are scrubbed from the trace.
func TestScenario_Deterministic(t *testing.T) {
	run := func() []string {
		r := newScenarioRig(t, weekdayStart)
		r.start(simulator.ScenarioWeekday, 42)
		r.pace.runTo(t, weekdayStart.Add(2*time.Hour))
		return r.tap.trace()
	}
	a, b := run(), run()
	if len(a) < 100 {
		t.Fatalf("trace has %d requests; too few to compare", len(a))
	}
	for i := range min(len(a), len(b)) {
		if a[i] != b[i] {
			t.Fatalf("traces differ at request %d:\n  run 1: %s\n  run 2: %s", i, a[i], b[i])
		}
	}
	if len(a) != len(b) {
		t.Fatalf("trace lengths differ: %d vs %d", len(a), len(b))
	}
}
