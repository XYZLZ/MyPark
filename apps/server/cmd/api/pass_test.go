package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/XYZLZ/MyPark/apps/server/internal/domain/authz"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/gate"
	"github.com/XYZLZ/MyPark/apps/server/internal/testsupport"
)

type passData struct {
	Result string `json:"result"`
	Reason string `json:"reason"`
}

func passOf(t *testing.T, rec *httptest.ResponseRecorder) passData {
	t.Helper()
	var d passData
	if err := json.Unmarshal([]byte(dataOf(t, rec)), &d); err != nil {
		t.Fatalf("pass data: %v", err)
	}
	return d
}

func (e *deviceEnv) passBody(direction string, stayID uuid.UUID) []byte {
	e.t.Helper()
	return e.eventBody(uuid.New(), "barrier.passed", map[string]any{"direction": direction, "stay_id": stayID})
}

func (e *deviceEnv) pass(d testsupport.TestDevice, stayID uuid.UUID) passData {
	e.t.Helper()
	return passOf(e.t, e.send(d, e.passBody(gate.DirectionEntry, stayID)))
}

// openStay opens an entry_pending stay through the device API: a ticket, or
// a card when d is an entry barrier and uid is set.
func (e *deviceEnv) openStay(d testsupport.TestDevice, uid string) uuid.UUID {
	e.t.Helper()
	var rec *httptest.ResponseRecorder
	if uid == "" {
		rec = e.ticketRequest(d)
	} else {
		rec = e.entry(d, gate.KindEmployeeCard, uid)
	}
	got := entryOf(e.t, rec)
	if got.Result != "open" || got.StayID == nil {
		e.t.Fatalf("entry = %+v, want open", got)
	}
	return *got.StayID
}

type stayRow struct {
	Status              string
	AcceptedAt          time.Time
	EnteredAt, VoidedAt *time.Time
}

func (e *deviceEnv) stay(id uuid.UUID) stayRow {
	e.t.Helper()
	var s stayRow
	if err := e.db.Super.QueryRow(e.t.Context(),
		`select status, accepted_at, entered_at, voided_at from stays where public_id = $1`, id).
		Scan(&s.Status, &s.AcceptedAt, &s.EnteredAt, &s.VoidedAt); err != nil {
		e.t.Fatalf("stay %s: %v", id, err)
	}
	return s
}

func (e *deviceEnv) occupancy() (occupied, version int64) {
	e.t.Helper()
	if err := e.db.Super.QueryRow(e.t.Context(),
		`select occupied, version from lot_occupancy where lot_id = $1`, e.lotID).Scan(&occupied, &version); err != nil {
		e.t.Fatal(err)
	}
	return occupied, version
}

// voidStale advances the clock past the 60 s window and runs the void job.
func (e *deviceEnv) voidStale() time.Time {
	e.t.Helper()
	e.clk.Advance(61 * time.Second)
	if _, err := gate.VoidUnpassedEntries(e.t.Context(), e.db.App, e.clk.Now()); err != nil {
		e.t.Fatalf("void: %v", err)
	}
	return e.clk.Now()
}

func TestPass_EntryActivatesAndNotifies(t *testing.T) {
	e := newDeviceEnv(t)
	id := e.openStay(e.device(authz.DeviceDispenser), "")
	accepted := e.clk.Now()
	e.clk.Advance(5 * time.Second)
	occ, ver := e.occupancy()
	l := e.listen("lot_changed")

	if got := e.pass(e.device(authz.DeviceEntryBarrier), id); got != (passData{Result: "ack"}) {
		t.Fatalf("answer = %+v, want a plain ack", got)
	}
	s := e.stay(id)
	if s.Status != "active" || s.EnteredAt == nil || !s.EnteredAt.Equal(e.clk.Now()) || !s.AcceptedAt.Equal(accepted) {
		t.Errorf("stay = %+v, want active, entered at %v, accepted at %v", s, e.clk.Now(), accepted)
	}
	occ2, ver2 := e.occupancy()
	if occ2 != occ+1 || ver2 != ver+1 {
		t.Errorf("occupancy = %d v%d, want %d v%d", occ2, ver2, occ+1, ver+1)
	}
	if got, want := l.drain(), fmt.Sprintf("%d:%d", e.lotID, ver2); len(got) != 1 || got[0] != want {
		t.Errorf("lot_changed = %v, want [%s]", got, want)
	}
	e.invariant()
}

func TestPass_DuplicateEntry(t *testing.T) {
	e := newDeviceEnv(t)
	barrier := e.device(authz.DeviceEntryBarrier)
	id := e.openStay(e.device(authz.DeviceDispenser), "")
	if got := e.pass(barrier, id); got.Result != "ack" || got.Reason != "" {
		t.Fatalf("first pass = %+v, want ack", got)
	}
	occ, ver := e.occupancy()
	dup := passData{Result: "ack", Reason: "gate.duplicate_pass"}
	if got := e.pass(barrier, id); got != dup {
		t.Errorf("second pass = %+v, want %+v", got, dup)
	}
	if _, err := e.db.Super.Exec(t.Context(),
		`update stays set status = 'paid', paid_at = $2, exit_deadline_at = $2 where public_id = $1`, id, e.clk.Now()); err != nil {
		t.Fatal(err)
	}
	if got := e.pass(barrier, id); got != dup {
		t.Errorf("pass of a paid stay = %+v, want %+v", got, dup)
	}
	if occ2, ver2 := e.occupancy(); occ2 != occ || ver2 != ver {
		t.Errorf("occupancy = %d v%d, want %d v%d unchanged", occ2, ver2, occ, ver)
	}
	e.invariant()
}

// Two passes of one stay at once: the stay is counted exactly once.
func TestPass_ConcurrentSameStay(t *testing.T) {
	e := newDeviceEnv(t)
	barrier := e.device(authz.DeviceEntryBarrier)
	id := e.openStay(e.device(authz.DeviceDispenser), "")
	occ, _ := e.occupancy()

	recs := make([]*httptest.ResponseRecorder, 2)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range recs {
		req := barrier.Request(t, deviceEventsPath, e.passBody(gate.DirectionEntry, id), e.clk.Now())
		req.RemoteAddr = "203.0.113.7:5555"
		wg.Go(func() {
			<-start
			recs[i] = httptest.NewRecorder()
			e.h.ServeHTTP(recs[i], req)
		})
	}
	close(start)
	wg.Wait()
	reasons := map[string]int{}
	for _, rec := range recs {
		got := passOf(t, rec)
		if got.Result != "ack" {
			t.Errorf("answer = %+v, want ack", got)
		}
		reasons[got.Reason]++
	}
	if reasons[""] != 1 || reasons["gate.duplicate_pass"] != 1 {
		t.Errorf("reasons = %v, want one plain ack and one duplicate_pass", reasons)
	}
	if occ2, _ := e.occupancy(); occ2 != occ+1 {
		t.Errorf("occupied = %d, want %d", occ2, occ+1)
	}
	e.invariant()
}

// A car that passes after its entry was voided is inside: it is counted,
// logged at warn and audited (M2C-31).
func TestPass_LateEntryOnVoided(t *testing.T) {
	var logs bytes.Buffer
	e := newDeviceEnv(t, func(d *deps) { d.log = slog.New(slog.NewJSONHandler(&logs, nil)) })
	id := e.openStay(e.device(authz.DeviceDispenser), "")
	voidedAt := e.voidStale()
	if s := e.stay(id); s.Status != "voided" {
		t.Fatalf("stay = %s, want voided before the pass", s.Status)
	}
	e.clk.Advance(10 * time.Second)
	occ, ver := e.occupancy()

	if got := e.pass(e.device(authz.DeviceEntryBarrier), id); got != (passData{Result: "ack"}) {
		t.Fatalf("late pass = %+v, want ack", got)
	}
	s := e.stay(id)
	if s.Status != "active" || s.EnteredAt == nil || !s.EnteredAt.Equal(e.clk.Now()) || s.VoidedAt == nil || !s.VoidedAt.Equal(voidedAt) {
		t.Errorf("stay = %+v, want active entered at %v with voided_at %v kept", s, e.clk.Now(), voidedAt)
	}
	if occ2, ver2 := e.occupancy(); occ2 != occ+1 || ver2 != ver+1 {
		t.Errorf("occupancy = %d v%d, want %d v%d", occ2, ver2, occ+1, ver+1)
	}
	if n := e.count(`select count(*) from audit_log where action = 'gate.late_entry_pass'
		and actor_kind = 'system' and entity_type = 'stay' and entity_id = $1`, id.String()); n != 1 {
		t.Errorf("late pass audit rows = %d, want 1", n)
	}
	if !strings.Contains(logs.String(), `"level":"WARN","msg":"gate: late entry pass`) {
		t.Errorf("no warn log for the late pass:\n%s", logs.String())
	}
	e.invariant()
}

// A voided card stay cannot come back while the card has a newer open stay
// (one open stay per card, M2C-28).
func TestPass_LateEntryCardConflict(t *testing.T) {
	e := newDeviceEnv(t)
	barrier := e.device(authz.DeviceEntryBarrier)
	uid := e.cardUID("EMP-007")
	old := e.openStay(barrier, uid)
	e.voidStale()
	newer := e.openStay(barrier, uid)
	occ, ver := e.occupancy()

	want := passData{Result: "rejected", Reason: "gate.credential_in_use"}
	if got := e.pass(barrier, old); got != want {
		t.Fatalf("late pass = %+v, want %+v", got, want)
	}
	if s := e.stay(old); s.Status != "voided" || s.EnteredAt != nil {
		t.Errorf("old stay = %+v, want voided and never entered", s)
	}
	if s := e.stay(newer); s.Status != "entry_pending" {
		t.Errorf("newer stay = %s, want entry_pending", s.Status)
	}
	if occ2, ver2 := e.occupancy(); occ2 != occ || ver2 != ver {
		t.Errorf("occupancy = %d v%d, want %d v%d unchanged", occ2, ver2, occ, ver)
	}
	if n := e.count(`select count(*) from audit_log where action = 'gate.late_entry_pass'`); n != 0 {
		t.Errorf("late pass audit rows = %d, want 0", n)
	}
	e.invariant()
}

func TestPass_UnknownOrOtherLotStay(t *testing.T) {
	e := newDeviceEnv(t)
	barrier := e.device(authz.DeviceEntryBarrier)
	other := testsupport.SeedSecondLot(t, e.db)
	stayIn := func(lotID int64, status string) uuid.UUID {
		t.Helper()
		var id uuid.UUID
		if err := e.db.Super.QueryRow(t.Context(),
			`insert into stays (lot_id, credential_kind, ticket_code_hash, status, tariff_version_id,
			                    accepted_at, entered_at, exited_at, updated_at)
			 select $1, 'ticket', sha256(gen_random_uuid()::text::bytea), $2, v.id, $3,
			        case when $2 = 'exited' then $3::timestamptz end, case when $2 = 'exited' then $3::timestamptz end, $3
			 from (select id from tariff_versions where lot_id = $1 order by effective_from desc limit 1) v
			 returning public_id`, lotID, status, e.clk.Now()).Scan(&id); err != nil {
			t.Fatalf("stay in lot %d: %v", lotID, err)
		}
		return id
	}
	otherLot := stayIn(other, "entry_pending")
	exited := stayIn(e.lotID, "exited")
	occ, ver := e.occupancy()

	want := passData{Result: "rejected", Reason: "gate.stay_unknown"}
	for name, id := range map[string]uuid.UUID{"random": uuid.New(), "other lot": otherLot, "exited": exited} {
		if got := e.pass(barrier, id); got != want {
			t.Errorf("%s: answer = %+v, want %+v", name, got, want)
		}
	}
	if s := e.stay(otherLot); s.Status != "entry_pending" {
		t.Errorf("other lot's stay = %s, want untouched", s.Status)
	}
	if occ2, ver2 := e.occupancy(); occ2 != occ || ver2 != ver {
		t.Errorf("occupancy = %d v%d, want %d v%d unchanged", occ2, ver2, occ, ver)
	}
	e.invariant()
	testsupport.AssertOccupancyInvariant(t, e.db.App, other)
}

// Exit passes arrive with S9.2: until then they are unhandled and store nothing.
func TestPass_ExitStillUnhandled(t *testing.T) {
	e := newDeviceEnv(t)
	rec := e.send(e.device(authz.DeviceExitBarrier), e.passBody(gate.DirectionExit, uuid.New()))
	wantError(t, rec, http.StatusInternalServerError, "internal.unexpected")
	if n := e.eventRows(); n != 0 {
		t.Errorf("events = %d, want 0", n)
	}
	e.invariant()
}
