package simulator

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
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
	ErrScenarioUnavailable = errors.New("simulator: scenario unavailable")
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

// qrPattern is a reservation QR token: 64 bytes of base64url without padding.
// Go's $ matches only at the end of the text, so a trailing newline fails.
var qrPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{86}$`)

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
// needs a card label (default EMP-001), reservation_qr the token in QR. A car
// that is not held stays DwellMinutes (default 30).
type InjectParams struct {
	Cred         CredentialKind
	Card         string
	QR           secret.String
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
	// Drivers returns the driver API for reservation_arrivals and no_show; nil
	// makes those scenarios unavailable.
	Drivers func(ctx context.Context) (DriverAPI, error)
	// Logger receives the reserved-car booking diagnostics; nil discards.
	Logger *slog.Logger
}

// Runner runs one scenario at a time (design §13.4) and keeps its engine
// alive while the scenario is active, so a control-API call that adds work
// after the queue emptied still runs.
type Runner struct {
	d  RunnerDeps
	wg sync.WaitGroup

	mu sync.Mutex
	s  *session // the current or the last run

	// afterDrainLeave, when set (tests only), runs after each Leave a drain
	// queues.
	afterDrainLeave func(*session)
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
	idle        atomic.Bool // the loop waits for work on an empty queue

	// Engine goroutine only.
	lastSeq int64
	denials int

	mu       sync.Mutex
	closed   bool      // no more arrivals: Stop was called
	drainReq bool      // that Stop asked for a drain
	implicit bool      // started by Inject, not by a scenario; Start takes it over
	res      *Reserver // reservation_arrivals and no_show only
	ids      []string  // every car handed to the fleet
}

// Start validates p, reads the lot's spaces and starts the scenario. ctx
// bounds the run, so pass the process context, not a request's.
//
// When Inject already started an implicit idle session, Start takes it over:
// the cars stay, the held ones stay held, and ctx is not used (the session
// keeps its own). The session also keeps the spaces read when Inject started
// it: re-reading them would reset the free list under the parked cars, so a
// takeover reads nothing. The switch to the scenario's clock runs in the
// engine goroutine, between two steps, and Start returns once it is done; if
// the session is stopped before the switch, Start begins a fresh session
// after a hard stop and answers ErrAlreadyRunning during a drain. A
// takeover is reproducible from the seed only when the cars it inherits are
// held and parked: a car still on its way in or out keeps the timing it had on
// the idle clock.
func (r *Runner) Start(ctx context.Context, p StartParams) (Status, error) {
	p, err := r.resolve(p)
	if err != nil {
		return Status{}, err
	}
	res, err := r.reserver(ctx, p)
	if err != nil {
		return Status{}, err
	}
	for {
		if r.busy() {
			return Status{}, ErrAlreadyRunning
		}
		taking := r.running() // an implicit session, since not busy
		var codes []string
		if !taking {
			if codes, err = r.lotSpaces(ctx); err != nil {
				return Status{}, err
			}
		}

		r.mu.Lock()
		if s := r.s; s != nil && !s.finished() {
			if !s.takeable() {
				r.mu.Unlock()
				return Status{}, ErrAlreadyRunning
			}
			applied := s.takeover(p, res)
			r.mu.Unlock()
			switch ok, retry := s.awaitTakeover(applied); {
			case ok:
				return s.status(), nil
			case retry:
				continue // stopped before the switch: start afresh
			default:
				return Status{}, ErrAlreadyRunning
			}
		}
		if taking {
			r.mu.Unlock()
			continue // the implicit session ended meanwhile: read the spaces
		}
		st := r.newSession(ctx, p, codes, false, res).status()
		r.mu.Unlock()
		return st, nil
	}
}

// reserver builds the Reserver of a reserved-car scenario, or nil for any other.
// It signs in the pool's first driver: a 401 means the simulated drivers are
// not seeded (design §13.1).
func (r *Runner) reserver(ctx context.Context, p StartParams) (*Reserver, error) {
	pool := PoolArrivals
	switch p.Scenario {
	case ScenarioReservationArrivals:
	case ScenarioNoShow:
		pool = PoolNoShow
	default:
		return nil, nil
	}
	if r.d.Drivers == nil {
		return nil, fmt.Errorf("%w: %s needs the simulated drivers", ErrScenarioUnavailable, p.Scenario)
	}
	api, err := r.d.Drivers(ctx)
	if err != nil {
		return nil, fmt.Errorf("simulator: driver API: %w", err)
	}
	if err := api.Login(ctx, pool[0]); err != nil {
		if apiErr, ok := errors.AsType[*DriverAPIError](err); ok && apiErr.Status == http.StatusUnauthorized {
			return nil, fmt.Errorf("%w: %s needs the simulated drivers", ErrScenarioUnavailable, p.Scenario)
		}
		return nil, fmt.Errorf("simulator: sign in a simulated driver: %w", err)
	}
	return NewReserver(api, r.d.Wall.Now, r.d.Logger), nil
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
func (r *Runner) newSession(ctx context.Context, p StartParams, codes []string, implicit bool, res *Reserver) *session {
	clk := r.newClock(p.StartAt, p.Speed)
	eng := NewEngine(clk, p.Seed)
	s := &session{
		r: r, p: p, startedAt: r.d.Wall.Now(), clock: clk, eng: eng, done: make(chan struct{}), drained: make(chan struct{}),
		fleet: NewFleetSim(eng, r.d.Client, r.d.FleetSecret, r.d.Log), implicit: implicit, res: res,
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

// takeover turns an implicit idle session into the scenario p. It claims the
// session at once, so a concurrent Start is refused, and queues the switch as
// a step at the current virtual time: the engine finishes the step in flight,
// then moves to the scenario's clock with its queued events shifted along, so
// cars in flight carry on and parked cars stay where they are. The returned
// channel receives true once the switch is done, or false when the session
// was stopped first and nothing was switched.
func (s *session) takeover(p StartParams, res *Reserver) <-chan bool {
	s.mu.Lock()
	s.implicit, s.res = false, res
	s.mu.Unlock()
	applied := make(chan bool, 1)
	s.eng.At(s.eng.Now(), func(context.Context) {
		if s.isClosed() {
			applied <- false
			return
		}
		clk := s.r.newClock(p.StartAt, p.Speed)
		s.mu.Lock()
		s.p, s.startedAt, s.clock = p, s.r.d.Wall.Now(), clk
		s.mu.Unlock()
		s.eng.Rebase(clk)
		s.schedule()
		applied <- true
	})
	return applied
}

// awaitTakeover waits for the switch queued by takeover. When the session is
// stopped first, a hard stop lets Start begin a fresh session once it is gone
// (retry true); a drain makes Start answer ErrAlreadyRunning, as it would to
// any Start during a drain.
func (s *session) awaitTakeover(applied <-chan bool) (ok, retry bool) {
	select {
	case ok := <-applied:
		if ok {
			return true, false
		}
	case <-s.done:
	}
	s.mu.Lock()
	drain := s.drainReq
	s.mu.Unlock()
	if drain {
		return false, false
	}
	<-s.done
	return false, true
}

func (r *Runner) resolve(p StartParams) (StartParams, error) {
	switch p.Scenario {
	case ScenarioIdle, ScenarioWeekday, ScenarioWeekendPeak, ScenarioFillToFull, ScenarioSensorDrift,
		ScenarioReservationArrivals, ScenarioNoShow:
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
	s, finish, err := r.beginStop(drain)
	if err != nil {
		return s, err
	}
	return finish(), nil
}

// StopAsync is Stop without the wait: it closes the run, queues the drain's
// Leaves and returns the run's status at once, while the drain (up to
// drainTimeout) and the shutdown finish in the background. Wait joins them.
// A second stop during the drain gives ErrNotRunning, except Stop(false),
// which cuts the drain.
func (r *Runner) StopAsync(drain bool) (Status, error) {
	s, finish, err := r.beginStop(drain)
	if err != nil {
		return s, err
	}
	st := r.Status()
	r.wg.Go(func() { finish() })
	return st, nil
}

// beginStop does the part of a stop that does not wait and returns the part
// that does.
func (r *Runner) beginStop(drain bool) (Status, func() Status, error) {
	s := r.current()
	if s == nil {
		return r.Status(), nil, ErrNotRunning
	}
	s.mu.Lock()
	if s.finished() {
		s.mu.Unlock()
		return s.status(), nil, ErrNotRunning
	}
	hard := func() Status {
		s.cancel()
		<-s.done
		return s.status()
	}
	if s.closed {
		escalate := !drain && s.drainReq
		s.mu.Unlock()
		if !escalate {
			return s.status(), nil, ErrNotRunning
		}
		return Status{}, hard, nil
	}
	s.closed, s.drainReq = true, drain
	ids := slices.Clone(s.ids)
	s.mu.Unlock()
	if !drain {
		return Status{}, hard, nil
	}

	for _, id := range ids {
		_ = s.fleet.Leave(id, false) // a car that is gone answers ErrCarNotFound
		if r.afterDrainLeave != nil {
			r.afterDrainLeave(s)
		}
	}
	// Only now: a queue that empties while Leaves are still being queued
	// must not end the drain.
	s.draining.Store(true)
	s.eng.Nudge() // an idle engine re-checks whether anything is left to run
	s.checkDrained()
	timedOut := make(chan struct{})
	timer := r.d.Wall.AfterFunc(drainTimeout, func() { close(timedOut) })
	return Status{}, func() Status {
		defer timer.Stop()
		select {
		case <-s.drained:
		case <-s.done:
		case <-timedOut:
		}
		return hard()
	}, nil
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
		if p.Card != "" || p.QR != "" {
			return "", fmt.Errorf("%w: a ticket car has no card or qr", ErrInvalidParams)
		}
	case CredEmployeeCard:
		car.Card = cmp.Or(p.Card, defaultCard)
		if !cardPattern.MatchString(car.Card) {
			return "", fmt.Errorf("%w: card %q, want EMP-nnn", ErrInvalidParams, car.Card)
		}
	case CredReservationQR:
		if p.Card != "" || !qrPattern.MatchString(p.QR.Reveal()) {
			return "", fmt.Errorf("%w: a reservation_qr car needs a well-formed qr and no card", ErrInvalidParams)
		}
		car.QR = p.QR
	default:
		return "", fmt.Errorf("%w: credential %q", ErrInvalidParams, p.Cred)
	}
	if p.Cred == CredEmployeeCard && p.QR != "" {
		return "", fmt.Errorf("%w: an employee car has no qr", ErrInvalidParams)
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
	ctx := r.runCtx()
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
	return r.newSession(ctx, StartParams{Scenario: ScenarioIdle, Speed: 1, StartAt: r.d.Wall.Now()}, codes, true, nil), nil
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

// runCtx is the process context runs are bound to (RunnerDeps.Ctx).
func (r *Runner) runCtx() context.Context { return cmp.Or(r.d.Ctx, context.Background()) }

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
		s.idle.Store(true)
		select {
		case <-s.eng.Wake():
		case <-ctx.Done():
			return
		}
		s.idle.Store(false)
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
		if s.isClosed() {
			return // a stopping run sends nothing more, so a drain can see an empty queue
		}
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
	if ReservedPerHour(s.p.Scenario) > 0 {
		s.eng.At(nextReserved(s.eng.Rand(), s.p.Scenario, s.eng.Now()), s.arriveReserved)
	}
}

// arriveReserved books one reserved car and queues the next. Every draw
// happens before the booking, so the server's answer never changes the random
// sequence (FR-SIM-7). A reservation_arrivals car presents its QR 1-5 virtual
// minutes later; a no_show car is never sent.
func (s *session) arriveReserved(ctx context.Context) {
	s.mu.Lock()
	res, closed := s.res, s.closed
	s.mu.Unlock()
	if closed || res == nil {
		return
	}
	r := s.eng.Rand()
	s.eng.At(nextReserved(r, s.p.Scenario, s.eng.Now()), s.arriveReserved)
	walk := time.Duration(60+r.Int64N(241)) * time.Second
	drive := s.newCar()
	car, ok := res.Book(ctx, s.p.Scenario)
	if !ok || s.p.Scenario == ScenarioNoShow {
		return
	}
	car.Dwell, car.PayLate = drive.Dwell, drive.PayLate
	s.eng.At(s.eng.Now().Add(walk), func(context.Context) { s.admit(car) })
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

// PublicLotID reads the first lot's id from the API's public endpoint, so the
// simulator never hard-codes a database id.
func PublicLotID(ctx context.Context, hc *http.Client, baseURL string) (uuid.UUID, error) {
	id, err := firstLotID(ctx, hc, baseURL)
	if err != nil {
		return uuid.Nil, err
	}
	lot, err := uuid.Parse(id)
	if err != nil {
		return uuid.Nil, errors.New("simulator: the API lists a lot with a malformed id")
	}
	return lot, nil
}

func firstLotID(ctx context.Context, hc *http.Client, baseURL string) (string, error) {
	var lots []struct {
		ID string `json:"id"`
	}
	if err := getData(ctx, hc, baseURL+"/v1/lots", &lots); err != nil {
		return "", err
	}
	if len(lots) == 0 {
		return "", errors.New("simulator: the API lists no lot")
	}
	return lots[0].ID, nil
}

// PublicSpaces returns a RunnerDeps.Spaces that reads the first lot's space
// codes from the API's public endpoints (design §11.1).
func PublicSpaces(hc *http.Client, baseURL string) func(context.Context) ([]string, error) {
	return func(ctx context.Context) ([]string, error) {
		lot, err := firstLotID(ctx, hc, baseURL)
		if err != nil {
			return nil, err
		}
		var spaces struct {
			Spaces []struct {
				Code string `json:"code"`
			} `json:"spaces"`
		}
		if err := getData(ctx, hc, baseURL+"/v1/lots/"+url.PathEscape(lot)+"/spaces", &spaces); err != nil {
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
