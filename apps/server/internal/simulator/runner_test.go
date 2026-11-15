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
	api *fakeAPI
	clk *haltClock
	log *EventLog
	run *Runner
}

// newRunnerRig builds a runner over the fake API. The clock halts horizon
// after the scenario's start.
func newRunnerRig(t *testing.T, horizon time.Duration, spaces func(context.Context) ([]string, error)) *runnerRig {
	t.Helper()
	rig := &runnerRig{log: NewEventLog(5000)}
	rig.api = &fakeAPI{script: nil, used: map[string]int{}, creds: map[string]int{}}
	client := NewDeviceClient(ClientConfig{
		BaseURL: "http://api.test", Transport: HandlerTransport(rig.api),
		SignClock: fakeclock.New(t0), FleetSecret: fleetSecret,
	})
	rig.run = NewRunner(RunnerDeps{
		Client: client, FleetSecret: fleetSecret, Log: rig.log, Wall: fakeclock.New(t0), Spaces: spaces,
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
	for _, p := range []StartParams{{Scenario: ScenarioWeekday}, {Scenario: ScenarioWeekday, Speed: 121}, {Scenario: ScenarioWeekday, Speed: -1}, {Speed: 30, Scenario: "nope"}, {Speed: 30}} {
		if _, err := rig.run.Start(ctx, p); !errors.Is(err, ErrInvalidParams) {
			t.Errorf("Start(%+v) = %v, want ErrInvalidParams", p, err)
		}
	}
	if st := rig.run.Status(); st.Running {
		t.Fatalf("rejected starts left a run: %+v", st)
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
	if _, err := rig.run.Inject(InjectParams{Cred: CredTicket}); !errors.Is(err, ErrNotRunning) {
		t.Errorf("Inject idle = %v, want ErrNotRunning", err)
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
