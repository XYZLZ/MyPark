package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log/slog"
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

// payData is the data of a pay-station answer (design §7.2).
type payData struct {
	Result      string     `json:"result"`
	Reason      string     `json:"reason"`
	StayID      *uuid.UUID `json:"stay_id"`
	Status      string     `json:"status"`
	TicketCode  string     `json:"ticket_code"`
	AmountCents *int64     `json:"amount_cents"`
	Breakdown   *struct {
		Kind       string     `json:"kind"`
		BilledFrom *time.Time `json:"billed_from"`
		PaidCents  int64      `json:"paid_cents"`
	} `json:"breakdown"`
	ReceiptNo    string     `json:"receipt_no"`
	PaidAt       *time.Time `json:"paid_at"`
	ExitDeadline *time.Time `json:"exit_deadline"`
}

func payOf(t *testing.T, rec *httptest.ResponseRecorder) payData {
	t.Helper()
	var d payData
	if err := json.Unmarshal([]byte(dataOf(t, rec)), &d); err != nil {
		t.Fatalf("pay data: %v", err)
	}
	return d
}

func (d payData) amount() int64 {
	if d.AmountCents == nil {
		return -1
	}
	return *d.AmountCents
}

func (d payData) kind() string {
	if d.Breakdown == nil {
		return ""
	}
	return d.Breakdown.Kind
}

// payEnv is a device env with a pay station and a ticket stay entered at t0.
type payEnv struct {
	*deviceEnv
	ps         testsupport.TestDevice
	t0         time.Time
	stayID     uuid.UUID
	ticketCode string
}

func newPayEnv(t *testing.T, mods ...func(*deps)) *payEnv {
	t.Helper()
	e := &payEnv{deviceEnv: newDeviceEnv(t, mods...)}
	e.ps = e.device(authz.DevicePayStation)
	e.t0 = e.clk.Now()
	got := entryOf(t, e.ticketRequest(e.device(authz.DeviceDispenser)))
	if got.Result != "open" || got.StayID == nil {
		t.Fatalf("entry = %+v, want open", got)
	}
	e.stayID, e.ticketCode = *got.StayID, got.TicketCode
	if p := e.pass(e.device(authz.DeviceEntryBarrier), e.stayID); p.Result != "ack" {
		t.Fatalf("entry pass = %+v", p)
	}
	return e
}

// at sets the clock to t0 + d.
func (e *payEnv) at(d time.Duration) time.Time {
	e.clk.Set(e.t0.Add(d))
	return e.clk.Now()
}

func (e *payEnv) scan(kind, value string) payData {
	e.t.Helper()
	return payOf(e.t, e.send(e.ps, e.eventBody(uuid.New(), "paystation.scan",
		map[string]any{"credential": map[string]any{"kind": kind, "value": value}})))
}

func (e *payEnv) cashBody(id uuid.UUID, amount int64) []byte {
	e.t.Helper()
	return e.eventBody(id, "paystation.cash_paid", map[string]any{"stay_id": e.stayID, "amount_cents": amount})
}

func (e *payEnv) cash(amount int64) payData {
	e.t.Helper()
	return payOf(e.t, e.send(e.ps, e.cashBody(uuid.New(), amount)))
}

func (e *payEnv) lost(id uuid.UUID) payData {
	e.t.Helper()
	return payOf(e.t, e.send(e.ps, e.eventBody(uuid.New(), "paystation.lost_ticket", map[string]any{"stay_id": id})))
}

type payStay struct {
	Status         string
	PaidAt, Exit   *time.Time
	LostTicket     bool
	TicketCodeHash []byte
}

func (e *payEnv) payStay() payStay {
	e.t.Helper()
	var s payStay
	if err := e.db.Super.QueryRow(e.t.Context(),
		`select status, paid_at, exit_deadline_at, lost_ticket, ticket_code_hash from stays where public_id = $1`, e.stayID).
		Scan(&s.Status, &s.PaidAt, &s.Exit, &s.LostTicket, &s.TicketCodeHash); err != nil {
		e.t.Fatalf("stay: %v", err)
	}
	return s
}

func (e *payEnv) payments() int64 { return e.count(`select count(*) from payments`) }

// payAt scans then pays the amount due at t0 + d, which must be want.
func (e *payEnv) payAt(d time.Duration, want int64) payData {
	e.t.Helper()
	e.at(d)
	if got := e.scan(gate.KindTicket, e.ticketCode); got.amount() != want {
		e.t.Fatalf("scan at %v = %+v, want %d due", d, got, want)
	}
	got := e.cash(want)
	if got.Result != "ack" {
		e.t.Fatalf("cash %d at %v = %+v, want ack", want, d, got)
	}
	return got
}

func wantRejected(t *testing.T, got payData, reason string) {
	t.Helper()
	if got.Result != "rejected" || got.Reason != reason {
		t.Fatalf("answer = %+v, want rejected %s", got, reason)
	}
}

func TestScan_PricesActiveTicket(t *testing.T) {
	e := newPayEnv(t)
	e.at(time.Hour + time.Second)
	got := e.scan(gate.KindTicket, e.ticketCode)
	if got.Result != "ack" || got.StayID == nil || *got.StayID != e.stayID || got.Status != "active" ||
		got.amount() != 7500 || got.kind() != "time" || got.Breakdown.BilledFrom != nil {
		t.Fatalf("scan = %+v, want ack active 7500 time", got)
	}
	if s := e.payStay(); s.Status != "active" || s.PaidAt != nil || s.LostTicket {
		t.Errorf("stay = %+v, want unchanged", s)
	}
	if n := e.payments(); n != 0 {
		t.Errorf("payments = %d, want 0", n)
	}
	e.invariant()
}

// Review Focus 2: the code typed in another form finds the same stay.
func TestScan_NormalizedTicketFindsStay(t *testing.T) {
	e := newPayEnv(t)
	const code = "01ABCDEF0123GH1J" // has 0 and 1, so the O and I/L aliases apply
	sum := sha256.Sum256([]byte(code))
	if _, err := e.db.Super.Exec(t.Context(), `update stays set ticket_code_hash = $2 where public_id = $1`, e.stayID, sum[:]); err != nil {
		t.Fatal(err)
	}
	e.at(30 * time.Minute)
	for _, typed := range []string{code, "01ab-cdef-0123-gh1j", "OLAB CDEF OI23 GHIJ", "oiab-cdef-o123-gh1j"} {
		got := e.scan(gate.KindTicket, typed)
		if got.Result != "ack" || got.StayID == nil || *got.StayID != e.stayID || got.amount() != 5000 {
			t.Errorf("scan %q = %+v, want ack for the stay", typed, got)
		}
	}
	e.invariant()
}

func TestScan_UnknownOrReservationRejected(t *testing.T) {
	e := newPayEnv(t)
	other := testsupport.SeedSecondLot(t, e.db)
	otherDispenser := testsupport.NewTestDevice(t, e.db, e.d.sealer, other, authz.DeviceDispenser)
	otherCode := entryOf(t, e.ticketRequest(otherDispenser)).TicketCode
	if otherCode == "" {
		t.Fatal("second lot issued no ticket")
	}
	e.at(time.Hour)
	for name, cred := range map[string][2]string{
		"unknown":      {gate.KindTicket, "ZZZZZZZZZZZZZZZZ"},
		"malformed":    {gate.KindTicket, "not-a-code!"},
		"reservation":  {gate.KindReservationQR, e.ticketCode},
		"other lot":    {gate.KindTicket, otherCode},
		"unknown card": {gate.KindEmployeeCard, "04:a1:b2:c3"},
	} {
		t.Run(name, func(t *testing.T) {
			wantRejected(t, e.scan(cred[0], cred[1]), "gate.credential_invalid")
		})
	}
	e.invariant()
}

func TestCash_PaysAndSetsWindow(t *testing.T) {
	var logs bytes.Buffer
	e := newPayEnv(t, func(d *deps) {
		d.log = slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	})
	now := e.at(time.Hour + time.Second)
	if got := e.scan(gate.KindTicket, e.ticketCode); got.amount() != 7500 {
		t.Fatalf("scan = %+v, want 7500", got)
	}
	occ, ver := e.occupancy()
	eventID := uuid.New()
	got := payOf(t, e.send(e.ps, e.cashBody(eventID, 7500)))

	var payID int64
	var method, status, key string
	var amount int64
	var succeededAt time.Time
	var deviceID int64
	if err := e.db.Super.QueryRow(t.Context(),
		`select id, method, status, idempotency_key, amount_cents, succeeded_at, device_id from payments`).
		Scan(&payID, &method, &status, &key, &amount, &succeededAt, &deviceID); err != nil {
		t.Fatalf("payment: %v", err)
	}
	if got.Result != "ack" || got.ReceiptNo != fmt.Sprintf("MP-%08d", payID) || got.amount() != 7500 ||
		got.PaidAt == nil || !got.PaidAt.Equal(now) || got.ExitDeadline == nil || !got.ExitDeadline.Equal(now.Add(15*time.Minute)) {
		t.Fatalf("cash = %+v, want ack MP-%08d 7500 paid %v deadline +15m", got, payID, now)
	}
	if wantKey := "cash:" + e.ps.PublicID.String() + ":" + eventID.String(); method != "cash" || status != "succeeded" ||
		key != wantKey || amount != 7500 || !succeededAt.Equal(now) || deviceID != e.ps.ID {
		t.Errorf("payment = %s %s %s %d %v dev %d, want cash succeeded %s 7500 at %v", method, status, key, amount, succeededAt, deviceID, wantKey, now)
	}
	if s := e.payStay(); s.Status != "paid" || s.PaidAt == nil || !s.PaidAt.Equal(now) || s.Exit == nil || !s.Exit.Equal(now.Add(15*time.Minute)) {
		t.Errorf("stay = %+v, want paid at %v with a 15 m window", s, now)
	}
	if occ2, ver2 := e.occupancy(); occ2 != occ || ver2 != ver {
		t.Errorf("occupancy = %d v%d, want %d v%d unchanged", occ2, ver2, occ, ver)
	}
	if again := payOf(t, e.send(e.ps, e.cashBody(eventID, 7500))); again.ReceiptNo != got.ReceiptNo || e.payments() != 1 {
		t.Errorf("replay = %+v with %d payments, want the same receipt and 1 payment", again, e.payments())
	}
	if strings.Contains(logs.String(), e.ticketCode) {
		t.Error("the plaintext ticket code reached the logs")
	}
	e.invariant()
}

func TestCash_AmountMismatch(t *testing.T) {
	e := newPayEnv(t)
	e.at(time.Hour + time.Second)
	for _, paid := range []int64{5000, 7499, 7501, 10000} {
		got := e.cash(paid)
		wantRejected(t, got, "gate.amount_mismatch")
		if got.amount() != 7500 || got.ReceiptNo != "" {
			t.Errorf("cash %d = %+v, want the due 7500 and no receipt", paid, got)
		}
	}
	if n := e.payments(); n != 0 {
		t.Errorf("payments = %d, want 0", n)
	}
	if s := e.payStay(); s.Status != "active" || s.PaidAt != nil {
		t.Errorf("stay = %+v, want active and unpaid", s)
	}
	e.invariant()
}

func TestCash_NothingDue(t *testing.T) {
	e := newPayEnv(t)
	e.at(5 * time.Minute)
	if got := e.scan(gate.KindTicket, e.ticketCode); got.amount() != 0 || got.kind() != "grace" {
		t.Fatalf("scan in grace = %+v, want 0 grace", got)
	}
	got := e.cash(100)
	wantRejected(t, got, "gate.nothing_due")
	if got.AmountCents != nil {
		t.Errorf("nothing_due carries amount %d", *got.AmountCents)
	}

	e.payAt(time.Hour+time.Second, 7500)
	e.at(time.Hour + 10*time.Minute)
	if got := e.scan(gate.KindTicket, e.ticketCode); got.Status != "paid" || got.amount() != 0 || got.kind() != "paid" {
		t.Errorf("scan in the window = %+v, want paid 0", got)
	}
	wantRejected(t, e.cash(2500), "gate.nothing_due")
	if n := e.payments(); n != 1 {
		t.Errorf("payments = %d, want 1", n)
	}
	e.invariant()
}

// O1 then O8's cash: the overstay is billed from the latest payment, the lazy
// paid -> active transition keeps the deadline, and paying again resets it.
func TestOverstay_LazyReactivationAndSecondPayment(t *testing.T) {
	e := newPayEnv(t)
	first := e.payAt(90*time.Minute, 7500)

	e.at(110 * time.Minute)
	got := e.scan(gate.KindTicket, e.ticketCode)
	if got.Status != "active" || got.amount() != 5000 || got.kind() != "overstay" ||
		got.Breakdown.BilledFrom == nil || !got.Breakdown.BilledFrom.Equal(e.t0.Add(90*time.Minute)) || got.Breakdown.PaidCents != 7500 {
		t.Fatalf("overstay scan = %+v, want active 5000 overstay billed from t0+1h30", got)
	}
	if s := e.payStay(); s.Status != "active" || s.Exit == nil || !s.Exit.Equal(*first.ExitDeadline) {
		t.Errorf("stay = %+v, want active with deadline %v kept", s, *first.ExitDeadline)
	}

	second := e.cash(5000)
	if second.Result != "ack" || second.ExitDeadline == nil || !second.ExitDeadline.Equal(e.t0.Add(125*time.Minute)) {
		t.Fatalf("second cash = %+v, want ack with deadline t0+2h05", second)
	}
	if s := e.payStay(); s.Status != "paid" || !s.PaidAt.Equal(e.t0.Add(110*time.Minute)) {
		t.Errorf("stay = %+v, want paid at t0+1h50", s)
	}
	if n := e.payments(); n != 2 {
		t.Errorf("payments = %d, want 2", n)
	}
	e.invariant()
}

func TestLostTicket_ReplacesCodeAndPrices(t *testing.T) {
	var logs bytes.Buffer
	e := newPayEnv(t, func(d *deps) {
		d.log = slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	})
	e.at(3 * time.Hour)
	got := e.lost(e.stayID)
	if got.Result != "ack" || got.StayID == nil || *got.StayID != e.stayID || len(got.TicketCode) != 16 ||
		got.TicketCode == e.ticketCode || got.amount() != 50000 || got.kind() != "lost_ticket" {
		t.Fatalf("lost = %+v, want ack with a new code, 50000 lost_ticket", got)
	}
	s := e.payStay()
	if want := sha256.Sum256([]byte(got.TicketCode)); s.Status != "active" || !s.LostTicket || !bytes.Equal(s.TicketCodeHash, want[:]) {
		t.Errorf("stay = %+v, want active, lost, hashed under the new code", s)
	}
	wantRejected(t, e.scan(gate.KindTicket, e.ticketCode), "gate.credential_invalid")
	if again := e.scan(gate.KindTicket, got.TicketCode); again.amount() != 50000 || again.kind() != "lost_ticket" {
		t.Errorf("scan of the new code = %+v, want 50000 lost_ticket", again)
	}

	var stored string
	if err := e.db.Super.QueryRow(t.Context(),
		`select response::text from device_events where type = 'paystation.lost_ticket'`).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(stored, got.TicketCode) || !strings.Contains(stored, "sealed") {
		t.Errorf("stored response %s: want the new code sealed", stored)
	}
	if strings.Contains(logs.String(), got.TicketCode) {
		t.Error("the new ticket code reached the logs")
	}
	e.invariant()
}

// L7: a ticket lost inside the payment window owes the lost fee minus what
// was paid, and the stay owes money again.
func TestLostTicket_OnPaidStay_L7(t *testing.T) {
	e := newPayEnv(t)
	e.payAt(90*time.Minute, 7500)
	e.at(100 * time.Minute)
	got := e.lost(e.stayID)
	if got.Result != "ack" || got.amount() != 42500 || got.kind() != "lost_ticket" {
		t.Fatalf("lost = %+v, want 42500 lost_ticket", got)
	}
	if s := e.payStay(); s.Status != "active" || s.Exit == nil {
		t.Errorf("stay = %+v, want active with its deadline kept", s)
	}
	if again := e.cash(42500); again.Result != "ack" {
		t.Errorf("cash 42500 = %+v, want ack", again)
	}
	e.invariant()
}

// O13: marked lost after the window closed, the uncovered penalty wins.
func TestLostTicket_AfterDeadline_O13(t *testing.T) {
	e := newPayEnv(t)
	e.payAt(90*time.Minute, 7500)
	e.at(2 * time.Hour)
	got := e.lost(e.stayID)
	if got.Result != "ack" || got.amount() != 42500 || got.kind() != "lost_ticket" {
		t.Fatalf("lost = %+v, want 42500 lost_ticket", got)
	}
	if again := e.scan(gate.KindTicket, got.TicketCode); again.amount() != 42500 || again.Status != "active" {
		t.Errorf("scan = %+v, want active 42500", again)
	}
	e.invariant()
}

func TestLostTicket_EmployeeStayRejected(t *testing.T) {
	e := newPayEnv(t)
	barrier := e.device(authz.DeviceEntryBarrier)
	card := e.openStay(barrier, e.cardUID("EMP-007"))
	if p := e.pass(barrier, card); p.Result != "ack" {
		t.Fatalf("card pass = %+v", p)
	}
	e.at(time.Hour)
	for name, id := range map[string]uuid.UUID{"card stay": card, "random": uuid.New()} {
		t.Run(name, func(t *testing.T) { wantRejected(t, e.lost(id), "gate.stay_unknown") })
	}
	if n := e.count(`select count(*) from stays where lost_ticket`); n != 0 {
		t.Errorf("lost stays = %d, want 0", n)
	}
	e.invariant()
}

// Two cash payments for one stay at once, as two events or as the same event
// sent twice: the stay lock serializes them and the stay is charged once.
func TestCash_ConcurrentDoublePaymentChargesOnce(t *testing.T) {
	for _, sameEvent := range []bool{false, true} {
		t.Run(fmt.Sprintf("same event %v", sameEvent), func(t *testing.T) {
			e := newPayEnv(t)
			e.at(time.Hour + time.Second)
			ids := []uuid.UUID{uuid.New(), uuid.New()}
			if sameEvent {
				ids[1] = ids[0]
			}
			recs := make([]*httptest.ResponseRecorder, 2)
			start := make(chan struct{})
			var wg sync.WaitGroup
			for i := range recs {
				req := e.ps.Request(t, deviceEventsPath, e.cashBody(ids[i], 7500), e.clk.Now())
				req.RemoteAddr = "203.0.113.7:5555"
				wg.Go(func() {
					<-start
					recs[i] = httptest.NewRecorder()
					e.h.ServeHTTP(recs[i], req)
				})
			}
			close(start)
			wg.Wait()

			answers := map[string]int{}
			receipts := map[string]bool{}
			for i, rec := range recs {
				got := payOf(t, rec)
				answers[got.Result+" "+got.Reason]++
				if got.ReceiptNo != "" {
					receipts[got.ReceiptNo] = true
				}
				t.Logf("request %d: %+v", i, got)
			}
			want := map[string]int{"ack ": 1, "rejected gate.nothing_due": 1}
			if sameEvent {
				want = map[string]int{"ack ": 2} // the loser replays the winner's answer
			}
			if fmt.Sprint(answers) != fmt.Sprint(want) || len(receipts) != 1 {
				t.Errorf("answers = %v receipts %v, want %v and one receipt", answers, receipts, want)
			}
			if n := e.payments(); n != 1 {
				t.Errorf("payments = %d, want 1", n)
			}
			if n := e.count(`select coalesce(sum(amount_cents), 0) from payments`); n != 7500 {
				t.Errorf("charged %d, want 7500", n)
			}
			e.invariant()
		})
	}
}

// A card resolves to its open stay: free once inside, unknown while pending.
func TestScan_EmployeeCard(t *testing.T) {
	e := newPayEnv(t)
	barrier := e.device(authz.DeviceEntryBarrier)
	uid := e.cardUID("EMP-007")
	card := e.openStay(barrier, uid)
	wantRejected(t, e.scan(gate.KindEmployeeCard, uid), "gate.credential_invalid")
	if p := e.pass(barrier, card); p.Result != "ack" {
		t.Fatalf("card pass = %+v", p)
	}
	e.at(30 * time.Hour)
	got := e.scan(gate.KindEmployeeCard, strings.ToUpper(strings.ReplaceAll(uid, ":", "")))
	if got.Result != "ack" || got.StayID == nil || *got.StayID != card || got.amount() != 0 || got.kind() != "employee" {
		t.Fatalf("card scan = %+v, want ack 0 employee for the card stay", got)
	}
	wantRejected(t, payOf(t, e.send(e.ps, e.eventBody(uuid.New(), "paystation.cash_paid",
		map[string]any{"stay_id": card, "amount_cents": 100}))), "gate.nothing_due")
	e.invariant()
}
