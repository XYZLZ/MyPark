package simulator

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"github.com/XYZLZ/MyPark/apps/server/internal/devicefleet"
	"github.com/XYZLZ/MyPark/apps/server/internal/domain/secret"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/clock"
)

var (
	ErrAlreadyRunning      = errors.New("simulator: a scenario is already running")
	ErrNotRunning          = errors.New("simulator: no scenario is running")
	ErrScenarioUnavailable = errors.New("simulator: scenario unavailable in M2")
	// ErrInvalidParams wraps a request the runner refuses: bad scenario, speed
	// or injected-car fields.
	ErrInvalidParams = errors.New("simulator: invalid parameters")
)

const (
	// DefaultSpeed is the speed factor of a start request that names none.
	DefaultSpeed = 30
	MaxSpeed     = 120
	// defaultInjectDwell is how long an injected car that is not held stays.
	defaultInjectDwell = 30 * time.Minute
	maxInjectDwell     = 600 * time.Minute
	// drainTimeout bounds Stop(true) in wall time: a car the server keeps
	// refusing must not hold the stop forever.
	drainTimeout   = 10 * time.Minute
	fullDenials    = 3
	heartbeatEvery = time.Minute
	defaultCard    = "EMP-001"
)

var cardPattern = regexp.MustCompile(`^EMP-[0-9]{3}$`)

// StartParams starts a scenario. Speed is 1-120 and has no default here: the
// caller applies DefaultSpeed when the request names none. A zero StartAt
// means today 07:00 in the lot's zone (weekday, weekend_peak) or now.
type StartParams struct {
	Scenario Scenario
	Seed     uint64
	Speed    int
	StartAt  time.Time
}

// Status is the control API's status document (design §13.3).
type Status struct {
	Scenario    Scenario  `json:"scenario"`
	Running     bool      `json:"running"`
	Seed        uint64    `json:"seed"`
	Speed       int       `json:"speed"`
	VirtualNow  time.Time `json:"virtual_now"`
	CarsInside  int       `json:"cars_inside"`
	StartedAt   time.Time `json:"started_at,omitzero"`
	ReachedFull bool      `json:"reached_full"`
}

// InjectParams describes a car the control API adds. Cred employee_card
// needs a card label (default EMP-001). A car that is not held stays
// DwellMinutes (default 30).
type InjectParams struct {
	Cred         CredentialKind
	Card         string
	Hold         bool
	DwellMinutes int
}

// RunnerDeps are the runner's collaborators.
type RunnerDeps struct {
	Client      *DeviceClient
	FleetSecret secret.Bytes
	// NewClock builds the virtual clock of a run; nil gives a RealClock on Wall.
	NewClock func(start time.Time, speed int) Clock
	// Wall times StartedAt, the default start and the drain cap. A real
	// deployment passes clock.System{}.
	Wall clock.Timers
	Log  *EventLog
	// Ctx bounds a session that Inject starts by itself (the process
	// context); nil means context.Background().
	Ctx context.Context
	// Spaces reads the lot's space codes at each start (see PublicSpaces); nil
	// keeps the pilot lot's E001..E280.
	Spaces func(ctx context.Context) ([]string, error)
}

// Runner runs one scenario at a time (design §13.4) and keeps its engine
// alive while the scenario is active, so a control-API call that adds work
// after the queue emptied still runs.
type Runner struct {
	d  RunnerDeps
	wg sync.WaitGroup

	mu sync.Mutex
	s  *session // the current or the last run
}

// NewRunner returns an idle runner.
func NewRunner(d RunnerDeps) *Runner { return &Runner{d: d} }

// session is one run: its engine, fleet and goroutine.
type session struct {
	r         *Runner
	p         StartParams // p, startedAt, clock and implicit change at a takeover: use mu
	startedAt time.Time
	clock     Clock
	eng       *Engine
	fleet     *FleetSim
	cancel    context.CancelFunc
	done      chan struct{}
	endNow    time.Time // set before done closes

	reachedFull atomic.Bool
	draining    atomic.Bool
	drained     chan struct{}
	drainOnce   sync.Once

	// Engine goroutine only.
	lastSeq int64
	denials int

	mu       sync.Mutex
	closed   bool     // no more arrivals: Stop was called
	drainReq bool     // that Stop asked for a drain
	implicit bool     // started by Inject, not by a scenario; Start takes it over
	ids      []string // every car handed to the fleet
}

// Start validates p, reads the lot's spaces and starts the scenario. ctx
// bounds the run, so pass the process context, not a request's. When Inject
// already started an implicit idle session, Start takes it over: the cars stay,
// the held ones stay held, and ctx is not used (the session keeps its own).
func (r *Runner) Start(ctx context.Context, p StartParams) (Status, error) {
	p, err := r.resolve(p)
	if err != nil {
		return Status{}, err
	}
	if r.busy() {
		return Status{}, ErrAlreadyRunning
	}
	codes, err := r.lotSpaces(ctx)
	if err != nil {
		return Status{}, err
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if s := r.s; s != nil && !s.finished() {
		if !s.takeable() {
			return Status{}, ErrAlreadyRunning
		}
		s.takeover(p)
		return s.status(), nil
	}
	return r.newSession(ctx, p, codes, false).status(), nil
}

// lotSpaces reads the lot's space codes, or none when no reader is set.
func (r *Runner) lotSpaces(ctx context.Context) ([]string, error) {
	if r.d.Spaces == nil {
		return nil, nil
	}
	codes, err := r.d.Spaces(ctx)
	if err != nil {
		return nil, fmt.Errorf("simulator: read the lot's spaces: %w", err)
	}
	return codes, nil
}

func (r *Runner) newClock(start time.Time, speed int) Clock {
	if r.d.NewClock != nil {
		return r.d.NewClock(start, speed)
	}
	return NewRealClock(start, speed, r.d.Wall)
}

// newSession builds and starts a run. The caller holds r.mu.
func (r *Runner) newSession(ctx context.Context, p StartParams, codes []string, implicit bool) *session {
	clk := r.newClock(p.StartAt, p.Speed)
	eng := NewEngine(clk, p.Seed)
	s := &session{
		r: r, p: p, startedAt: r.d.Wall.Now(), clock: clk, eng: eng, done: make(chan struct{}), drained: make(chan struct{}),
		fleet: NewFleetSim(eng, r.d.Client, r.d.FleetSecret, r.d.Log), implicit: implicit,
	}
	if len(codes) > 0 {
		s.fleet.SetSpaces(codes)
	}
	eng.SetAfterStep(s.afterStep)
	if !implicit {
		s.schedule()
	}
	runCtx, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	r.s = s
	r.wg.Go(func() { s.loop(runCtx) })
	return s
}

// takeable reports whether Start may take the session over.
func (s *session) takeable() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.implicit && !s.closed
}

// takeover turns an implicit idle session into the scenario p. The engine moves
// to the scenario's clock with its queued events shifted along, so cars in
// flight carry on; parked cars stay where they are.
func (s *session) takeover(p StartParams) {
	clk := s.r.newClock(p.StartAt, p.Speed)
	s.mu.Lock()
	s.p, s.startedAt, s.clock, s.implicit = p, s.r.d.Wall.Now(), clk, false
	s.mu.Unlock()
	s.eng.Rebase(clk)
	s.schedule()
}

func (r *Runner) resolve(p StartParams) (StartParams, error) {
	switch p.Scenario {
	case ScenarioIdle, ScenarioWeekday, ScenarioWeekendPeak, ScenarioFillToFull, ScenarioSensorDrift:
	case ScenarioReservationArrivals, ScenarioNoShow:
		return p, fmt.Errorf("%w: %s", ErrScenarioUnavailable, p.Scenario)
	default:
		return p, fmt.Errorf("%w: unknown scenario %q", ErrInvalidParams, p.Scenario)
	}
	if p.Seed > math.MaxInt64 {
		return p, fmt.Errorf("%w: seed %d exceeds the int64 range", ErrInvalidParams, p.Seed)
	}
	if p.Speed < 1 || p.Speed > MaxSpeed {
		return p, fmt.Errorf("%w: speed %d, want 1-%d", ErrInvalidParams, p.Speed, MaxSpeed)
	}
	if p.StartAt.IsZero() {
		p.StartAt = r.d.Wall.Now()
		if p.Scenario == ScenarioWeekday || p.Scenario == ScenarioWeekendPeak {
			d := p.StartAt.In(lotZone)
			p.StartAt = time.Date(d.Year(), d.Month(), d.Day(), 7, 0, 0, 0, lotZone)
		}
	}
	return p, nil
}

// Stop ends the run. With drain, every car inside is sent out first and Stop
// returns when they are gone, when nothing can move any more, or after
// drainTimeout on the wall clock. Without it the run is cut and the cars are
// forgotten, their stays left active (design §13.4). Stop(false) during a
// drain turns it into a hard stop.
func (r *Runner) Stop(drain bool) (Status, error) {
	s := r.current()
	if s == nil {
		return r.Status(), ErrNotRunning
	}
	s.mu.Lock()
	if s.finished() {
		s.mu.Unlock()
		return s.status(), ErrNotRunning
	}
	if s.closed {
		escalate := !drain && s.drainReq
		s.mu.Unlock()
		if !escalate {
			return s.status(), ErrNotRunning
		}
		s.cancel()
		<-s.done
		return s.status(), nil
	}
	s.closed, s.drainReq = true, drain
	ids := slices.Clone(s.ids)
	s.mu.Unlock()

	if drain {
		for _, id := range ids {
			_ = s.fleet.Leave(id, false) // a car that is gone answers ErrCarNotFound
		}
		// Only now: a queue that empties while Leaves are still being queued
		// must not end the drain.
		s.draining.Store(true)
		s.eng.Nudge() // an idle engine re-checks whether anything is left to run
		s.checkDrained()
		timedOut := make(chan struct{})
		defer r.d.Wall.AfterFunc(drainTimeout, func() { close(timedOut) }).Stop()
		select {
		case <-s.drained:
		case <-s.done:
		case <-timedOut:
		}
	}
	s.cancel()
	<-s.done
	return s.status(), nil
}

// Status reports the current run, or the last one after it ended.
func (r *Runner) Status() Status {
	if s := r.current(); s != nil {
		return s.status()
	}
	return Status{Speed: DefaultSpeed, VirtualNow: r.d.Wall.Now()}
}

// Inject adds a car to the running scenario (design §13.3) and returns its id.
func (r *Runner) Inject(p InjectParams) (string, error) {
	car := &Car{Cred: p.Cred, Hold: p.Hold, Dwell: defaultInjectDwell}
	switch p.Cred {
	case CredTicket:
		if p.Card != "" {
			return "", fmt.Errorf("%w: a ticket car has no card", ErrInvalidParams)
		}
	case CredEmployeeCard:
		car.Card = cmp.Or(p.Card, defaultCard)
		if !cardPattern.MatchString(car.Card) {
			return "", fmt.Errorf("%w: card %q, want EMP-nnn", ErrInvalidParams, car.Card)
		}
	default:
		return "", fmt.Errorf("%w: credential %q", ErrInvalidParams, p.Cred)
	}
	if p.DwellMinutes < 0 || p.DwellMinutes > int(maxInjectDwell/time.Minute) {
		return "", fmt.Errorf("%w: dwell_minutes %d, want 1-600", ErrInvalidParams, p.DwellMinutes)
	}
	if p.DwellMinutes > 0 {
		car.Dwell = time.Duration(p.DwellMinutes) * time.Minute
	}
	s, err := r.activeOrImplicit()
	if err != nil {
		return "", err
	}
	if !s.admit(car) {
		return "", ErrNotRunning
	}
	return car.ID, nil
}

// activeOrImplicit is the active run. With none, it starts an implicit idle
// session at speed 1 on the real clock, as design §13.3 has injected cars run
// when idle; a later Start takes it over. A run that is stopping takes no cars.
func (r *Runner) activeOrImplicit() (*session, error) {
	if s := r.active(); s != nil {
		return s, nil
	}
	if r.running() {
		return nil, ErrNotRunning
	}
	ctx := cmp.Or(r.d.Ctx, context.Background())
	codes, err := r.lotSpaces(ctx)
	if err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if s := r.s; s != nil && !s.finished() { // another caller got there first
		if s.isClosed() {
			return nil, ErrNotRunning
		}
		return s, nil
	}
	return r.newSession(ctx, StartParams{Scenario: ScenarioIdle, Speed: 1, StartAt: r.d.Wall.Now()}, codes, true), nil
}

// Leave sends a car out: ErrNotRunning without a run, ErrCarNotFound for an
// unknown or departed car.
func (r *Runner) Leave(carID string, loseTicket bool) error {
	s := r.active()
	if s == nil {
		return ErrNotRunning
	}
	return s.fleet.Leave(carID, loseTicket)
}

// SetFault sets a sensor's failure mode: ErrNotRunning without a run,
// ErrUnknownSpace for a code the lot lacks.
func (r *Runner) SetFault(code string, f Fault) error {
	s := r.active()
	if s == nil {
		return ErrNotRunning
	}
	return s.fleet.SetFault(code, f)
}

// Wait returns once every run goroutine has ended: after Stop, or after the
// start context was cancelled.
func (r *Runner) Wait() { r.wg.Wait() }

func (r *Runner) current() *session {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.s
}

func (r *Runner) running() bool {
	s := r.current()
	return s != nil && !s.finished()
}

// busy is a run that Start may not replace.
func (r *Runner) busy() bool {
	s := r.current()
	return s != nil && !s.finished() && !s.takeable()
}

// active is the current run when it still takes work: started, not stopping.
func (r *Runner) active() *session {
	s := r.current()
	if s == nil || s.finished() {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	return s
}

func (s *session) isClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

func (s *session) finished() bool {
	select {
	case <-s.done:
		return true
	default:
		return false
	}
}

func (s *session) status() Status {
	s.mu.Lock()
	p, started, clk := s.p, s.startedAt, s.clock
	s.mu.Unlock()
	st := Status{
		Scenario: p.Scenario, Seed: p.Seed, Speed: p.Speed, StartedAt: started,
		ReachedFull: s.reachedFull.Load(), CarsInside: s.fleet.Inside(),
	}
	if s.finished() {
		st.VirtualNow = s.endNow
	} else {
		st.Running, st.VirtualNow = true, clk.Now()
	}
	return st
}

// loop runs the engine and, whenever its queue empties, waits for more work
// (a Leave, an Inject) instead of letting the run die.
func (s *session) loop(ctx context.Context) {
	defer func() {
		s.mu.Lock()
		s.endNow = s.clock.Now()
		s.mu.Unlock()
		close(s.done)
	}()
	for {
		if err := s.eng.Run(ctx); err != nil {
			return
		}
		if s.draining.Load() {
			s.drainOnce.Do(func() { close(s.drained) }) // nothing more can move
		}
		select {
		case <-s.eng.Wake():
		case <-ctx.Done():
			return
		}
	}
}

func (s *session) afterStep() {
	if s.draining.Load() {
		s.checkDrained()
	}
}

// checkDrained ends the drain once no car is in play.
func (s *session) checkDrained() {
	if s.fleet.Active() == 0 {
		s.drainOnce.Do(func() { close(s.drained) })
	}
}

// admit hands car to the fleet unless the run is stopping.
func (s *session) admit(car *Car) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return false
	}
	s.fleet.Arrive(car)
	s.ids = append(s.ids, car.ID)
	return true
}

// schedule queues the scenario's first events at the start time.
func (s *session) schedule() {
	start := s.p.StartAt
	if s.p.Scenario == ScenarioIdle {
		for _, d := range devicefleet.Devices() {
			s.eng.At(start, s.heartbeat(d))
		}
		return
	}
	s.eng.At(start, s.begin)
}

func (s *session) heartbeat(dev devicefleet.Device) func(context.Context) {
	return func(ctx context.Context) {
		rep, err := s.r.d.Client.Send(ctx, dev, Outgoing{EventID: uuid.New(), Type: "heartbeat", Payload: struct{}{}})
		s.r.d.Log.Append(outcome(LogEntry{At: s.eng.Now(), Device: dev.Name, Type: "heartbeat"}, rep, err))
		s.eng.At(s.eng.Now().Add(heartbeatEvery), s.heartbeat(dev))
	}
}

// begin runs once at the start time, in the engine goroutine: it sets up the
// scenario's extras and queues the first arrival.
func (s *session) begin(context.Context) {
	if s.isClosed() {
		return
	}
	s.eng.reseed(s.p.Seed) // a taken-over session drew while implicit
	s.lastSeq = s.r.d.Log.LastSeq()
	if s.p.Scenario == ScenarioSensorDrift {
		s.fleet.DriftSensors(3, 0.1)
	}
	if s.p.Scenario.followsWeekday() {
		s.employees()
	}
	s.eng.At(nextArrival(s.eng.Rand(), s.p.Scenario, s.eng.Now()), s.arrive)
}

// employees schedules today's 30 card holders (07:30-08:30, 8-9 h stays) and
// itself again at midnight. Draws are made for every card, so the sequence
// does not depend on the start time.
func (s *session) employees() {
	if s.isClosed() {
		return // a stopping run schedules nothing more
	}
	now := s.eng.Now().In(lotZone)
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, lotZone)
	r := s.eng.Rand()
	for i := 1; i <= employeeCount; i++ {
		at := day.Add(7*time.Hour + 30*time.Minute + time.Duration(r.Int64N(3600))*time.Second)
		stay := 8*time.Hour + time.Duration(r.Int64N(3600))*time.Second
		if at.Before(now) {
			continue
		}
		car := &Car{Cred: CredEmployeeCard, Card: fmt.Sprintf("EMP-%03d", i), Dwell: stay}
		s.eng.At(at, func(context.Context) { s.admit(car) })
	}
	s.eng.At(day.AddDate(0, 0, 1), func(context.Context) { s.employees() })
}

// arrive sends the next car and queues the one after it. fill_to_full stops
// after three consecutive gate.lot_full denials at the dispenser.
func (s *session) arrive(context.Context) {
	if s.sawFull() {
		return
	}
	if !s.admit(s.newCar()) {
		return
	}
	s.eng.At(nextArrival(s.eng.Rand(), s.p.Scenario, s.eng.Now()), s.arrive)
}

func (s *session) newCar() *Car {
	if s.p.Scenario == ScenarioFillToFull {
		return &Car{Cred: CredTicket, Hold: true} // no exits
	}
	r := s.eng.Rand()
	car := &Car{Cred: CredTicket, Dwell: Dwell(r, s.p.Scenario)}
	if s.p.Scenario.followsWeekday() {
		car.LoseTicket, car.PayLate = r.Float64() < loseTicketP, r.Float64() < payLateP
	}
	return car
}

// sawFull reads the entry outcomes logged since the last look and reports
// whether fill_to_full is done. Reading the log keeps the fleet unaware of
// scenarios; the order of entries is the engine's order, so this is
// deterministic.
func (s *session) sawFull() bool {
	if s.p.Scenario != ScenarioFillToFull {
		return false
	}
	for _, e := range s.r.d.Log.After(s.lastSeq) {
		s.lastSeq = e.Seq
		if e.Type != "entry.credential_presented" {
			continue
		}
		if e.Result == "deny" && e.Reason == "gate.lot_full" {
			s.denials++
		} else {
			s.denials = 0
		}
	}
	if s.denials >= fullDenials {
		s.reachedFull.Store(true)
	}
	return s.reachedFull.Load()
}

// PublicSpaces returns a RunnerDeps.Spaces that reads the first lot's space
// codes from the API's public endpoints (design §11.1).
func PublicSpaces(hc *http.Client, baseURL string) func(context.Context) ([]string, error) {
	return func(ctx context.Context) ([]string, error) {
		var lots []struct {
			ID string `json:"id"`
		}
		if err := getData(ctx, hc, baseURL+"/v1/lots", &lots); err != nil {
			return nil, err
		}
		if len(lots) == 0 {
			return nil, errors.New("simulator: the API lists no lot")
		}
		var spaces struct {
			Spaces []struct {
				Code string `json:"code"`
			} `json:"spaces"`
		}
		if err := getData(ctx, hc, baseURL+"/v1/lots/"+url.PathEscape(lots[0].ID)+"/spaces", &spaces); err != nil {
			return nil, err
		}
		codes := make([]string, len(spaces.Spaces))
		for i, sp := range spaces.Spaces {
			codes[i] = sp.Code
		}
		return codes, nil
	}
}

func getData(ctx context.Context, hc *http.Client, u string, into any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	res, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("simulator: GET %s answered %d", u, res.StatusCode)
	}
	var env struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, maxReplyBytes)).Decode(&env); err != nil {
		return fmt.Errorf("simulator: GET %s: %w", u, err)
	}
	return json.Unmarshal(env.Data, into)
}
