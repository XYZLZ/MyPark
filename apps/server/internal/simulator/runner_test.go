package simulator

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/XYZLZ/MyPark/apps/server/internal/testsupport/fakeclock"
)

var monday7 = time.Date(2026, 11, 16, 7, 0, 0, 0, lotZone)

// haltClock is a StepClock that blocks at a horizon: an event past it waits
// until ctx is done, a new earlier event (the engine's wake), or release.
type haltClock struct {
	*StepClock
	until   atomic.Int64
	reached chan struct{}
	once    sync.Once
}

func (c *haltClock) WaitUntil(ctx context.Context, t time.Time) error {
	if t.UnixNano() > c.until.Load() {
		c.once.Do(func() { close(c.reached) })
		<-ctx.Done()
		return ctx.Err()
	}
	return c.StepClock.WaitUntil(ctx, t)
}

func (c *haltClock) release() { c.until.Store(1<<62 - 1) }

type runnerRig struct {
	wall *fakeclock.Clock
	api  *fakeAPI
	clk  *haltClock
	log  *EventLog
	run  *Runner
}

// newRunnerRig builds a runner over the fake API. The clock halts horizon
// after the scenario's start.
func newRunnerRig(t *testing.T, horizon time.Duration, spaces func(context.Context) ([]string, error)) *runnerRig {
	t.Helper()
	rig := &runnerRig{log: NewEventLog(5000), wall: fakeclock.New(t0)}
	rig.api = &fakeAPI{script: nil, used: map[string]int{}, creds: map[string]int{}}
	client := NewDeviceClient(ClientConfig{
		BaseURL: "http://api.test", Transport: HandlerTransport(rig.api),
		SignClock: fakeclock.New(t0), FleetSecret: fleetSecret,
	})
	rig.run = NewRunner(RunnerDeps{
		Client: client, FleetSecret: fleetSecret, Log: rig.log, Wall: rig.wall, Spaces: spaces,
		NewClock: func(start time.Time, _ int) Clock {
			rig.clk = &haltClock{StepClock: NewStepClock(start), reached: make(chan struct{})}
			rig.clk.until.Store(start.Add(horizon).UnixNano())
			rig.api.now = rig.clk.Now
			return rig.clk
		},
	})
	t.Cleanup(func() { _, _ = rig.run.Stop(false); rig.run.Wait() })
	return rig
}

func eventually(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestRunner_StartStopErrors(t *testing.T) {
	rig := newRunnerRig(t, time.Hour, nil)
	ctx := t.Context()
	if _, err := rig.run.Stop(false); !errors.Is(err, ErrNotRunning) {
		t.Errorf("Stop idle = %v, want ErrNotRunning", err)
	}
	for _, s := range []Scenario{ScenarioReservationArrivals, ScenarioNoShow} {
		if _, err := rig.run.Start(ctx, StartParams{Speed: 30, Scenario: s}); !errors.Is(err, ErrScenarioUnavailable) {
			t.Errorf("Start(%s) = %v, want ErrScenarioUnavailable", s, err)
		}
	}
	for _, p := range []StartParams{{Scenario: ScenarioWeekday}, {Scenario: ScenarioWeekday, Speed: 121}, {Scenario: ScenarioWeekday, Speed: -1}, {Speed: 30, Scenario: "nope"}, {Speed: 30}, {Speed: 30, Scenario: ScenarioWeekday, Seed: 1 << 63}} {
		if _, err := rig.run.Start(ctx, p); !errors.Is(err, ErrInvalidParams) {
			t.Errorf("Start(%+v) = %v, want ErrInvalidParams", p, err)
		}
	}
	if st := rig.run.Status(); st.Running || st.Speed != DefaultSpeed {
		t.Fatalf("status before any run = %+v, want idle with speed %d (the contract minimum is 1)", st, DefaultSpeed)
	}

	st, err := rig.run.Start(ctx, StartParams{Speed: 30, Scenario: ScenarioWeekday, Seed: 5, StartAt: monday7})
	if err != nil {
		t.Fatal(err)
	}
	if !st.Running || st.Scenario != ScenarioWeekday || st.Seed != 5 || st.Speed != 30 || !st.VirtualNow.Equal(monday7) || st.StartedAt.IsZero() {
		t.Errorf("start status = %+v (speed defaults to 30)", st)
	}
	if _, err := rig.run.Start(ctx, StartParams{Speed: 30, Scenario: ScenarioIdle}); !errors.Is(err, ErrAlreadyRunning) {
		t.Errorf("second Start = %v, want ErrAlreadyRunning", err)
	}
	if _, err := rig.run.Stop(false); err != nil {
		t.Fatal(err)
	}
	if _, err := rig.run.Stop(false); !errors.Is(err, ErrNotRunning) {
		t.Errorf("second Stop = %v, want ErrNotRunning", err)
	}
	if st := rig.run.Status(); st.Running || st.Scenario != ScenarioWeekday || st.Seed != 5 {
		t.Errorf("status after stop = %+v, want the last scenario, not running", st)
	}
	if err := rig.run.Leave("car-000001", false); !errors.Is(err, ErrNotRunning) {
		t.Errorf("Leave idle = %v, want ErrNotRunning", err)
	}
}

func TestRunner_DefaultStartAt(t *testing.T) {
	wall := time.Date(2026, 11, 16, 15, 30, 0, 0, lotZone) // a Monday afternoon in the lot's zone
	var got []time.Time
	r := NewRunner(RunnerDeps{
		Client:      NewDeviceClient(ClientConfig{BaseURL: "http://api.test", Transport: HandlerTransport(http.NotFoundHandler()), SignClock: fakeclock.New(t0), FleetSecret: fleetSecret}),
		FleetSecret: fleetSecret, Log: NewEventLog(10), Wall: fakeclock.New(wall),
		NewClock: func(start time.Time, _ int) Clock {
			got = append(got, start)
			return &haltClock{StepClock: NewStepClock(start), reached: make(chan struct{})}
		},
	})
	for _, s := range []Scenario{ScenarioWeekday, ScenarioWeekendPeak, ScenarioFillToFull} {
		if _, err := r.Start(t.Context(), StartParams{Speed: 30, Scenario: s}); err != nil {
			t.Fatal(err)
		}
		if _, err := r.Stop(false); err != nil {
			t.Fatal(err)
		}
	}
	want := []time.Time{monday7, monday7, wall}
	for i := range want {
		if !got[i].Equal(want[i]) {
			t.Errorf("start %d = %v, want %v", i, got[i], want[i])
		}
	}
}

// requestTrace is the device request sequence without server-issued codes.
func requestTrace(a *fakeAPI) []string {
	var out []string
	for _, c := range a.snapshot() {
		p := map[string]any{}
		for k, v := range c.Payload {
			if k == "stay_id" {
				continue
			}
			if cred, ok := v.(map[string]any); ok && k == "credential" {
				v = cred["kind"]
			}
			p[k] = v
		}
		out = append(out, fmt.Sprintf("%s %s %v", c.Kind, c.Type, p))
	}
	return out
}

func TestRunner_SameSeedSameTrace(t *testing.T) {
	run := func(seed uint64) []string {
		rig := newRunnerRig(t, 2*time.Hour, nil)
		if _, err := rig.run.Start(t.Context(), StartParams{Speed: 30, Scenario: ScenarioWeekday, Seed: seed, StartAt: monday7}); err != nil {
			t.Fatal(err)
		}
		<-rig.clk.reached
		if _, err := rig.run.Stop(false); err != nil {
			t.Fatal(err)
		}
		return requestTrace(rig.api)
	}
	a, b, c := run(42), run(42), run(43)
	if len(a) < 100 {
		t.Fatalf("only %d requests in 2 virtual hours: the scenario barely ran", len(a))
	}
	if !slices.Equal(a, b) {
		t.Errorf("seed 42 traces differ (%d vs %d requests)", len(a), len(b))
	}
	if slices.Equal(a, c) {
		t.Error("seeds 42 and 43 give the same trace")
	}
	cards := 0
	for _, l := range a {
		if strings.HasPrefix(l, "entry_barrier entry.credential_presented") {
			cards++
		}
	}
	if cards == 0 {
		t.Error("no employee card reached the entry barrier in the 07:30-08:30 window")
	}
}

func TestRunner_FillToFullStopsAfterThreeDenials(t *testing.T) {
	rig := newRunnerRig(t, 100*time.Hour, nil)
	rig.api.fullAfter = 10 // the 10th entry and every later one is denied
	if _, err := rig.run.Start(t.Context(), StartParams{Speed: 30, Scenario: ScenarioFillToFull, Seed: 1, StartAt: monday7}); err != nil {
		t.Fatal(err)
	}
	eventually(t, "reached_full", func() bool { return rig.run.Status().ReachedFull })
	eventually(t, "nine cars parked", func() bool { return rig.run.Status().CarsInside == 9 })

	st, err := rig.run.Stop(false)
	if err != nil {
		t.Fatal(err)
	}
	// Without the stop rule the arrivals would run to the 100 h horizon (tens
	// of thousands of cars), so a handful of denials proves arrivals ended.
	denied := 0
	for _, e := range rig.log.After(0) {
		if e.Reason == "gate.lot_full" {
			denied++
		}
	}
	if denied < 3 || denied > 6 {
		t.Errorf("lot_full denials = %d, want 3 (a few more only for cars already at the dispenser)", denied)
	}
	if !st.ReachedFull || st.CarsInside != 9 {
		t.Errorf("status = %+v, want reached_full with 9 cars", st)
	}
}

func TestRunner_DrainEmptiesLot(t *testing.T) {
	rig := newRunnerRig(t, time.Hour, nil)
	if _, err := rig.run.Start(t.Context(), StartParams{Speed: 30, Scenario: ScenarioWeekday, Seed: 9, StartAt: monday7}); err != nil {
		t.Fatal(err)
	}
	<-rig.clk.reached
	if rig.run.Status().CarsInside == 0 {
		t.Fatal("no cars inside after a virtual hour: nothing to drain")
	}
	rig.clk.release() // the drain's exits lie past the horizon
	st, err := rig.run.Stop(true)
	if err != nil {
		t.Fatal(err)
	}
	if st.Running || st.CarsInside != 0 {
		t.Errorf("status after drain = %+v", st)
	}
	last := map[string]LogEntry{}
	for _, e := range rig.log.After(0) {
		last[e.CarID] = e
	}
	for id, e := range last {
		if e.Type != "sensor.space_changed" {
			t.Errorf("%s ended on %s/%s, want a sensor event", id, e.Device, e.Type)
		}
	}
	net := map[string]int{}
	for _, c := range rig.api.snapshot() {
		if c.Type == "sensor.space_changed" {
			if c.Payload["occupied"] == true {
				net[fmt.Sprint(c.Payload["space_code"])]++
			} else {
				net[fmt.Sprint(c.Payload["space_code"])]--
			}
		}
	}
	for code, n := range net {
		if n != 0 {
			t.Errorf("space %s ends %d occupied events ahead", code, n)
		}
	}
}

// A Leave on a held car must run after the engine's queue emptied, and while
// it runs (the control API calls from other goroutines).
func TestRunner_LeaveAfterIdleAndConcurrently(t *testing.T) {
	rig := newRunnerRig(t, 100*time.Hour, nil)
	rig.api.fullAfter = 4
	if _, err := rig.run.Start(t.Context(), StartParams{Speed: 30, Scenario: ScenarioFillToFull, Seed: 2, StartAt: monday7}); err != nil {
		t.Fatal(err)
	}
	// car-000001 may not exist yet: hammer Leave from several goroutines while
	// the engine runs, with Status reads in between.
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			for {
				err := rig.run.Leave("car-000001", false)
				if err == nil {
					return
				}
				if !errors.Is(err, ErrCarNotFound) {
					t.Errorf("Leave = %v", err)
					return
				}
				_ = rig.run.Status()
			}
		})
	}
	wg.Wait()
	eventually(t, "reached_full", func() bool { return rig.run.Status().ReachedFull })
	left := func(car string) bool {
		for _, e := range rig.log.After(0) {
			if e.CarID == car && e.Type == "barrier.passed" && e.Device == "barrier-exit-1" {
				return true
			}
		}
		return false
	}
	eventually(t, "car-000001 out", func() bool { return left("car-000001") })
	eventually(t, "the other cars parked", func() bool { return rig.run.Status().CarsInside == 2 })

	// The engine has nothing left to do: this Leave must wake it.
	if err := rig.run.Leave("car-000002", false); err != nil {
		t.Fatal(err)
	}
	eventually(t, "car-000002 out", func() bool { return left("car-000002") })
	if err := rig.run.Leave("car-000099", false); !errors.Is(err, ErrCarNotFound) {
		t.Errorf("Leave unknown = %v, want ErrCarNotFound", err)
	}
}

func TestRunner_InjectHeldCarAndLeave(t *testing.T) {
	rig := newRunnerRig(t, 100*time.Hour, nil)
	if _, err := rig.run.Start(t.Context(), StartParams{Speed: 30, Scenario: ScenarioFillToFull, Seed: 4, StartAt: monday7}); err != nil {
		t.Fatal(err)
	}
	id, err := rig.run.Inject(InjectParams{Cred: CredEmployeeCard, Card: "EMP-007", Hold: true})
	if err != nil || id == "" {
		t.Fatalf("Inject = %q, %v", id, err)
	}
	if _, err := rig.run.Inject(InjectParams{Cred: CredEmployeeCard, Card: "bad"}); !errors.Is(err, ErrInvalidParams) {
		t.Errorf("Inject bad card = %v, want ErrInvalidParams", err)
	}
	if err := rig.run.SetFault("E001", FaultSilent); err != nil {
		t.Errorf("SetFault = %v", err)
	}
	if err := rig.run.SetFault("ZZZ", FaultSilent); !errors.Is(err, ErrUnknownSpace) {
		t.Errorf("SetFault unknown = %v, want ErrUnknownSpace", err)
	}
	if err := rig.run.Leave(id, false); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the injected car out", func() bool {
		for _, e := range rig.log.After(0) {
			if e.CarID == id && e.Type == "barrier.passed" && e.Device == "barrier-exit-1" {
				return true
			}
		}
		return false
	})
}

func TestRunner_SetsLotSpaces(t *testing.T) {
	rig := newRunnerRig(t, time.Hour, func(context.Context) ([]string, error) { return []string{"Z1", "Z2"}, nil })
	if _, err := rig.run.Start(t.Context(), StartParams{Speed: 30, Scenario: ScenarioFillToFull, Seed: 1, StartAt: monday7}); err != nil {
		t.Fatal(err)
	}
	eventually(t, "two sensor events", func() bool { return len(rig.api.payloads("sensor.space_changed")) >= 2 })
	for _, p := range rig.api.payloads("sensor.space_changed") {
		if c := p["space_code"]; c != "Z1" && c != "Z2" {
			t.Errorf("sensor event for %v, want one of the lot's spaces", c)
		}
	}
	failing := newRunnerRig(t, time.Hour, func(context.Context) ([]string, error) { return nil, errors.New("api down") })
	if _, err := failing.run.Start(t.Context(), StartParams{Speed: 30, Scenario: ScenarioIdle}); err == nil {
		t.Error("Start succeeded although the lot's spaces could not be read")
	}
	if failing.run.Status().Running {
		t.Error("a failed Start left a run")
	}
}

func TestPublicSpaces_ReadsTheFirstLot(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/lots", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"id":"lot-1"}],"meta":{}}`))
	})
	mux.HandleFunc("GET /v1/lots/lot-1/spaces", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":{"spaces":[{"code":"A1","state":"free"},{"code":"A2","state":"occupied"}]},"meta":{}}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	got, err := PublicSpaces(srv.Client(), srv.URL)(t.Context())
	if err != nil || !slices.Equal(got, []string{"A1", "A2"}) {
		t.Errorf("PublicSpaces = %v, %v", got, err)
	}
	if _, err := PublicSpaces(srv.Client(), srv.URL+"/nope")(t.Context()); err == nil {
		t.Error("PublicSpaces succeeded against a 404")
	}
}

func TestRunner_IdleSendsHeartbeats(t *testing.T) {
	rig := newRunnerRig(t, 10*time.Minute, nil)
	if _, err := rig.run.Start(t.Context(), StartParams{Scenario: ScenarioIdle, Seed: 1, StartAt: monday7, Speed: 1}); err != nil {
		t.Fatal(err)
	}
	<-rig.clk.reached
	byDevice := map[string]int{}
	for _, c := range rig.api.snapshot() {
		if c.Type != "heartbeat" {
			t.Errorf("idle sent %s", c.Type)
		}
		byDevice[string(c.Kind)]++
	}
	// 10 minutes at one heartbeat a minute, the first at the start: 11 each.
	if len(byDevice) != 6 {
		t.Fatalf("heartbeats from %d device kinds, want 6: %v", len(byDevice), byDevice)
	}
	for k, n := range byDevice {
		if n != 11 {
			t.Errorf("%s sent %d heartbeats, want 11", k, n)
		}
	}
}

func TestRunner_SensorDriftRuns(t *testing.T) {
	rig := newRunnerRig(t, 2*time.Hour, nil)
	if _, err := rig.run.Start(t.Context(), StartParams{Speed: 30, Scenario: ScenarioSensorDrift, Seed: 3, StartAt: monday7}); err != nil {
		t.Fatal(err)
	}
	<-rig.clk.reached
	entries, sensors := len(rig.api.payloads("entry.credential_presented")), len(rig.api.payloads("sensor.space_changed"))
	if entries == 0 || sensors == 0 {
		t.Errorf("entries = %d, sensor events = %d: the scenario did not run", entries, sensors)
	}
}

func exited(rig *runnerRig, car string) bool {
	for _, e := range rig.log.After(0) {
		if e.CarID == car && e.Type == "barrier.passed" && e.Device == "barrier-exit-1" {
			return true
		}
	}
	return false
}

func TestRunner_InjectWhileIdleStartsAnImplicitSession(t *testing.T) {
	rig := newRunnerRig(t, 2*time.Hour, nil)
	id, err := rig.run.Inject(InjectParams{Cred: CredTicket, Hold: true})
	if err != nil || id != "car-000001" {
		t.Fatalf("Inject = %q, %v, want car-000001", id, err)
	}
	if st := rig.run.Status(); !st.Running || st.Scenario != ScenarioIdle || st.Speed != 1 {
		t.Errorf("status = %+v, want a running idle session at speed 1", st)
	}
	parked := func() bool {
		for _, e := range rig.log.After(0) {
			if e.CarID == id && e.Type == "sensor.space_changed" {
				return true
			}
		}
		return false
	}
	eventually(t, "the car to park", parked)
	if rig.run.Status().CarsInside != 1 {
		t.Fatalf("cars inside = %d, want 1", rig.run.Status().CarsInside)
	}

	// The demo path: the weekday scenario takes the session over, keeping the car.
	st, err := rig.run.Start(t.Context(), StartParams{Speed: 30, Scenario: ScenarioWeekday, Seed: 5, StartAt: monday7})
	if err != nil {
		t.Fatalf("Start over the implicit session = %v", err)
	}
	if st.Scenario != ScenarioWeekday || st.Speed != 30 || st.Seed != 5 || st.VirtualNow.Before(monday7) {
		t.Errorf("status after takeover = %+v", st)
	}
	<-rig.clk.reached
	if exited(rig, id) {
		t.Fatal("the held car left by itself")
	}
	rig.clk.until.Store(rig.clk.Now().Add(30 * time.Minute).UnixNano())
	if err := rig.run.Leave(id, false); err != nil {
		t.Fatalf("Leave after takeover = %v", err)
	}
	eventually(t, "the held car out", func() bool { return exited(rig, id) })
	if _, err := rig.run.Start(t.Context(), StartParams{Speed: 30, Scenario: ScenarioIdle}); !errors.Is(err, ErrAlreadyRunning) {
		t.Errorf("a second Start = %v, want ErrAlreadyRunning", err)
	}
}

func TestRunner_ImplicitSessionTakesLeaveFaultAndStop(t *testing.T) {
	rig := newRunnerRig(t, 100*time.Hour, nil)
	id, err := rig.run.Inject(InjectParams{Cred: CredEmployeeCard, Card: "EMP-003", Hold: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := rig.run.SetFault("E001", FaultSilent); err != nil {
		t.Errorf("SetFault = %v", err)
	}
	if err := rig.run.Leave(id, false); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the car out", func() bool { return exited(rig, id) })
	st, err := rig.run.Stop(false)
	if err != nil || st.Running {
		t.Errorf("Stop = %+v, %v; want the implicit session stopped", st, err)
	}
	if _, err := rig.run.Stop(false); !errors.Is(err, ErrNotRunning) {
		t.Errorf("second Stop = %v, want ErrNotRunning", err)
	}
}

func TestRunner_DrainFillToFullLeavesNoCarInside(t *testing.T) {
	rig := newRunnerRig(t, 100*time.Hour, nil)
	rig.api.fullAfter = 40
	// After each Leave, let the engine run that car out and go idle: a drain
	// marked before every Leave is queued would end at the first idle engine,
	// with cars still to send out.
	leaves := 0
	rig.run.afterDrainLeave = func(s *session) {
		leaves++
		eventually(t, "an idle engine", func() bool { return s.idle.Load() && s.eng.pending() == 0 })
		select {
		case <-s.drained:
			t.Fatalf("the drain ended after %d of 39 Leaves", leaves)
		default:
		}
	}
	if _, err := rig.run.Start(t.Context(), StartParams{Speed: 30, Scenario: ScenarioFillToFull, Seed: 6, StartAt: monday7}); err != nil {
		t.Fatal(err)
	}
	eventually(t, "a full lot", func() bool { st := rig.run.Status(); return st.ReachedFull && st.CarsInside == 39 })
	st, err := rig.run.Stop(true)
	if err != nil {
		t.Fatal(err)
	}
	if st.Running || st.CarsInside != 0 {
		t.Fatalf("status after drain = %+v, want 0 cars inside", st)
	}
	for n := 1; n <= 39; n++ {
		if id := fmt.Sprintf("car-%06d", n); !exited(rig, id) {
			t.Errorf("%s never left", id)
		}
	}
}

// stallDrain fills the lot with five held cars, blocks every exit past the
// current virtual time and starts Stop(true) in the background. It returns
// once every car has paid, so the drain is under way and cannot finish.
func stallDrain(t *testing.T, rig *runnerRig) <-chan Status {
	t.Helper()
	out := make(chan Status, 1)
	stallDrainWith(t, rig, func() {
		go func() {
			st, _ := rig.run.Stop(true)
			out <- st
		}()
	})
	return out
}

// stallDrainWith is stallDrain with the stop call given by the caller.
func stallDrainWith(t *testing.T, rig *runnerRig, stop func()) {
	t.Helper()
	rig.api.fullAfter = 6
	if _, err := rig.run.Start(t.Context(), StartParams{Speed: 30, Scenario: ScenarioFillToFull, Seed: 8, StartAt: monday7}); err != nil {
		t.Fatal(err)
	}
	eventually(t, "a full lot", func() bool { st := rig.run.Status(); return st.ReachedFull && st.CarsInside == 5 })
	eventually(t, "five parked cars", func() bool { return len(rig.api.payloads("sensor.space_changed")) == 5 })
	rig.clk.until.Store(rig.clk.Now().UnixNano())
	stop()
	eventually(t, "five scans", func() bool { return len(rig.api.payloads("paystation.scan")) == 5 })
}

func TestRunner_DrainGivesUpAfterTheCapOnTheWallClock(t *testing.T) {
	rig := newRunnerRig(t, 100*time.Hour, nil)
	out := stallDrain(t, rig)
	deadline := time.Now().Add(10 * time.Second)
	for {
		rig.wall.Advance(drainTimeout) // the cap is on the injected wall clock
		select {
		case st := <-out:
			if st.Running || st.CarsInside != 5 {
				t.Errorf("status after the cap = %+v, want stopped with the 5 stuck cars", st)
			}
			return
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("Stop(true) ignored the wall clock cap")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestRunner_StopDuringADrainIsAHardStop(t *testing.T) {
	rig := newRunnerRig(t, 100*time.Hour, nil)
	out := stallDrain(t, rig)
	if st, err := rig.run.Stop(false); err != nil || st.Running {
		t.Fatalf("Stop(false) during a drain = %+v, %v; want a hard stop", st, err)
	}
	if st := <-out; st.Running {
		t.Errorf("the drain returned %+v after the hard stop", st)
	}
	if _, err := rig.run.Stop(true); !errors.Is(err, ErrNotRunning) {
		t.Errorf("Stop after the stop = %v, want ErrNotRunning", err)
	}
}

// A drain with cars the server will not let out must end when nothing can move
// any more: the employee chain must not keep the queue alive.
func TestRunner_DrainEndsWhenStuckCarsLeaveNothingToRun(t *testing.T) {
	rig := newRunnerRig(t, time.Hour, nil)
	rig.api.script = map[string][]string{"exit.credential_presented": {denyNotAllowed}}
	if _, err := rig.run.Start(t.Context(), StartParams{Speed: 30, Scenario: ScenarioWeekday, Seed: 9, StartAt: monday7}); err != nil {
		t.Fatal(err)
	}
	<-rig.clk.reached
	rig.clk.release()
	out := make(chan Status, 1)
	go func() {
		st, _ := rig.run.Stop(true)
		out <- st
	}()
	select {
	case st := <-out:
		if st.Running || st.CarsInside == 0 {
			t.Errorf("status = %+v, want stopped with the stuck cars still inside", st)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the drain never ended: the queue was kept alive")
	}
}

// Takeover runs in the engine goroutine: while a car step is in flight, Start
// waits, and the session keeps its old scenario until the step is done, so the
// step cannot schedule into the old time frame after the rebase.
func TestRunner_TakeoverWaitsForTheStepInFlight(t *testing.T) {
	rig := newRunnerRig(t, 100*time.Hour, nil)
	entered, release := make(chan struct{}), make(chan struct{})
	var once, released sync.Once
	unblock := func() { released.Do(func() { close(release) }) }
	t.Cleanup(unblock) // runs before the rig's Stop, which waits for the step
	rig.api.onCall = func(typ string) {
		if typ == "podium.vehicle_detected" {
			once.Do(func() { close(entered); <-release })
		}
	}
	if _, err := rig.run.Inject(InjectParams{Cred: CredTicket, Hold: true}); err != nil {
		t.Fatal(err)
	}
	<-entered
	out := make(chan Status, 1)
	go func() {
		st, err := rig.run.Start(t.Context(), StartParams{Speed: 30, Scenario: ScenarioWeekday, Seed: 5, StartAt: monday7})
		if err != nil {
			t.Errorf("Start = %v", err)
		}
		out <- st
	}()
	eventually(t, "Start to claim the session", rig.run.busy)
	if st := rig.run.Status(); st.Scenario != ScenarioIdle {
		t.Errorf("status while the step is in flight = %+v, want the implicit idle session untouched", st)
	}
	select {
	case st := <-out:
		t.Fatalf("Start returned %+v while a car step was in flight", st)
	default:
	}
	unblock()
	if st := <-out; st.Scenario != ScenarioWeekday || st.VirtualNow.Before(monday7) {
		t.Errorf("status after the takeover = %+v", st)
	}
}

// A closed idle session sends no more heartbeats, so a drain with a car the
// server will not let out ends by an empty queue, not by the wall clock cap.
func TestRunner_HeartbeatsStopOnceClosed(t *testing.T) {
	rig := newRunnerRig(t, time.Hour, nil)
	rig.api.script = map[string][]string{"exit.credential_presented": {denyNotAllowed}}
	if _, err := rig.run.Start(t.Context(), StartParams{Speed: 30, Scenario: ScenarioIdle, StartAt: monday7}); err != nil {
		t.Fatal(err)
	}
	if _, err := rig.run.Inject(InjectParams{Cred: CredTicket, Hold: true}); err != nil {
		t.Fatal(err)
	}
	<-rig.clk.reached
	rig.clk.release() // time runs free: only a closed run can end the heartbeats
	out := make(chan Status, 1)
	go func() {
		st, _ := rig.run.Stop(true)
		out <- st
	}()
	select {
	case st := <-out:
		if st.Running || st.CarsInside != 1 {
			t.Errorf("status = %+v, want stopped with the stuck car inside", st)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the drain never ended: heartbeats kept the queue alive")
	}
}

func TestRunner_StopAsyncReturnsAtOnceAndDrains(t *testing.T) {
	rig := newRunnerRig(t, 100*time.Hour, nil)
	var first Status
	stallDrainWith(t, rig, func() {
		var err error
		if first, err = rig.run.StopAsync(true); err != nil {
			t.Fatalf("StopAsync = %v", err)
		}
	})
	if !first.Running || !rig.run.Status().Running {
		t.Fatalf("StopAsync answered %+v, want the run still draining", first)
	}
	if _, err := rig.run.StopAsync(true); !errors.Is(err, ErrNotRunning) {
		t.Errorf("a second stop during the drain = %v, want ErrNotRunning", err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for rig.run.Status().Running {
		if time.Now().After(deadline) {
			t.Fatal("the background drain never ended at the cap")
		}
		rig.wall.Advance(drainTimeout)
		time.Sleep(time.Millisecond)
	}
	rig.run.Wait()
}

// When the implicit session is stopped while Start waits for its takeover,
// Start starts a fresh session instead of failing.
func TestRunner_TakeoverOfAStoppedSessionStartsFresh(t *testing.T) {
	rig := newRunnerRig(t, 100*time.Hour, nil)
	entered, release := make(chan struct{}), make(chan struct{})
	var once, released sync.Once
	unblock := func() { released.Do(func() { close(release) }) }
	t.Cleanup(unblock)
	rig.api.onCall = func(typ string) {
		if typ == "podium.vehicle_detected" {
			once.Do(func() { close(entered); <-release })
		}
	}
	if _, err := rig.run.Inject(InjectParams{Cred: CredTicket, Hold: true}); err != nil {
		t.Fatal(err)
	}
	<-entered
	type result struct {
		st  Status
		err error
	}
	out := make(chan result, 1)
	go func() {
		st, err := rig.run.Start(t.Context(), StartParams{Speed: 30, Scenario: ScenarioWeekday, Seed: 5, StartAt: monday7})
		out <- result{st, err}
	}()
	eventually(t, "Start to claim the session", rig.run.busy)
	stopped := make(chan struct{})
	go func() {
		_, _ = rig.run.Stop(false)
		close(stopped)
	}()
	eventually(t, "the implicit session to close", func() bool { return rig.run.active() == nil })
	unblock()
	<-stopped
	res := <-out
	if res.err != nil || !res.st.Running || res.st.Scenario != ScenarioWeekday || res.st.CarsInside != 0 {
		t.Fatalf("Start = %+v, %v; want a fresh running weekday session", res.st, res.err)
	}
	if st := rig.run.Status(); !st.Running || st.Scenario != ScenarioWeekday {
		t.Errorf("status = %+v, want the fresh weekday run", st)
	}
}
