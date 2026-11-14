package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/XYZLZ/MyPark/apps/server/internal/domain/authz"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/gate"
	"github.com/XYZLZ/MyPark/apps/server/internal/testsupport"
)

// exitEnv is a pay env with an exit barrier.
type exitEnv struct {
	*payEnv
	barrier testsupport.TestDevice
}

func newExitEnv(t *testing.T) *exitEnv {
	t.Helper()
	e := &exitEnv{payEnv: newPayEnv(t)}
	e.barrier = e.device(authz.DeviceExitBarrier)
	return e
}

func (e *exitEnv) exitCredBody(kind, value string) []byte {
	e.t.Helper()
	return e.eventBody(uuid.New(), "exit.credential_presented",
		map[string]any{"credential": map[string]any{"kind": kind, "value": value}})
}

func (e *exitEnv) exitCred(kind, value string) payData {
	e.t.Helper()
	return payOf(e.t, e.send(e.barrier, e.exitCredBody(kind, value)))
}

func (e *exitEnv) exitPass(id uuid.UUID) passData {
	e.t.Helper()
	return passOf(e.t, e.send(e.barrier, e.passBody(gate.DirectionExit, id)))
}

// wantOpen checks an exit credential opened for the stay with no amount.
func wantOpen(t *testing.T, got payData, stayID uuid.UUID) {
	t.Helper()
	if got.Result != "open" || got.Reason != "" || got.StayID == nil || *got.StayID != stayID || got.AmountCents != nil {
		t.Fatalf("exit credential = %+v, want open for %s", got, stayID)
	}
}

// wantPaymentRequired checks an exit credential denied for an amount due.
func wantPaymentRequired(t *testing.T, got payData, stayID uuid.UUID, amount int64) {
	t.Helper()
	if got.Result != "deny" || got.Reason != "gate.payment_required" || got.StayID == nil || *got.StayID != stayID ||
		got.amount() != amount || got.Breakdown == nil {
		t.Fatalf("exit credential = %+v, want deny gate.payment_required %d with a breakdown", got, amount)
	}
}

type exitRow struct {
	Status                     string
	PaidAt, ExitedAt, AuthedAt *time.Time
}

func (e *exitEnv) exitRow(id uuid.UUID) exitRow {
	e.t.Helper()
	var r exitRow
	if err := e.db.Super.QueryRow(e.t.Context(),
		`select status, paid_at, exited_at, exit_authorized_at from stays where public_id = $1`, id).
		Scan(&r.Status, &r.PaidAt, &r.ExitedAt, &r.AuthedAt); err != nil {
		e.t.Fatalf("stay %s: %v", id, err)
	}
	return r
}

// wantZeroPayment checks the stay's RD$0 payment, written by the exit pass at now.
func (e *exitEnv) wantZeroPayment(id uuid.UUID, now time.Time) {
	e.t.Helper()
	var method, status string
	var amount int64
	var succeededAt time.Time
	if err := e.db.Super.QueryRow(e.t.Context(),
		`select p.method, p.status, p.amount_cents, p.succeeded_at from payments p join stays s on s.id = p.stay_id
		 where s.public_id = $1 and p.idempotency_key = $2`, id, "zero:"+id.String()).
		Scan(&method, &status, &amount, &succeededAt); err != nil {
		e.t.Fatalf("zero payment: %v", err)
	}
	if method != "none" || status != "succeeded" || amount != 0 || !succeededAt.Equal(now) {
		e.t.Errorf("zero payment = %s %s %d at %v, want none succeeded 0 at %v", method, status, amount, succeededAt, now)
	}
}

func TestExit_PaidInsideWindowOpens(t *testing.T) {
	e := newExitEnv(t)
	paid := e.payAt(time.Hour, 5000)
	e.at(70 * time.Minute)
	wantOpen(t, e.exitCred(gate.KindTicket, e.ticketCode), e.stayID)
	if r := e.exitRow(e.stayID); r.Status != "paid" || r.AuthedAt == nil || !r.AuthedAt.Equal(e.clk.Now()) {
		t.Fatalf("stay = %+v, want paid and authorized at %v", r, e.clk.Now())
	}

	now := e.at(70*time.Minute + 20*time.Second)
	occ, ver := e.occupancy()
	l := e.listen("lot_changed")
	if got := e.exitPass(e.stayID); got != (passData{Result: "ack"}) {
		t.Fatalf("exit pass = %+v, want a plain ack", got)
	}
	r := e.exitRow(e.stayID)
	if r.Status != "exited" || r.ExitedAt == nil || !r.ExitedAt.Equal(now) || r.PaidAt == nil || !r.PaidAt.Equal(*paid.PaidAt) {
		t.Errorf("stay = %+v, want exited at %v with paid_at %v kept", r, now, *paid.PaidAt)
	}
	occ2, ver2 := e.occupancy()
	if occ2 != occ-1 || ver2 != ver+1 {
		t.Errorf("occupancy = %d v%d, want %d v%d", occ2, ver2, occ-1, ver+1)
	}
	if got, want := l.drain(), fmt.Sprintf("%d:%d", e.lotID, ver2); len(got) != 1 || got[0] != want {
		t.Errorf("lot_changed = %v, want [%s]", got, want)
	}
	if n := e.payments(); n != 1 {
		t.Errorf("payments = %d, want only the cash one", n)
	}
	e.invariant()
}

func TestExit_GraceZeroPayment(t *testing.T) {
	e := newExitEnv(t)
	e.at(5 * time.Minute)
	wantOpen(t, e.exitCred(gate.KindTicket, e.ticketCode), e.stayID)
	if n := e.payments(); n != 0 {
		t.Fatalf("payments after the credential = %d, want 0 (the zero payment is the pass's)", n)
	}
	occ, _ := e.occupancy()
	now := e.at(5*time.Minute + 30*time.Second)
	if got := e.exitPass(e.stayID); got != (passData{Result: "ack"}) {
		t.Fatalf("exit pass = %+v, want ack", got)
	}
	r := e.exitRow(e.stayID)
	if r.Status != "exited" || r.ExitedAt == nil || !r.ExitedAt.Equal(now) || r.PaidAt == nil || !r.PaidAt.Equal(*r.ExitedAt) {
		t.Errorf("stay = %+v, want exited with paid_at == exited_at == %v", r, now)
	}
	if n := e.payments(); n != 1 {
		t.Errorf("payments = %d, want 1", n)
	}
	e.wantZeroPayment(e.stayID, now)
	if occ2, _ := e.occupancy(); occ2 != occ-1 {
		t.Errorf("occupied = %d, want %d", occ2, occ-1)
	}
	e.invariant()
}

// M2C-32: the zero decision is taken at the credential, so a car authorized
// inside grace is not charged for passing the barrier after it.
func TestExit_DecisionAtAuthorization(t *testing.T) {
	e := newExitEnv(t)
	e.at(9*time.Minute + 58*time.Second)
	wantOpen(t, e.exitCred(gate.KindTicket, e.ticketCode), e.stayID)
	now := e.at(10*time.Minute + 2*time.Second)
	if got := e.exitPass(e.stayID); got != (passData{Result: "ack"}) {
		t.Fatalf("exit pass = %+v, want ack", got)
	}
	if r := e.exitRow(e.stayID); r.Status != "exited" {
		t.Errorf("stay = %+v, want exited", r)
	}
	if n := e.count(`select coalesce(sum(amount_cents), 0) from payments`); n != 0 || e.payments() != 1 {
		t.Errorf("charged %d over %d payments, want RD$0 in one zero payment", n, e.payments())
	}
	e.wantZeroPayment(e.stayID, now)
	e.invariant()
}

func TestExit_PaymentRequired(t *testing.T) {
	e := newExitEnv(t)
	e.at(2 * time.Hour)
	wantPaymentRequired(t, e.exitCred(gate.KindTicket, e.ticketCode), e.stayID, 7500)
	if r := e.exitRow(e.stayID); r.Status != "active" || r.AuthedAt != nil {
		t.Errorf("stay = %+v, want active and not authorized", r)
	}
	wantRejectedPass(t, e.exitPass(e.stayID), "gate.exit_not_authorized")
	e.invariant()
}

func wantRejectedPass(t *testing.T, got passData, reason string) {
	t.Helper()
	if got != (passData{Result: "rejected", Reason: reason}) {
		t.Fatalf("exit pass = %+v, want rejected %s", got, reason)
	}
}

// O8, both halves (S2.1 carry): an overstay is never a zero exit; paying the
// overstay opens the exit, inside the new window.
func TestExit_O8FlowAgreement(t *testing.T) {
	e := newExitEnv(t)
	first := e.payAt(90*time.Minute, 7500)
	e.at(110 * time.Minute)
	if got := e.scan(gate.KindTicket, e.ticketCode); got.Status != "active" || got.amount() != 5000 {
		t.Fatalf("scan = %+v, want active 5000", got)
	}
	if s := e.payStay(); s.Exit == nil || !s.Exit.Equal(*first.ExitDeadline) {
		t.Fatalf("stay = %+v, want the deadline %v kept", s, *first.ExitDeadline)
	}
	wantPaymentRequired(t, e.exitCred(gate.KindTicket, e.ticketCode), e.stayID, 5000)
	if got := e.cash(5000); got.Result != "ack" {
		t.Fatalf("cash 5000 = %+v, want ack", got)
	}

	e.at(115 * time.Minute)
	wantOpen(t, e.exitCred(gate.KindTicket, e.ticketCode), e.stayID)
	if got := e.exitPass(e.stayID); got != (passData{Result: "ack"}) {
		t.Fatalf("exit pass = %+v, want ack", got)
	}
	if r := e.exitRow(e.stayID); r.Status != "exited" || !r.PaidAt.Equal(e.t0.Add(110*time.Minute)) {
		t.Errorf("stay = %+v, want exited with paid_at t0+1h50", r)
	}
	if n := e.payments(); n != 2 {
		t.Errorf("payments = %d, want 2 (no zero payment for a paid stay)", n)
	}
	e.invariant()
}

// Paid, then the window closes before the exit: the overstay is due again.
func TestExit_AfterWindowPaymentRequired(t *testing.T) {
	e := newExitEnv(t)
	e.payAt(90*time.Minute, 7500)
	e.at(106 * time.Minute)
	wantPaymentRequired(t, e.exitCred(gate.KindTicket, e.ticketCode), e.stayID, 5000)
	if r := e.exitRow(e.stayID); r.Status != "active" || r.AuthedAt != nil {
		t.Errorf("stay = %+v, want active (lazily reactivated) and not authorized", r)
	}
	e.invariant()
}

func TestExitPass_NotAuthorized(t *testing.T) {
	e := newExitEnv(t)
	e.at(5 * time.Minute)
	occ, ver := e.occupancy()
	wantRejectedPass(t, e.exitPass(e.stayID), "gate.exit_not_authorized") // tailgating: no credential

	wantOpen(t, e.exitCred(gate.KindTicket, e.ticketCode), e.stayID)
	e.clk.Advance(61 * time.Second)
	wantRejectedPass(t, e.exitPass(e.stayID), "gate.exit_not_authorized") // stale authorization

	if r := e.exitRow(e.stayID); r.Status != "active" || r.ExitedAt != nil || r.PaidAt != nil {
		t.Errorf("stay = %+v, want active, never paid or exited", r)
	}
	if occ2, ver2 := e.occupancy(); occ2 != occ || ver2 != ver {
		t.Errorf("occupancy = %d v%d, want %d v%d unchanged", occ2, ver2, occ, ver)
	}
	if n := e.payments(); n != 0 {
		t.Errorf("payments = %d, want 0", n)
	}

	// Exactly 60 s after the authorization is still fresh.
	wantOpen(t, e.exitCred(gate.KindTicket, e.ticketCode), e.stayID)
	e.clk.Advance(60 * time.Second)
	if got := e.exitPass(e.stayID); got != (passData{Result: "ack"}) {
		t.Errorf("pass 60 s after the authorization = %+v, want ack", got)
	}
	e.invariant()
}

func TestExitPass_DuplicateAndUnknown(t *testing.T) {
	e := newExitEnv(t)
	e.at(5 * time.Minute)
	wantOpen(t, e.exitCred(gate.KindTicket, e.ticketCode), e.stayID)
	if got := e.exitPass(e.stayID); got != (passData{Result: "ack"}) {
		t.Fatalf("exit pass = %+v, want ack", got)
	}
	occ, ver := e.occupancy()
	if got := e.exitPass(e.stayID); got != (passData{Result: "ack", Reason: "gate.duplicate_pass"}) {
		t.Errorf("second exit pass = %+v, want ack gate.duplicate_pass", got)
	}

	voided := e.openStay(e.device(authz.DeviceDispenser), "")
	e.voidStale()
	pending := e.openStay(e.device(authz.DeviceDispenser), "")
	for name, id := range map[string]uuid.UUID{"entry_pending": pending, "voided": voided, "random": uuid.New()} {
		t.Run(name, func(t *testing.T) { wantRejectedPass(t, e.exitPass(id), "gate.stay_unknown") })
	}
	if occ2, ver2 := e.occupancy(); occ2 != occ || ver2 != ver {
		t.Errorf("occupancy = %d v%d, want %d v%d unchanged", occ2, ver2, occ, ver)
	}
	if n := e.payments(); n != 1 {
		t.Errorf("payments = %d, want the one zero payment", n)
	}
	e.invariant()
}

func TestExit_EmployeeCard(t *testing.T) {
	e := newExitEnv(t)
	entryBarrier := e.device(authz.DeviceEntryBarrier)
	uid := e.cardUID("EMP-007")
	card := e.openStay(entryBarrier, uid)
	if p := e.pass(entryBarrier, card); p.Result != "ack" {
		t.Fatalf("card entry pass = %+v", p)
	}
	e.at(30 * time.Hour)
	wantOpen(t, e.exitCred(gate.KindEmployeeCard, uid), card)
	now := e.at(30*time.Hour + 10*time.Second)
	if got := e.exitPass(card); got != (passData{Result: "ack"}) {
		t.Fatalf("exit pass = %+v, want ack", got)
	}
	if r := e.exitRow(card); r.Status != "exited" || !r.PaidAt.Equal(now) {
		t.Errorf("card stay = %+v, want exited, paid at %v", r, now)
	}
	e.wantZeroPayment(card, now)
	// Out of the lot, the card can come back in.
	if got := entryOf(t, e.entry(entryBarrier, gate.KindEmployeeCard, uid)); got.Result != "open" {
		t.Errorf("re-entry = %+v, want open", got)
	}
	e.invariant()
}

// S9.1 ruling: a card revoked while its car is inside can still leave
// through its open stay, audited; it cannot start a new stay.
func TestExit_RevokedCardInsideMayLeave(t *testing.T) {
	e := newExitEnv(t)
	entryBarrier := e.device(authz.DeviceEntryBarrier)
	uid := e.cardUID("EMP-007")
	card := e.openStay(entryBarrier, uid)
	if p := e.pass(entryBarrier, card); p.Result != "ack" {
		t.Fatalf("card entry pass = %+v", p)
	}
	e.at(2 * time.Hour)
	var cardID string
	if err := e.db.Super.QueryRow(t.Context(),
		`update employee_cards set status = 'revoked', revoked_at = $1 where employee_code = 'EMP-007'
		 returning public_id::text`, e.clk.Now()).Scan(&cardID); err != nil {
		t.Fatal(err)
	}

	wantOpen(t, e.exitCred(gate.KindEmployeeCard, uid), card)
	var entity, auditCard, auditDevice string
	if err := e.db.Super.QueryRow(t.Context(),
		`select entity_type || ':' || entity_id, after->>'employee_card_id', after->>'device_id'
		 from audit_log where action = 'gate.revoked_card_exit' and actor_kind = 'system'`).
		Scan(&entity, &auditCard, &auditDevice); err != nil {
		t.Fatalf("revoked card exit audit: %v", err)
	}
	if entity != "stay:"+card.String() || auditCard != cardID || auditDevice != e.barrier.PublicID.String() {
		t.Errorf("audit = %s card %s device %s, want stay:%s card %s device %s",
			entity, auditCard, auditDevice, card, cardID, e.barrier.PublicID)
	}
	if got := e.exitPass(card); got != (passData{Result: "ack"}) {
		t.Fatalf("exit pass = %+v, want ack", got)
	}
	if r := e.exitRow(card); r.Status != "exited" {
		t.Errorf("card stay = %+v, want exited", r)
	}

	// Outside now: the revoked card neither exits again nor enters.
	got := e.exitCred(gate.KindEmployeeCard, uid)
	if got.Result != "deny" || got.Reason != "gate.credential_invalid" || got.StayID != nil {
		t.Errorf("exit credential of a revoked card outside = %+v, want deny gate.credential_invalid", got)
	}
	wantDeny(t, e.entry(entryBarrier, gate.KindEmployeeCard, uid), "gate.credential_invalid")
	e.invariant()
}

func TestExit_CredentialInvalid(t *testing.T) {
	e := newExitEnv(t)
	other := testsupport.SeedSecondLot(t, e.db)
	otherDispenser := testsupport.NewTestDevice(t, e.db, e.d.sealer, other, authz.DeviceDispenser)
	otherEntry := entryOf(t, e.ticketRequest(otherDispenser))
	otherCode := otherEntry.TicketCode
	// Inside its own lot and in grace: only the lot filter refuses it here.
	if p := e.pass(testsupport.NewTestDevice(t, e.db, e.d.sealer, other, authz.DeviceEntryBarrier), *otherEntry.StayID); p.Result != "ack" {
		t.Fatalf("other lot's entry pass = %+v", p)
	}
	pendingCode := entryOf(t, e.ticketRequest(e.device(authz.DeviceDispenser))).TicketCode
	e.at(5 * time.Minute)
	for name, cred := range map[string][2]string{
		"unknown":       {gate.KindTicket, "ZZZZZZZZZZZZZZZZ"},
		"malformed":     {gate.KindTicket, "not-a-code!"},
		"reservation":   {gate.KindReservationQR, e.ticketCode},
		"other lot":     {gate.KindTicket, otherCode},
		"entry_pending": {gate.KindTicket, pendingCode},
		"unknown card":  {gate.KindEmployeeCard, "04:a1:b2:c3"},
		"revoked card":  {gate.KindEmployeeCard, e.cardUID("EMP-031")},
	} {
		t.Run(name, func(t *testing.T) {
			if got := e.exitCred(cred[0], cred[1]); got.Result != "deny" || got.Reason != "gate.credential_invalid" || got.StayID != nil {
				t.Errorf("exit credential = %+v, want deny gate.credential_invalid", got)
			}
		})
	}
	if n := e.count(`select count(*) from stays where exit_authorized_at is not null`); n != 0 {
		t.Errorf("authorized stays = %d, want 0", n)
	}

	// Another lot's authorized stay cannot leave through this lot's barrier.
	if _, err := e.db.Super.Exec(t.Context(),
		`update stays set exit_authorized_at = $2 where public_id = $1`, *otherEntry.StayID, e.clk.Now()); err != nil {
		t.Fatal(err)
	}
	wantRejectedPass(t, e.exitPass(*otherEntry.StayID), "gate.stay_unknown")
	if r := e.exitRow(*otherEntry.StayID); r.Status != "active" || r.ExitedAt != nil {
		t.Errorf("other lot's stay = %+v, want active, never exited", r)
	}
	e.invariant()
	testsupport.AssertOccupancyInvariant(t, e.db.App, other)
}

// S9.1 carry: a second lost mark after the paid window closed reopens the
// stay for the overstay and still rotates the code.
func TestLostTicket_SecondMarkAfterWindowOwesOverstay(t *testing.T) {
	e := newExitEnv(t)
	e.at(time.Hour)
	first := e.lost(e.stayID)
	if got := e.cash(first.amount()); first.amount() != 50000 || got.Result != "ack" {
		t.Fatalf("lost %+v then cash %+v, want 50000 paid", first, got)
	}
	e.at(90 * time.Minute)
	second := e.lost(e.stayID)
	if second.Result != "ack" || second.TicketCode == "" || second.TicketCode == first.TicketCode || second.amount() != 5000 {
		t.Fatalf("second lost = %+v, want ack, a rotated code and 5000 due", second)
	}
	if s := e.payStay(); s.Status != "active" || !s.LostTicket {
		t.Errorf("stay = %+v, want active and lost", s)
	}
	wantRejected(t, e.scan(gate.KindTicket, first.TicketCode), "gate.credential_invalid")
	wantPaymentRequired(t, e.exitCred(gate.KindTicket, second.TicketCode), e.stayID, 5000)
	e.invariant()
}

// Two exit passes of one stay at once: the car leaves exactly once.
func TestExitPass_ConcurrentSameStay(t *testing.T) {
	e := newExitEnv(t)
	e.at(5 * time.Minute)
	wantOpen(t, e.exitCred(gate.KindTicket, e.ticketCode), e.stayID)
	occ, _ := e.occupancy()

	recs := make([]*httptest.ResponseRecorder, 2)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range recs {
		req := e.barrier.Request(t, deviceEventsPath, e.passBody(gate.DirectionExit, e.stayID), e.clk.Now())
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
	if occ2, _ := e.occupancy(); occ2 != occ-1 {
		t.Errorf("occupied = %d, want %d", occ2, occ-1)
	}
	if n := e.payments(); n != 1 {
		t.Errorf("payments = %d, want one zero payment", n)
	}
	e.invariant()
}

// An exit credential racing a cash payment of the same stay: the stay lock
// orders them, and the exit opens only if the cash landed first.
func TestExit_CredentialRacesCash(t *testing.T) {
	e := newExitEnv(t)
	e.at(time.Hour + time.Second)

	cashReq := e.ps.Request(t, deviceEventsPath, e.cashBody(uuid.New(), 7500), e.clk.Now())
	exitReq := e.barrier.Request(t, deviceEventsPath, e.exitCredBody(gate.KindTicket, e.ticketCode), e.clk.Now())
	cashRec, exitRec := httptest.NewRecorder(), httptest.NewRecorder()
	start := make(chan struct{})
	var wg sync.WaitGroup
	for _, rr := range []struct {
		req *httptest.ResponseRecorder
		r   *http.Request
	}{{cashRec, cashReq}, {exitRec, exitReq}} {
		rr.r.RemoteAddr = "203.0.113.7:5555"
		wg.Go(func() {
			<-start
			e.h.ServeHTTP(rr.req, rr.r)
		})
	}
	close(start)
	wg.Wait()

	if got := payOf(t, cashRec); got.Result != "ack" || got.amount() != 7500 {
		t.Errorf("cash = %+v, want ack 7500", got)
	}
	got := payOf(t, exitRec)
	r := e.exitRow(e.stayID)
	switch got.Result {
	case "open": // cash first
		wantOpen(t, got, e.stayID)
		if r.AuthedAt == nil {
			t.Errorf("stay = %+v, want authorized", r)
		}
	case "deny": // exit first
		wantPaymentRequired(t, got, e.stayID, 7500)
		if r.AuthedAt != nil {
			t.Errorf("stay = %+v, want not authorized", r)
		}
	default:
		t.Fatalf("exit credential = %+v, want open or deny", got)
	}
	if r.Status != "paid" {
		t.Errorf("stay = %+v, want paid", r)
	}
	if n := e.payments(); n != 1 {
		t.Errorf("payments = %d, want 1", n)
	}
	e.invariant()
}
