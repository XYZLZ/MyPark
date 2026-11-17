package simulator

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/XYZLZ/MyPark/apps/server/internal/devicefleet"
	"github.com/XYZLZ/MyPark/apps/server/internal/domain/authz"
	"github.com/XYZLZ/MyPark/apps/server/internal/domain/secret"
)

// CredentialKind is how a car identifies itself at the gates.
type CredentialKind string

const (
	CredTicket       CredentialKind = "ticket"
	CredEmployeeCard CredentialKind = "employee_card"
	// CredReservationQR is a reservation's QR token (M3B-46).
	CredReservationQR CredentialKind = "reservation_qr"
)

// Fault is a sensor failure mode (design §13.3).
type Fault string

const (
	FaultNone          Fault = "none"
	FaultStuckOccupied Fault = "stuck_occupied" // never reports free
	FaultStuckFree     Fault = "stuck_free"     // never reports occupied
	FaultSilent        Fault = "silent"         // reports nothing
)

// ErrCarNotFound is returned for a car id that is unknown or already gone.
var ErrCarNotFound = errors.New("simulator: car not found")

// ErrUnknownSpace is returned by SetFault for a space code the lot lacks.
var ErrUnknownSpace = errors.New("simulator: unknown space")

const (
	// defaultSpaces matches the pilot lot's spaces E001..E280.
	defaultSpaces = 280
	// maxRepays bounds the pay-and-retry loop at the exit barrier.
	maxRepays       = 3
	credentialDelay = 2 * time.Second
	lateMargin      = time.Minute
)

// Car is one simulated vehicle. Hand it to Arrive and do not touch it after:
// the simulator owns its fields. ID, StayID, TicketCode, Space and Inside are
// filled in as the car moves; Hold parks the car until Leave.
type Car struct {
	ID         string
	Cred       CredentialKind
	Card       string
	QR         secret.String // CredReservationQR only; revealed only into the device payload
	Hold       bool
	Dwell      time.Duration
	LoseTicket bool
	PayLate    bool
	StayID     string
	TicketCode string
	Space      string
	Inside     bool
}

// carRun is a car's progress. The state machine mirrors the server's answers:
// a step runs only after the previous one was accepted.
type carRun struct {
	car      *Car
	inPlace  bool // parked in its space, waiting out the dwell or for Leave
	leaveReq bool // Leave came before the car parked
	leaving  bool // the pay-station sequence has started
	lateDone bool
	window   time.Duration // exit window of the latest payment (deadline - paid_at); 0 when unknown
	stuck    bool          // halted inside the lot; Leave starts it again
	repays   int
}

// FleetSim drives cars through the device API (design §13.2). All random
// draws happen in the engine goroutine; Arrive, Leave, Inside and SetFault may
// be called from others.
type FleetSim struct {
	e     *Engine
	c     *DeviceClient
	fleet secret.Bytes
	log   *EventLog

	podium, dispenser, entry, exit, pay, sensors devicefleet.Device

	mu     sync.Mutex
	seq    int
	cars   map[string]*carRun
	known  map[string]bool
	free   []string
	faults map[string]Fault
	inside int
	drop   float64 // chance that a sensor event is lost
}

// NewFleetSim returns a simulator over the fleet manifest, with the pilot
// lot's spaces E001..E280 free.
func NewFleetSim(e *Engine, c *DeviceClient, fleetSecret secret.Bytes, log *EventLog) *FleetSim {
	f := &FleetSim{
		e: e, c: c, fleet: fleetSecret, log: log,
		podium: devicefleet.ByKind(authz.DevicePodium), dispenser: devicefleet.ByKind(authz.DeviceDispenser),
		entry: devicefleet.ByKind(authz.DeviceEntryBarrier), exit: devicefleet.ByKind(authz.DeviceExitBarrier),
		pay: devicefleet.ByKind(authz.DevicePayStation), sensors: devicefleet.ByKind(authz.DeviceSensorGateway),
		cars: map[string]*carRun{},
	}
	codes := make([]string, defaultSpaces)
	for i := range codes {
		codes[i] = fmt.Sprintf("E%03d", i+1)
	}
	f.SetSpaces(codes)
	return f
}

// SetSpaces replaces the lot's space codes, all free, and clears every fault.
// Call it before cars arrive.
func (f *FleetSim) SetSpaces(codes []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.free = append([]string(nil), codes...)
	f.known = make(map[string]bool, len(codes))
	for _, c := range codes {
		f.known[c] = true
	}
	f.faults = map[string]Fault{}
}

// Active returns how many cars are in play: on their way in, inside, or halted
// inside the lot. A car that left, or never got in, is not counted.
func (f *FleetSim) Active() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.cars)
}

// DriftSensors makes n random spaces' sensors stuck (each occupied or free)
// and loses each sensor event with probability drop (0..1), on top of the
// per-space faults (design §13.2 sensor_drift). Call it from the engine
// goroutine, before cars arrive: it draws from the engine's generator.
func (f *FleetSim) DriftSensors(n int, drop float64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.drop = drop
	pool := append([]string(nil), f.free...)
	for range min(n, len(pool)) {
		i := f.e.Rand().IntN(len(pool))
		f.faults[pool[i]] = []Fault{FaultStuckOccupied, FaultStuckFree}[f.e.Rand().IntN(2)]
		pool[i] = pool[len(pool)-1]
		pool = pool[:len(pool)-1]
	}
}

// dropped draws whether a sensor event is lost. It draws only when drops are
// on, so other scenarios keep their random sequence. Call with f.mu held, from
// the engine goroutine.
func (f *FleetSim) dropped() bool { return f.drop > 0 && f.e.Rand().Float64() < f.drop }

// Inside returns how many cars the server let in and has not let out.
func (f *FleetSim) Inside() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.inside
}

// SetFault sets the failure mode of one space's sensor.
func (f *FleetSim) SetFault(code string, fault Fault) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.known[code] {
		return fmt.Errorf("%w: %q", ErrUnknownSpace, code)
	}
	if fault == FaultNone {
		delete(f.faults, code)
	} else {
		f.faults[code] = fault
	}
	return nil
}

// Arrive gives car an id (car-000001, ...) and schedules its trip from the
// current virtual time.
func (f *FleetSim) Arrive(car *Car) {
	f.mu.Lock()
	f.seq++
	car.ID = fmt.Sprintf("car-%06d", f.seq)
	r := &carRun{car: car}
	f.cars[car.ID] = r
	f.mu.Unlock()
	f.after(0, func(ctx context.Context) { f.detect(ctx, r) })
}

// Leave sends a parked car to the pay station (or tells a car still on its way
// to go as soon as it parks). loseTicket makes it report the ticket lost. A
// car the server halted inside the lot starts its departure again.
//
// A car that is already leaving ignores the call, loseTicket included: the
// pay-station sequence is under way and cannot be redone mid-flight.
func (f *FleetSim) Leave(carID string, loseTicket bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.cars[carID]
	if !ok {
		return fmt.Errorf("%w: %s", ErrCarNotFound, carID)
	}
	if r.leaving && !r.stuck {
		return nil
	}
	if loseTicket && r.car.Cred == CredTicket {
		r.car.LoseTicket = true
	}
	if r.stuck {
		r.stuck, r.leaving, r.repays = false, false, 0
		f.after(0, func(ctx context.Context) { f.depart(ctx, r) })
		return nil
	}
	if r.inPlace {
		f.after(0, func(ctx context.Context) { f.depart(ctx, r) })
	} else {
		r.leaveReq = true
	}
	return nil
}

func (f *FleetSim) after(d time.Duration, step func(context.Context)) {
	f.e.At(f.e.Now().Add(d), step)
}

// between draws a whole-second duration in [lo, hi].
func (f *FleetSim) between(lo, hi time.Duration) time.Duration {
	return lo + time.Duration(f.e.Rand().Int64N(int64((hi-lo)/time.Second)+1))*time.Second
}

// credential is the {kind, value} a car presents. It is the only place that
// reveals a QR token.
func (f *FleetSim) credential(car *Car) map[string]any {
	value := car.TicketCode
	switch car.Cred {
	case CredEmployeeCard:
		value = devicefleet.CardUID(f.fleet, car.Card)
	case CredReservationQR:
		value = car.QR.Reveal()
	}
	return map[string]any{"kind": string(car.Cred), "value": value}
}

// tokenRef is the log's stand-in for a token: sha256:<12 hex>.
func tokenRef(token secret.String) string {
	sum := sha256.Sum256([]byte(token.Reveal()))
	return "sha256:" + hex.EncodeToString(sum[:])[:12]
}

// send posts one event, logs it and reports whether the server accepted it.
// A transport failure or a non-2xx answer is logged as result "error".
func (f *FleetSim) send(ctx context.Context, r *carRun, dev devicefleet.Device, typ string, payload any) (Reply, bool) {
	rep, err := f.c.Send(ctx, dev, Outgoing{
		EventID: uuid.New(), Type: typ, Payload: payload,
	})
	entry := LogEntry{At: f.e.Now(), Device: dev.Name, Type: typ, CarID: r.car.ID}
	if r.car.Cred == CredReservationQR {
		entry.Credential, entry.CredentialRef = string(CredReservationQR), tokenRef(r.car.QR)
	}
	f.log.Append(outcome(entry, rep, err))
	return rep, err == nil
}

// outcome fills entry's result and reason from a device answer. A transport
// failure or a non-2xx answer is result "error".
func outcome(entry LogEntry, rep Reply, err error) LogEntry {
	if err != nil {
		entry.Result, entry.Reason = "error", "transport_error"
		if apiErr, ok := errors.AsType[*APIError](err); ok && apiErr.Code != "" {
			entry.Reason = apiErr.Code
		}
		return entry
	}
	entry.Result, entry.Reason = rep.Result, rep.Reason
	return entry
}

// halt ends the car's trip where it stands. A car that never entered is gone.
// A car that entered keeps its space, stays counted inside (as the server
// counts it) and stays reachable: Leave starts it again.
func (f *FleetSim) halt(r *carRun) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.car.Inside {
		r.stuck = true
		return
	}
	delete(f.cars, r.car.ID)
}

// finish removes a car that left the lot.
func (f *FleetSim) finish(r *carRun) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.cars, r.car.ID)
}

func (f *FleetSim) detect(ctx context.Context, r *carRun) {
	if _, ok := f.send(ctx, r, f.podium, "podium.vehicle_detected", struct{}{}); !ok {
		f.halt(r)
		return
	}
	f.after(credentialDelay, func(ctx context.Context) { f.present(ctx, r) })
}

func (f *FleetSim) present(ctx context.Context, r *carRun) {
	dev, payload := f.dispenser, map[string]any{"kind": "ticket_request"}
	if r.car.Cred != CredTicket {
		dev, payload = f.entry, f.credential(r.car)
	}
	rep, ok := f.send(ctx, r, dev, "entry.credential_presented", payload)
	if !ok || rep.Result != "open" || rep.StayID == "" {
		f.halt(r) // a deny at the entry sends the car away
		return
	}
	f.mu.Lock()
	r.car.StayID, r.car.TicketCode = rep.StayID, rep.TicketCode
	f.mu.Unlock()
	f.after(f.between(3*time.Second, 8*time.Second), func(ctx context.Context) { f.enter(ctx, r) })
}

func (f *FleetSim) enter(ctx context.Context, r *carRun) {
	rep, ok := f.send(ctx, r, f.entry, "barrier.passed", map[string]any{"direction": "entry", "stay_id": r.car.StayID})
	if !ok || rep.Result != "ack" {
		f.halt(r)
		return
	}
	f.mu.Lock()
	r.car.Inside = true
	f.inside++
	f.mu.Unlock()
	f.after(f.between(20*time.Second, 60*time.Second), func(ctx context.Context) { f.park(ctx, r) })
}

func (f *FleetSim) park(ctx context.Context, r *carRun) {
	f.mu.Lock()
	var report bool
	if n := len(f.free); n > 0 {
		i := f.e.Rand().IntN(n)
		r.car.Space = f.free[i]
		f.free[i] = f.free[n-1]
		f.free = f.free[:n-1]
		report = f.faults[r.car.Space] != FaultSilent && f.faults[r.car.Space] != FaultStuckFree && !f.dropped()
	}
	space := r.car.Space
	f.mu.Unlock()
	if report {
		f.send(ctx, r, f.sensors, "sensor.space_changed", map[string]any{"space_code": space, "occupied": true})
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	switch {
	case r.leaveReq:
		f.after(0, func(ctx context.Context) { f.depart(ctx, r) })
	case r.car.Hold:
		r.inPlace = true // Leave schedules the departure
	default:
		r.inPlace = true
		f.after(r.car.Dwell, func(ctx context.Context) { f.depart(ctx, r) })
	}
}

// depart runs the pay-station sequence once, whoever asks first (the dwell
// timer or Leave).
func (f *FleetSim) depart(ctx context.Context, r *carRun) {
	f.mu.Lock()
	if r.leaving {
		f.mu.Unlock()
		return
	}
	r.leaving = true
	lost := r.car.LoseTicket
	f.mu.Unlock()

	if r.car.Cred != CredEmployeeCard && !f.settle(ctx, r, lost) {
		f.halt(r)
		return
	}
	f.scheduleExit(r)
}

// settle lists a lost ticket, scans, and pays what the scan asks. It reports
// false when the server refused a step.
func (f *FleetSim) settle(ctx context.Context, r *carRun, lost bool) bool {
	if lost {
		rep, ok := f.send(ctx, r, f.pay, "paystation.lost_ticket", map[string]any{"stay_id": r.car.StayID})
		if !ok || rep.Result != "ack" || rep.TicketCode == "" {
			return false
		}
		f.mu.Lock()
		r.car.TicketCode = rep.TicketCode // the replacement replaces the lost code
		f.mu.Unlock()
	}
	rep, ok := f.send(ctx, r, f.pay, "paystation.scan", map[string]any{"credential": f.credential(r.car)})
	if !ok || rep.Result != "ack" {
		return false
	}
	if rep.AmountCents == 0 {
		return true // the server rejects a RD$0 cash payment
	}
	return f.payCash(ctx, r, rep.AmountCents)
}

// payCash pays amount in cash and remembers the exit window. It keeps the
// window, not the deadline: the server's timestamps are wall time and the
// engine's are virtual.
func (f *FleetSim) payCash(ctx context.Context, r *carRun, amount int64) bool {
	rep, ok := f.send(ctx, r, f.pay, "paystation.cash_paid",
		map[string]any{"stay_id": r.car.StayID, "amount_cents": amount})
	if !ok || rep.Result != "ack" {
		return false
	}
	f.mu.Lock()
	r.window = 0
	if !rep.PaidAt.IsZero() && rep.ExitDeadline.After(rep.PaidAt) {
		r.window = rep.ExitDeadline.Sub(rep.PaidAt)
	}
	f.mu.Unlock()
	return true
}

// scheduleExit draws the 1–10 minute walk, always, so draws never depend on
// the server's answers. A PayLate car waits out its exit window plus a margin
// instead, once, counted from the engine's now.
func (f *FleetSim) scheduleExit(r *carRun) {
	walk := f.between(time.Minute, 10*time.Minute)
	f.mu.Lock()
	at := f.e.Now().Add(walk)
	if r.car.PayLate && !r.lateDone && r.window > 0 {
		r.lateDone = true
		at = f.e.Now().Add(r.window + lateMargin)
	}
	f.mu.Unlock()
	f.e.At(at, func(ctx context.Context) { f.leaveLot(ctx, r) })
}

func (f *FleetSim) leaveLot(ctx context.Context, r *carRun) {
	rep, ok := f.send(ctx, r, f.exit, "exit.credential_presented", map[string]any{"credential": f.credential(r.car)})
	switch {
	case !ok:
		f.halt(r)
	case rep.Result == "open":
		f.after(f.between(3*time.Second, 8*time.Second), func(ctx context.Context) { f.exitPass(ctx, r) })
	case rep.Reason == "gate.payment_required" && rep.AmountCents > 0 && r.repays < maxRepays:
		r.repays++
		if !f.payCash(ctx, r, rep.AmountCents) {
			f.halt(r)
			return
		}
		f.scheduleExit(r)
	default:
		f.halt(r) // denied: the car stays inside, as the server counts it
	}
}

func (f *FleetSim) exitPass(ctx context.Context, r *carRun) {
	rep, ok := f.send(ctx, r, f.exit, "barrier.passed", map[string]any{"direction": "exit", "stay_id": r.car.StayID})
	if !ok || rep.Result != "ack" {
		f.halt(r)
		return
	}
	f.mu.Lock()
	r.car.Inside = false
	f.inside--
	space := r.car.Space
	var report bool
	if space != "" {
		f.free = append(f.free, space)
		report = f.faults[space] != FaultSilent && f.faults[space] != FaultStuckOccupied && !f.dropped()
	}
	f.mu.Unlock()
	if report {
		f.send(ctx, r, f.sensors, "sensor.space_changed", map[string]any{"space_code": space, "occupied": false})
	}
	f.finish(r)
}
