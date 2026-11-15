package simulator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/XYZLZ/MyPark/apps/server/internal/devicefleet"
	"github.com/XYZLZ/MyPark/apps/server/internal/domain/authz"
	"github.com/XYZLZ/MyPark/apps/server/internal/testsupport/fakeclock"
)

const (
	fakeStay       = "019a6d00-0000-7000-8000-0000000000f1"
	fakeTicket     = "TICKET0000000001"
	fakeReplaced   = "REPLACE000000001"
	denyFull       = `{"result":"deny","reason":"gate.lot_full"}`
	denyPayment    = `{"result":"deny","reason":"gate.payment_required","stay_id":"` + fakeStay + `","amount_cents":7777}`
	denyNotAllowed = `{"result":"deny","reason":"gate.exit_not_authorized","stay_id":"` + fakeStay + `"}`
)

type apiCall struct {
	Kind    authz.DeviceKind
	Type    string
	Payload map[string]any
}

// fakeAPI answers device events by type. script holds replies (the "data"
// object) consumed in order per type; the last one repeats. Without a script
// the happy-path default answers.
type fakeAPI struct {
	mu     sync.Mutex
	now    func() time.Time
	calls  []apiCall
	script map[string][]string
	used   map[string]int
}

func (a *fakeAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	raw, _ := io.ReadAll(r.Body)
	var ev struct {
		Type    string         `json:"type"`
		Payload map[string]any `json:"payload"`
	}
	if err := json.Unmarshal(raw, &ev); err != nil {
		http.Error(w, "bad body", http.StatusBadRequest)
		return
	}
	id, _ := uuid.Parse(r.Header.Get("X-Device-Id"))
	var kind authz.DeviceKind
	for _, d := range devicefleet.Devices() {
		if d.PublicID == id {
			kind = d.Kind
		}
	}
	a.mu.Lock()
	a.calls = append(a.calls, apiCall{kind, ev.Type, ev.Payload})
	data := a.reply(kind, ev.Type)
	a.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_, _ = io.WriteString(w, `{"data":`+data+`}`)
}

func (a *fakeAPI) reply(kind authz.DeviceKind, typ string) string {
	if s := a.script[typ]; len(s) > 0 {
		i := min(a.used[typ], len(s)-1)
		a.used[typ]++
		return s[i]
	}
	now := a.now()
	switch typ {
	case "entry.credential_presented":
		if kind == authz.DeviceDispenser {
			return `{"result":"open","stay_id":"` + fakeStay + `","ticket_code":"` + fakeTicket + `"}`
		}
		return `{"result":"open","stay_id":"` + fakeStay + `"}`
	case "paystation.scan":
		return `{"result":"ack","stay_id":"` + fakeStay + `","status":"active","amount_cents":5000}`
	case "paystation.lost_ticket":
		return `{"result":"ack","stay_id":"` + fakeStay + `","ticket_code":"` + fakeReplaced + `","amount_cents":50000}`
	case "paystation.cash_paid":
		return fmt.Sprintf(`{"result":"ack","receipt_no":"MP-1","amount_cents":5000,"paid_at":%q,"exit_deadline":%q}`,
			now.Format(time.RFC3339), now.Add(15*time.Minute).Format(time.RFC3339))
	case "exit.credential_presented":
		return `{"result":"open","stay_id":"` + fakeStay + `"}`
	}
	return `{"result":"ack"}`
}

func (a *fakeAPI) snapshot() []apiCall {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]apiCall(nil), a.calls...)
}

// trace is the call list as "kind:type" (barrier.passed adds the direction,
// sensor.space_changed the occupied flag).
func (a *fakeAPI) trace() []string {
	var out []string
	for _, c := range a.snapshot() {
		s := string(c.Kind) + ":" + c.Type
		switch c.Type {
		case "barrier.passed":
			s += "(" + fmt.Sprint(c.Payload["direction"]) + ")"
		case "sensor.space_changed":
			s += "(" + fmt.Sprint(c.Payload["occupied"]) + ")"
		}
		out = append(out, s)
	}
	return out
}

func (a *fakeAPI) payloads(typ string) []map[string]any {
	var out []map[string]any
	for _, c := range a.snapshot() {
		if c.Type == typ {
			out = append(out, c.Payload)
		}
	}
	return out
}

type rig struct {
	api *fakeAPI
	clk *StepClock
	eng *Engine
	sim *FleetSim
	log *EventLog
}

func newRig(t *testing.T, seed uint64, script map[string][]string) *rig {
	t.Helper()
	clk := NewStepClock(t0)
	api := &fakeAPI{now: clk.Now, script: script, used: map[string]int{}}
	client := NewDeviceClient(ClientConfig{
		BaseURL: "http://api.test", Transport: HandlerTransport(api),
		SignClock: fakeclock.New(t0), FleetSecret: fleetSecret,
	})
	eng := NewEngine(clk, seed)
	log := NewEventLog(500)
	return &rig{api: api, clk: clk, eng: eng, log: log, sim: NewFleetSim(eng, client, fleetSecret, log)}
}

func (r *rig) run(t *testing.T) {
	t.Helper()
	if err := r.eng.Run(context.Background()); err != nil {
		t.Fatalf("run: %v", err)
	}
}

func wantTrace(t *testing.T, got, want []string) {
	t.Helper()
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("trace:\n got  %v\n want %v", got, want)
	}
}

var fullTicketTrace = []string{
	"podium:podium.vehicle_detected",
	"dispenser:entry.credential_presented",
	"entry_barrier:barrier.passed(entry)",
	"sensor_gateway:sensor.space_changed(true)",
	"pay_station:paystation.scan",
	"pay_station:paystation.cash_paid",
	"exit_barrier:exit.credential_presented",
	"exit_barrier:barrier.passed(exit)",
	"sensor_gateway:sensor.space_changed(false)",
}

func TestCar_TicketLifecycleSequence(t *testing.T) {
	r := newRig(t, 1, nil)
	car := &Car{Cred: CredTicket, Dwell: 30 * time.Minute}
	r.sim.Arrive(car)
	if car.ID != "car-000001" {
		t.Errorf("car id = %q", car.ID)
	}
	r.run(t)

	wantTrace(t, r.api.trace(), fullTicketTrace)
	if r.sim.Inside() != 0 {
		t.Errorf("Inside = %d, want 0", r.sim.Inside())
	}
	if p := r.api.payloads("paystation.scan")[0]["credential"].(map[string]any); p["kind"] != "ticket" || p["value"] != fakeTicket {
		t.Errorf("scan credential = %v", p)
	}
	if p := r.api.payloads("paystation.cash_paid")[0]; p["amount_cents"] != float64(5000) || p["stay_id"] != fakeStay {
		t.Errorf("cash payload = %v", p)
	}
	if p := r.api.payloads("exit.credential_presented")[0]["credential"].(map[string]any); p["value"] != fakeTicket {
		t.Errorf("exit credential = %v", p)
	}
	sensors := r.api.payloads("sensor.space_changed")
	if sensors[0]["space_code"] != sensors[1]["space_code"] || sensors[0]["space_code"] == "" {
		t.Errorf("sensor events disagree on the space: %v", sensors)
	}

	// FR-SIM-4 timing, read off the log.
	at := map[string]time.Time{}
	for _, e := range r.log.After(0) {
		if e.CarID != car.ID {
			t.Errorf("entry %+v has car id %q", e, e.CarID)
		}
		if k := e.Type + "/" + e.Device; at[k].IsZero() {
			at[k] = e.At // the first of its kind
		}
	}
	between := func(what string, from, to time.Time, lo, hi time.Duration) {
		t.Helper()
		if d := to.Sub(from); d < lo || d > hi {
			t.Errorf("%s took %v, want %v..%v", what, d, lo, hi)
		}
	}
	cred := at["entry.credential_presented/dispenser-entry-1"]
	pass := at["barrier.passed/barrier-entry-1"]
	occ := at["sensor.space_changed/sensors-1"]
	between("entry barrier", cred, pass, 3*time.Second, 8*time.Second)
	between("occupied sensor", pass, occ, 20*time.Second, 60*time.Second)
	between("dwell", occ, at["paystation.scan/paystation-1"], 30*time.Minute, 30*time.Minute)
	between("walk", at["paystation.cash_paid/paystation-1"], at["exit.credential_presented/barrier-exit-1"], time.Minute, 10*time.Minute)

	raw, _ := json.Marshal(r.log.After(0))
	if strings.Contains(string(raw), fakeTicket) {
		t.Error("the event log carries a ticket code")
	}
}

func TestCar_EmployeeSkipsScan(t *testing.T) {
	r := newRig(t, 2, nil)
	r.sim.Arrive(&Car{Cred: CredEmployeeCard, Card: "EMP-007", Dwell: time.Hour})
	r.run(t)

	wantTrace(t, r.api.trace(), []string{
		"podium:podium.vehicle_detected",
		"entry_barrier:entry.credential_presented",
		"entry_barrier:barrier.passed(entry)",
		"sensor_gateway:sensor.space_changed(true)",
		"exit_barrier:exit.credential_presented",
		"exit_barrier:barrier.passed(exit)",
		"sensor_gateway:sensor.space_changed(false)",
	})
	uid := devicefleet.CardUID(fleetSecret, "EMP-007")
	entry := r.api.payloads("entry.credential_presented")[0]
	if entry["kind"] != "employee_card" || entry["value"] != uid {
		t.Errorf("entry payload = %v, want employee_card %s", entry, uid)
	}
	if exit := r.api.payloads("exit.credential_presented")[0]["credential"].(map[string]any); exit["kind"] != "employee_card" || exit["value"] != uid {
		t.Errorf("exit credential = %v", exit)
	}
	raw, _ := json.Marshal(r.log.After(0))
	if strings.Contains(string(raw), uid) {
		t.Error("the event log carries a card UID")
	}
}

func TestCar_EntryDenyLeaves(t *testing.T) {
	r := newRig(t, 3, map[string][]string{"entry.credential_presented": {denyFull}})
	r.sim.Arrive(&Car{Cred: CredTicket, Dwell: time.Minute})
	r.run(t)

	wantTrace(t, r.api.trace(), []string{"podium:podium.vehicle_detected", "dispenser:entry.credential_presented"})
	logged := r.log.After(0)
	last := logged[len(logged)-1]
	if last.Type != "entry.credential_presented" || last.Result != "deny" || last.Reason != "gate.lot_full" {
		t.Errorf("last log entry = %+v", last)
	}
	if r.sim.Inside() != 0 {
		t.Errorf("Inside = %d", r.sim.Inside())
	}
}

func TestCar_ExitPaymentRequiredRepays(t *testing.T) {
	r := newRig(t, 4, map[string][]string{"exit.credential_presented": {denyPayment, `{"result":"open","stay_id":"` + fakeStay + `"}`}})
	r.sim.Arrive(&Car{Cred: CredTicket, Dwell: time.Minute})
	r.run(t)

	want := append([]string(nil), fullTicketTrace[:7]...)
	want = append(want, "pay_station:paystation.cash_paid", "exit_barrier:exit.credential_presented",
		"exit_barrier:barrier.passed(exit)", "sensor_gateway:sensor.space_changed(false)")
	wantTrace(t, r.api.trace(), want)
	if cash := r.api.payloads("paystation.cash_paid"); cash[1]["amount_cents"] != float64(7777) {
		t.Errorf("re-pay payload = %v, want the denied amount 7777", cash[1])
	}
	if r.sim.Inside() != 0 {
		t.Errorf("Inside = %d", r.sim.Inside())
	}
}

func TestCar_PaymentRequiredLoopIsBounded(t *testing.T) {
	r := newRig(t, 5, map[string][]string{"exit.credential_presented": {denyPayment}})
	r.sim.Arrive(&Car{Cred: CredTicket, Dwell: time.Minute})
	r.run(t)

	if n := len(r.api.payloads("exit.credential_presented")); n != maxRepays+1 {
		t.Errorf("exit attempts = %d, want %d", n, maxRepays+1)
	}
	if r.sim.Inside() != 1 {
		t.Errorf("Inside = %d: a car the server never let out is still inside", r.sim.Inside())
	}
}

func TestCar_ExitNotAuthorizedStaysInside(t *testing.T) {
	r := newRig(t, 6, map[string][]string{"exit.credential_presented": {denyNotAllowed}})
	r.sim.Arrive(&Car{Cred: CredTicket, Dwell: time.Minute})
	r.run(t)

	if got := r.api.trace(); got[len(got)-1] != "exit_barrier:exit.credential_presented" {
		t.Errorf("trace ends %v", got)
	}
	if r.sim.Inside() != 1 {
		t.Errorf("Inside = %d, want 1", r.sim.Inside())
	}
	logged := r.log.After(0)
	if last := logged[len(logged)-1]; last.Reason != "gate.exit_not_authorized" {
		t.Errorf("last log entry = %+v", last)
	}
}

func TestCar_PayLateWaitsPastDeadline(t *testing.T) {
	r := newRig(t, 7, map[string][]string{"exit.credential_presented": {denyPayment, `{"result":"open","stay_id":"` + fakeStay + `"}`}})
	r.sim.Arrive(&Car{Cred: CredTicket, Dwell: time.Minute, PayLate: true})
	r.run(t)

	var paid, exit time.Time
	for _, e := range r.log.After(0) {
		switch {
		case e.Type == "paystation.cash_paid" && paid.IsZero():
			paid = e.At
		case e.Type == "exit.credential_presented" && exit.IsZero():
			exit = e.At
		}
	}
	if d := exit.Sub(paid); d < 16*time.Minute {
		t.Errorf("first exit credential %v after paying, want at least deadline (15m) + 1m", d)
	}
	if cash := r.api.payloads("paystation.cash_paid"); len(cash) != 2 || cash[1]["amount_cents"] != float64(7777) {
		t.Errorf("cash payloads = %v, want a re-pay of 7777", cash)
	}
	if r.sim.Inside() != 0 {
		t.Errorf("Inside = %d", r.sim.Inside())
	}
}

// PF6: the server rejects a RD$0 cash payment (422), so a scan that owes
// nothing goes straight to the exit.
func TestCar_ZeroAmountSkipsCash(t *testing.T) {
	r := newRig(t, 8, map[string][]string{"paystation.scan": {`{"result":"ack","stay_id":"` + fakeStay + `","status":"active","amount_cents":0}`}})
	r.sim.Arrive(&Car{Cred: CredTicket, Dwell: time.Minute})
	r.run(t)

	want := append([]string(nil), fullTicketTrace[:5]...)
	want = append(want, fullTicketTrace[6:]...)
	wantTrace(t, r.api.trace(), want)
	if r.sim.Inside() != 0 {
		t.Errorf("Inside = %d", r.sim.Inside())
	}
}

func TestCar_HeldUntilLeave(t *testing.T) {
	r := newRig(t, 9, nil)
	car := &Car{Cred: CredTicket, Hold: true, Dwell: time.Minute}
	r.sim.Arrive(car)
	r.run(t)

	wantTrace(t, r.api.trace(), fullTicketTrace[:4])
	if r.sim.Inside() != 1 {
		t.Fatalf("Inside = %d, want 1", r.sim.Inside())
	}
	if err := r.sim.Leave("car-999999", false); !errors.Is(err, ErrCarNotFound) {
		t.Errorf("Leave(unknown) = %v, want ErrCarNotFound", err)
	}
	if err := r.sim.Leave(car.ID, false); err != nil {
		t.Fatal(err)
	}
	r.run(t)
	wantTrace(t, r.api.trace(), fullTicketTrace)
	if r.sim.Inside() != 0 {
		t.Errorf("Inside = %d, want 0", r.sim.Inside())
	}
	if err := r.sim.Leave(car.ID, false); !errors.Is(err, ErrCarNotFound) {
		t.Errorf("Leave(gone) = %v, want ErrCarNotFound", err)
	}
}

func TestCar_LeaveBeforeParkedDepartsOnArrival(t *testing.T) {
	r := newRig(t, 10, nil)
	car := &Car{Cred: CredTicket, Hold: true}
	r.sim.Arrive(car)
	if err := r.sim.Leave(car.ID, false); err != nil {
		t.Fatal(err)
	}
	r.run(t)
	wantTrace(t, r.api.trace(), fullTicketTrace)
}

func TestCar_LostTicket(t *testing.T) {
	r := newRig(t, 11, nil)
	r.sim.Arrive(&Car{Cred: CredTicket, Dwell: time.Minute, LoseTicket: true})
	r.run(t)

	want := append([]string(nil), fullTicketTrace[:4]...)
	want = append(want, "pay_station:paystation.lost_ticket")
	want = append(want, fullTicketTrace[4:]...)
	wantTrace(t, r.api.trace(), want)
	if p := r.api.payloads("paystation.lost_ticket")[0]; p["stay_id"] != fakeStay {
		t.Errorf("lost_ticket payload = %v", p)
	}
	for _, typ := range []string{"paystation.scan", "exit.credential_presented"} {
		if c := r.api.payloads(typ)[0]["credential"].(map[string]any); c["value"] != fakeReplaced {
			t.Errorf("%s credential = %v, want the replacement code", typ, c)
		}
	}
}

func TestCar_LeaveWithLostTicketOnHeldCar(t *testing.T) {
	r := newRig(t, 12, nil)
	car := &Car{Cred: CredTicket, Hold: true}
	r.sim.Arrive(car)
	r.run(t)
	if err := r.sim.Leave(car.ID, true); err != nil {
		t.Fatal(err)
	}
	r.run(t)
	if n := len(r.api.payloads("paystation.lost_ticket")); n != 1 {
		t.Errorf("lost_ticket events = %d, want 1", n)
	}
}

func TestFleetSim_SetFault(t *testing.T) {
	r := newRig(t, 13, nil)
	if err := r.sim.SetFault("E999", FaultSilent); err == nil {
		t.Error("SetFault on an unknown space succeeded")
	}
	r.sim.SetSpaces([]string{"E001"})
	if err := r.sim.SetFault("E001", FaultStuckFree); err != nil {
		t.Fatal(err)
	}
	r.sim.Arrive(&Car{Cred: CredTicket, Dwell: time.Minute})
	r.run(t)
	// stuck_free never reports occupied; the car's free event still goes out.
	got := r.api.trace()
	for _, s := range got {
		if s == "sensor_gateway:sensor.space_changed(true)" {
			t.Errorf("a stuck-free sensor reported occupied: %v", got)
		}
	}
	if err := r.sim.SetFault("E001", FaultSilent); err != nil {
		t.Fatal(err)
	}
	r.sim.Arrive(&Car{Cred: CredTicket, Dwell: time.Minute})
	r.run(t)
	if n := len(r.api.payloads("sensor.space_changed")); n != 1 {
		t.Errorf("sensor events = %d: a silent sensor sent more", n)
	}
}

// FR-SIM-7: same seed, same trace, even when steps draw from Rand() to
// schedule follow-ups. Event ids are random and are left out.
func TestFleetSim_SameSeedSameTrace(t *testing.T) {
	run := func(seed uint64) ([]LogEntry, []apiCall) {
		r := newRig(t, seed, nil)
		var spawn func(n int) func(context.Context)
		spawn = func(n int) func(context.Context) {
			return func(context.Context) {
				kind, dwell := CredTicket, time.Duration(1+r.eng.Rand().IntN(90))*time.Minute
				if r.eng.Rand().IntN(4) == 0 {
					kind = CredEmployeeCard
				}
				r.sim.Arrive(&Car{Cred: kind, Card: "EMP-001", Dwell: dwell, PayLate: r.eng.Rand().IntN(5) == 0})
				if n > 0 {
					next := time.Duration(1+r.eng.Rand().IntN(600)) * time.Second
					r.eng.At(r.eng.Now().Add(next), spawn(n-1))
				}
			}
		}
		r.eng.At(t0, spawn(11))
		r.run(t)
		return r.log.After(0), r.api.snapshot()
	}
	logA, callsA := run(42)
	logB, callsB := run(42)
	if len(logA) < 50 {
		t.Fatalf("only %d log entries: the run did not exercise the lifecycle", len(logA))
	}
	if fmt.Sprint(logA) != fmt.Sprint(logB) || fmt.Sprint(callsA) != fmt.Sprint(callsB) {
		t.Error("two runs with seed 42 differ")
	}
	if logC, _ := run(43); fmt.Sprint(logA) == fmt.Sprint(logC) {
		t.Error("seeds 42 and 43 gave the same log")
	}
}
