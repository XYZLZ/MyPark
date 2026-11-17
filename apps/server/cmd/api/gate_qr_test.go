package main

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/XYZLZ/MyPark/apps/server/internal/domain/authz"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/devices"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/gate"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/reservations"
	"github.com/XYZLZ/MyPark/apps/server/internal/testsupport"
)

// qrEnv is a feeEnv (clock at rsvNow, simulated gateway) that also speaks
// the device API of the same lot.
type qrEnv struct {
	*feeEnv
	dev     *deviceEnv
	barrier testsupport.TestDevice
}

func newQREnv(t *testing.T) *qrEnv {
	t.Helper()
	e := newFeeEnv(t)
	dev := &deviceEnv{apiEnv: e.apiEnv, lotID: e.lotID}
	return &qrEnv{feeEnv: e, dev: dev, barrier: dev.device(authz.DeviceEntryBarrier)}
}

// confirmed reserves and pays for a fresh driver and returns the
// reservation, the driver's token and the QR token.
func (e *qrEnv) confirmed(arrival string) (id, tok, qr string) {
	e.t.Helper()
	id, tok = e.paid(arrival)
	rec := e.serve(jsonReq(http.MethodGet, "/v1/reservations/"+id+"/qr", nil), tok)
	qr, _ = field(decodeBody(e.t, rec), "data.token").(string)
	if rec.Code != http.StatusOK || qr == "" {
		e.t.Fatalf("qr = %d %s", rec.Code, rec.Body)
	}
	return id, tok, qr
}

func (e *qrEnv) qrBody(token string) []byte {
	return e.dev.eventBody(uuid.New(), "entry.credential_presented", map[string]any{"kind": gate.KindReservationQR, "value": token})
}

func (e *qrEnv) present(token string) entryData {
	e.t.Helper()
	return entryOf(e.t, e.dev.send(e.barrier, e.qrBody(token)))
}

func (e *qrEnv) version() int {
	return e.count(`select version from lot_occupancy where lot_id = $1`, e.lotID)
}

func (e *qrEnv) invariants() {
	e.t.Helper()
	testsupport.AssertOccupancyInvariant(e.t, e.db.App, e.lotID)
	testsupport.AssertSlotInvariant(e.t, e.db.App, e.lotID)
}

func wantQROpen(t *testing.T, got entryData) uuid.UUID {
	t.Helper()
	if got.Result != "open" || got.StayID == nil || got.TicketCode != "" {
		t.Fatalf("answer = %+v, want open with a stay", got)
	}
	return *got.StayID
}

func wantQRDeny(t *testing.T, got entryData, reason string) {
	t.Helper()
	if got.Result != "deny" || got.Reason != reason || got.StayID != nil {
		t.Fatalf("answer = %+v, want deny %s", got, reason)
	}
}

// The guarantee (spec 5.2 rule 2): one space left and held for the
// reservation, so drive-up is refused while the QR gets in.
func TestGateQR_Guarantee(t *testing.T) {
	e := newQREnv(t)
	id, _, qr := e.confirmed(arrivalInWindow)
	testsupport.FillLot(t, e.db, e.lotID, 279, e.clk.Now())
	wantDeny(t, e.dev.ticketRequest(e.dev.device(authz.DeviceDispenser)), "gate.lot_full")
	if got := e.data("/v1/lots/" + e.publicID + "/availability")["available"]; got != 0.0 {
		t.Errorf("available = %v, want 0", got)
	}
	before := e.version()

	stayID := wantQROpen(t, e.present(qr))
	if n := e.count(`select count(*) from reservations where public_id = $1 and status = 'checked_in' and checked_in_at = $2`, id, e.clk.Now()); n != 1 {
		t.Error("want the reservation checked_in at now")
	}
	if n := e.count(`select count(*) from stays s join reservations r on r.id = s.reservation_id
	                 where s.public_id = $1 and r.public_id = $2 and s.credential_kind = 'reservation' and s.status = 'entry_pending'
	                 and s.accepted_at = $3 and s.tariff_version_id = r.tariff_version_id`, stayID, id, e.clk.Now()); n != 1 {
		t.Error("want an entry_pending reservation stay with the reservation's tariff, accepted at now")
	}
	if h := e.held(); h != 0 {
		t.Errorf("slots holding = %d, want 0 (released)", h)
	}
	if v := e.version(); v != before+1 {
		t.Errorf("version = %d, want %d", v, before+1)
	}
	e.invariants()
}

func TestGateQR_PhysicallyFull(t *testing.T) {
	e := newQREnv(t)
	id, _, qr := e.confirmed(arrivalInWindow)
	testsupport.FillLot(t, e.db, e.lotID, 280, e.clk.Now())
	before := e.version()
	body := e.qrBody(qr)

	wantQRDeny(t, entryOf(t, e.dev.send(e.barrier, body)), "gate.lot_physically_full")
	if n := e.count(`select count(*) from reservations where public_id = $1 and status = 'cancelled' and cancelled_by = 'system'
	                 and refund_reason = 'facility_unavailable' and cancelled_at = $2`, id, e.clk.Now()); n != 1 {
		t.Error("want the reservation cancelled by system, facility_unavailable, at now")
	}
	if h := e.held(); h != 0 {
		t.Errorf("slots holding = %d, want 0", h)
	}
	if n := e.count(`select count(*) from refunds where status = 'pending' and reason = 'facility_unavailable'
	                 and amount_cents = 15000 and next_attempt_at = $1`, e.clk.Now()); n != 1 {
		t.Error("want one pending full refund, facility_unavailable, due now")
	}
	if n := e.count(`select count(*) from reconciliation_alerts a join refunds f on a.details->>'refund_id' = f.public_id::text
	                 where a.kind = 'reservation_unadmitted' and a.details->>'reservation_id' = $1`, id); n != 1 {
		t.Error("want one reservation_unadmitted alert naming the reservation and the refund")
	}
	if n := e.count(`select count(*) from outbox_messages where dedupe_key = $1 and payload->>'refund_status' = 'pending'
	                 and payload->>'refund_reason' = 'facility_unavailable'`, "reservation_cancelled:"+id); n != 1 {
		t.Error("want the reservation_cancelled email with a pending refund")
	}
	if v := e.version(); v != before+1 {
		t.Errorf("version = %d, want %d", v, before+1)
	}

	rec := e.dev.send(e.barrier, body)
	wantQRDeny(t, entryOf(t, rec), "gate.lot_physically_full")
	if rec.Header().Get(devices.HeaderReplayed) != "true" {
		t.Error("the second send was not answered as a replay")
	}
	if n := e.count(`select (select count(*) from refunds) + (select count(*) from reconciliation_alerts)
	                 + (select count(*) from outbox_messages where template = 'reservation_cancelled')`); n != 3 || e.version() != before+1 {
		t.Errorf("after the replay: rows %d (want 3), version %d (want %d)", n, e.version(), before+1)
	}

	if n, err := e.processor(e.simGateway(&refundGateway{})).Run(t.Context(), e.clk.Now()); err != nil || n != 1 {
		t.Fatalf("refund Run = %d, %v; want 1", n, err)
	}
	if n := e.count(`select count(*) from refunds where status = 'succeeded'`); n != 1 {
		t.Error("want the refund succeeded")
	}
	e.invariants()
}

// Pending entries count toward the physically-full predicate (M3B-23).
func TestGateQR_PendingEntriesCountForFull(t *testing.T) {
	e := newQREnv(t)
	_, _, qr := e.confirmed(arrivalInWindow)
	testsupport.FillLot(t, e.db, e.lotID, 279, e.clk.Now())
	if _, err := e.db.App.Exec(t.Context(),
		`insert into stays (lot_id, credential_kind, ticket_code_hash, status, tariff_version_id, accepted_at, updated_at)
		 select $1, 'ticket', sha256('pending'::bytea), 'entry_pending', id, $2, $2 from tariff_versions where lot_id = $1`,
		e.lotID, e.clk.Now().Add(-10*time.Second)); err != nil {
		t.Fatal(err)
	}
	wantQRDeny(t, e.present(qr), "gate.lot_physically_full")
	e.invariants()
}

func TestGateQR_TooEarlyAndWindowClosed(t *testing.T) {
	e := newQREnv(t)
	id, _, qr := e.confirmed(arrival1502)
	wantQRDeny(t, e.present(qr), "reservation.too_early")

	var upper time.Time
	if err := e.db.App.QueryRow(t.Context(), `select upper(hold_window) from reservations where public_id = $1`, id).Scan(&upper); err != nil {
		t.Fatal(err)
	}
	e.clk.Set(upper)
	wantQRDeny(t, e.present(qr), "reservation.window_closed")

	if _, err := e.db.App.Exec(t.Context(),
		`with r as (update reservations set status = 'expired_no_show', expired_at = $2, updated_at = $2
		            where public_id = $1 returning lot_id, hold_window)
		 update reservation_slot_loads s set held = held - 1 from r
		 where s.lot_id = r.lot_id and r.hold_window @> s.slot_start`, id, upper); err != nil {
		t.Fatal(err)
	}
	wantQRDeny(t, e.present(qr), "reservation.window_closed")
	if n := e.count(`select count(*) from stays`); n != 0 {
		t.Errorf("stays = %d, want 0", n)
	}
	e.invariants()
}

// Tokens refused before any lock answer while a side transaction holds the
// lot row; a cancelled reservation keeps its nonce, so it is refused after
// the lock (ruling C17).
func TestGateQR_InvalidTokens(t *testing.T) {
	e := newQREnv(t)
	other := testsupport.SeedSecondLot(t, e.db)
	otherBarrier := testsupport.NewTestDevice(t, e.db, e.d.sealer, other, authz.DeviceEntryBarrier)

	_, _, good := e.confirmed(arrivalInWindow)
	// A well-formed token whose MAC is wrong: only the HMAC check refuses it.
	raw, err := base64.RawURLEncoding.DecodeString(good)
	if err != nil {
		t.Fatal(err)
	}
	raw[63] ^= 1
	forged := base64.RawURLEncoding.EncodeToString(raw)
	rotatedID, _, rotated := e.confirmed(arrivalInWindow)
	if _, err := e.db.App.Exec(t.Context(),
		`update reservations set qr_nonce = substring(sha256(qr_nonce) from 1 for 16) where public_id = $1`, rotatedID); err != nil {
		t.Fatal(err)
	}
	d := testsupport.SeedDrivers(t, e.db, 1)[0]
	unpaid := reservations.EncodeQR(e.cfg.QRHMACKey(), uuid.MustParse(e.reserve(d, arrivalInWindow)), [16]byte{1})

	side, err := e.db.Super.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = side.Rollback(context.WithoutCancel(t.Context())) }()
	if _, err := side.Exec(t.Context(), `select 1 from lot_occupancy where lot_id in ($1, $2) for update`, e.lotID, other); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		device testsupport.TestDevice
		token  string
	}{
		{"forged mac", e.barrier, forged},
		{"other lot", otherBarrier, good},
		{"nonce mismatch", e.barrier, rotated},
		{"pending_payment", e.barrier, unpaid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			req := tc.device.Request(t, deviceEventsPath, e.qrBody(tc.token), e.clk.Now()).WithContext(ctx)
			wantQRDeny(t, entryOf(t, e.serve(req, "")), "gate.credential_invalid")
		})
	}
	if err := side.Rollback(t.Context()); err != nil {
		t.Fatal(err)
	}

	id, tok, qr := e.confirmed(arrivalInWindow)
	if code, body := e.cancel(tok, id); code != http.StatusOK {
		t.Fatalf("cancel = %d %v", code, body)
	}
	wantQRDeny(t, e.present(qr), "gate.credential_invalid")
	if n := e.count(`select count(*) from stays`); n != 0 {
		t.Errorf("stays = %d, want 0", n)
	}
	e.invariants()
}

// Review Focus 5: a token pasted with whitespace is invalid, never a 500,
// and is stored only as a hash.
func TestGateQR_WhitespaceTokenInvalid(t *testing.T) {
	e := newQREnv(t)
	_, _, qr := e.confirmed(arrivalInWindow)
	for _, token := range []string{qr + "\n", " " + qr, qr + " ", "\t" + qr + "\r\n"} {
		wantQRDeny(t, e.present(token), "gate.credential_invalid")
	}
	var payloads string
	if err := e.db.Super.QueryRow(t.Context(), `select string_agg(payload::text, ' ') from device_events`).Scan(&payloads); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(payloads, qr) || strings.Count(payloads, `"sha256:`) != 4 {
		t.Errorf("device_events payloads %s: want four sha256:<12 hex> values and never the token", payloads)
	}
	if strings.Contains(e.logs.String(), qr) {
		t.Error("the QR token reached the logs")
	}
	if n := e.count(`select count(*) from stays`); n != 0 {
		t.Errorf("stays = %d, want 0", n)
	}
	e.invariants()
}

// M3B-24: re-presenting while the stay waits for the pass re-opens it.
func TestGateQR_RePresentWhilePending(t *testing.T) {
	e := newQREnv(t)
	id, _, qr := e.confirmed(arrivalInWindow)
	first := wantQROpen(t, e.present(qr))
	e.clk.Advance(20 * time.Second)
	if again := wantQROpen(t, e.present(qr)); again != first {
		t.Errorf("stay_id = %s, want the same %s", again, first)
	}
	if n := e.count(`select count(*) from stays s join reservations r on r.id = s.reservation_id
	                 where r.public_id = $1 and s.status = 'entry_pending' and s.accepted_at = $2`, id, e.clk.Now()); n != 1 {
		t.Error("want one entry_pending stay with accepted_at refreshed")
	}
	if n := e.count(`select count(*) from stays`); n != 1 {
		t.Errorf("stays = %d, want 1", n)
	}
	e.invariants()
}

func TestGateQR_CarInsideInUse(t *testing.T) {
	e := newQREnv(t)
	_, _, qr := e.confirmed(arrivalInWindow)
	stayID := wantQROpen(t, e.present(qr))
	if got := e.dev.pass(e.barrier, stayID); got != (passData{Result: "ack"}) {
		t.Fatalf("entry pass = %+v", got)
	}
	wantQRDeny(t, e.present(qr), "gate.credential_in_use")
	e.invariants()
}

func TestGateQR_LotClosed(t *testing.T) {
	e := newQREnv(t)
	id, _, qr := e.confirmed(arrivalInWindow)
	if _, err := e.db.Super.Exec(t.Context(), `update lots set status = 'closed' where id = $1`, e.lotID); err != nil {
		t.Fatal(err)
	}
	wantQRDeny(t, e.present(qr), "gate.lot_closed")
	if st := e.status(id); st != "confirmed" {
		t.Errorf("status = %s, want confirmed", st)
	}
	e.invariants()
}

// Review Focus 4: the driver cancels while the barrier reads the QR, inside
// the window and before arrival. Exactly one wins, every round.
func TestGateQR_CancelRacesCheckIn(t *testing.T) {
	e := newQREnv(t)
	wins := map[string]int{}
	for round := range 20 {
		id, tok, qr := e.confirmed(arrivalInWindow)
		cancelReq := jsonReq(http.MethodPost, "/v1/reservations/"+id+"/cancel", nil)
		cancelReq.Header.Set("Authorization", "Bearer "+tok)
		cancelReq.RemoteAddr = "203.0.113.7:5555"
		qrReq := e.barrier.Request(t, deviceEventsPath, e.qrBody(qr), e.clk.Now())
		qrReq.RemoteAddr = "203.0.113.7:5555"
		ctx, stop := context.WithTimeout(t.Context(), 10*time.Second)
		cancelRec, qrRec := httptest.NewRecorder(), httptest.NewRecorder()
		start := make(chan struct{})
		var wg sync.WaitGroup
		wg.Go(func() { <-start; e.h.ServeHTTP(cancelRec, cancelReq.WithContext(ctx)) })
		wg.Go(func() { <-start; e.h.ServeHTTP(qrRec, qrReq.WithContext(ctx)) })
		close(start)
		wg.Wait()
		if ctx.Err() != nil {
			t.Fatalf("round %d: requests did not finish within 10 s (deadlock?)", round)
		}
		stop()

		got := entryOf(t, qrRec)
		cancelBody := decodeBody(t, cancelRec)
		st := e.status(id)
		wins[st]++
		switch {
		case st == "cancelled" && cancelRec.Code == http.StatusOK && got.Result == "deny" && got.Reason == "gate.credential_invalid":
			if n := e.count(`select count(*) from refunds f join payments p on p.id = f.payment_id join reservations r on r.id = p.reservation_id
			                 where r.public_id = $1 and f.status = 'pending'`, id); n != 1 {
				t.Errorf("round %d: cancel won without a pending refund", round)
			}
		case st == "checked_in" && got.Result == "open" && cancelRec.Code == http.StatusConflict && errCode(cancelBody) == "reservation.invalid_state":
		default:
			t.Errorf("round %d: status %s, cancel %d %v, qr %+v", round, st, cancelRec.Code, cancelBody, got)
		}
	}
	t.Logf("winners: %v", wins)
	e.invariants()
}

// voidCheckIn stands in for the S6.2 void (M3B-60): the stay is voided and
// the reservation is confirmed again with its hold restored.
func (e *qrEnv) voidCheckIn(id string) {
	e.t.Helper()
	if _, err := e.db.App.Exec(e.t.Context(),
		`with r as (update reservations set status = 'confirmed', checked_in_at = null, updated_at = $2
		            where public_id = $1 returning id, lot_id, hold_window),
		      s as (update stays set status = 'voided', voided_at = $2, updated_at = $2
		            where reservation_id = (select id from r))
		 update reservation_slot_loads l set held = held + 1 from r
		 where l.lot_id = r.lot_id and r.hold_window @> l.slot_start`, id, e.clk.Now()); err != nil {
		e.t.Fatal(err)
	}
}

// M3B-24/M3B-60: a confirmed reservation whose stay was voided reuses it.
func TestGateQR_ReopenVoidedStay(t *testing.T) {
	e := newQREnv(t)
	id, _, qr := e.confirmed(arrivalInWindow)
	first := wantQROpen(t, e.present(qr))
	e.clk.Advance(61 * time.Second)
	voidedAt := e.clk.Now()
	e.voidCheckIn(id)
	e.clk.Advance(time.Minute)
	before := e.version()

	if again := wantQROpen(t, e.present(qr)); again != first {
		t.Errorf("stay_id = %s, want the voided %s", again, first)
	}
	if n := e.count(`select count(*) from stays where public_id = $1 and status = 'entry_pending'
	                 and accepted_at = $2 and voided_at = $3`, first, e.clk.Now(), voidedAt); n != 1 {
		t.Error("want the stay entry_pending at now with voided_at kept")
	}
	if st := e.status(id); st != "checked_in" {
		t.Errorf("status = %s, want checked_in", st)
	}
	if h := e.held(); h != 0 {
		t.Errorf("slots holding = %d, want 0", h)
	}
	if v := e.version(); v != before+1 {
		t.Errorf("version = %d, want %d", v, before+1)
	}
	if n := e.count(`select count(*) from stays`); n != 1 {
		t.Errorf("stays = %d, want 1", n)
	}
	e.invariants()
}

// Re-presented after the void into a physically full lot: the reservation
// is cancelled with a refund and its voided stay is left alone.
func TestGateQR_ReopenVoidedStayPhysicallyFull(t *testing.T) {
	e := newQREnv(t)
	id, _, qr := e.confirmed(arrivalInWindow)
	stayID := wantQROpen(t, e.present(qr))
	e.clk.Advance(61 * time.Second)
	e.voidCheckIn(id)
	testsupport.FillLot(t, e.db, e.lotID, 280, e.clk.Now())

	wantQRDeny(t, e.present(qr), "gate.lot_physically_full")
	if st := e.status(id); st != "cancelled" {
		t.Errorf("status = %s, want cancelled", st)
	}
	if n := e.count(`select count(*) from stays where public_id = $1 and status = 'voided'`, stayID); n != 1 {
		t.Error("want the stay still voided")
	}
	if n := e.count(`select count(*) from refunds where status = 'pending' and reason = 'facility_unavailable'`); n != 1 {
		t.Error("want one pending refund")
	}
	e.invariants()
}

// qrStay is a reservation stay inside the lot, with the devices that serve it.
type qrStay struct {
	e      *qrEnv
	d      testsupport.Driver
	id     string
	qr     string
	stay   uuid.UUID
	ps, ex testsupport.TestDevice
}

// inside reserves, pays and checks a fresh driver in by the QR, and lets the
// car pass the entry barrier at the clock's now.
func (e *qrEnv) inside() *qrStay {
	e.t.Helper()
	s := e.booked()
	s.stay = wantQROpen(e.t, e.present(s.qr))
	if p := e.dev.pass(e.barrier, s.stay); p.Result != "ack" {
		e.t.Fatalf("entry pass = %+v", p)
	}
	return s
}

// booked reserves and pays for a fresh driver and reads the QR token.
func (e *qrEnv) booked() *qrStay {
	e.t.Helper()
	d := testsupport.SeedDrivers(e.t, e.db, 1)[0]
	id, tok := e.reserve(d, arrivalInWindow), e.token(d)
	if code, body, _ := e.pay(tok, id, "key-qr-pay-0001", goodCard()); code != http.StatusOK {
		e.t.Fatalf("pay = %d %v", code, body)
	}
	rec := e.serve(jsonReq(http.MethodGet, "/v1/reservations/"+id+"/qr", nil), tok)
	qr, _ := field(decodeBody(e.t, rec), "data.token").(string)
	if qr == "" {
		e.t.Fatalf("qr = %d %s", rec.Code, rec.Body)
	}
	return &qrStay{e: e, d: d, id: id, qr: qr,
		ps: e.dev.device(authz.DevicePayStation), ex: e.dev.device(authz.DeviceExitBarrier)}
}

func qrCredential(token string) map[string]any {
	return map[string]any{"credential": map[string]any{"kind": gate.KindReservationQR, "value": token}}
}

func (s *qrStay) scan(token string) payData {
	s.e.t.Helper()
	return payOf(s.e.t, s.e.dev.send(s.ps, s.e.dev.eventBody(uuid.New(), "paystation.scan", qrCredential(token))))
}

func (s *qrStay) cash(stay uuid.UUID, amount int64) payData {
	s.e.t.Helper()
	return payOf(s.e.t, s.e.dev.send(s.ps, s.e.dev.eventBody(uuid.New(), "paystation.cash_paid",
		map[string]any{"stay_id": stay, "amount_cents": amount})))
}

func (s *qrStay) exitCred(token string) payData {
	s.e.t.Helper()
	return payOf(s.e.t, s.e.dev.send(s.ex, s.e.dev.eventBody(uuid.New(), "exit.credential_presented", qrCredential(token))))
}

func (s *qrStay) exitPass() passData {
	s.e.t.Helper()
	return passOf(s.e.t, s.e.dev.send(s.ex, s.e.dev.passBody(gate.DirectionExit, s.stay)))
}

// parkingUsers counts the parking_fee payments of the stay and how many of
// them carry the reservation's user (M3B-27).
func (s *qrStay) parkingUsers() (total, withUser int) {
	s.e.t.Helper()
	total = s.e.count(`select count(*) from payments p join stays st on st.id = p.stay_id
	                   where st.public_id = $1 and p.kind = 'parking_fee'`, s.stay)
	withUser = s.e.count(`select count(*) from payments p join stays st on st.id = p.stay_id
	                      join reservations r on r.id = st.reservation_id
	                      where st.public_id = $1 and p.kind = 'parking_fee' and p.user_id = r.user_id`, s.stay)
	return total, withUser
}

func (s *qrStay) wantCompleted(at time.Time) {
	s.e.t.Helper()
	if n := s.e.count(`select count(*) from reservations where public_id = $1 and status = 'completed' and completed_at = $2`,
		s.id, at); n != 1 {
		s.e.t.Errorf("want the reservation completed at %s", at)
	}
}

func TestGateQR_PayStationAndExit(t *testing.T) {
	e := newQREnv(t)
	s := e.inside()
	// A newer tariff after the booking must not reprice the stay (M3B-25).
	if _, err := e.db.Super.Exec(t.Context(),
		`insert into tariff_versions (lot_id, effective_from, first_hour_cents, additional_hour_cents, exit_grace_min,
		                              post_payment_exit_min, daily_cap_cents, lost_ticket_cents, reservation_fee_cents)
		 select lot_id, '2026-11-30T00:00:00Z', 9000, 9000, exit_grace_min, post_payment_exit_min, 90000,
		        lost_ticket_cents, reservation_fee_cents
		 from tariff_versions where lot_id = $1 order by effective_from limit 1`, e.lotID); err != nil {
		t.Fatal(err)
	}
	e.clk.Advance(2*time.Hour + 10*time.Minute)

	got := s.scan(s.qr)
	if got.Result != "ack" || got.StayID == nil || *got.StayID != s.stay || got.amount() != 10000 {
		t.Fatalf("scan = %+v amount %d, want ack 10000 on the reservation's tariff", got, got.amount())
	}
	if due := s.exitCred(s.qr); due.Result != "deny" || due.Reason != "gate.payment_required" {
		t.Fatalf("exit with a fee due = %+v, want deny gate.payment_required", due)
	}
	paid := s.cash(s.stay, 10000)
	if paid.Result != "ack" || paid.ReceiptNo == "" {
		t.Fatalf("cash = %+v, want ack with a receipt", paid)
	}
	if total, withUser := s.parkingUsers(); total != 1 || withUser != 1 {
		t.Errorf("cash payments = %d, with the reservation's user = %d, want 1 and 1", total, withUser)
	}
	wantOpen(t, s.exitCred(s.qr), s.stay)
	if p := s.exitPass(); p.Result != "ack" || p.Reason != "" {
		t.Fatalf("exit pass = %+v, want ack", p)
	}
	if n := e.count(`select count(*) from stays where public_id = $1 and status = 'exited' and exited_at = $2`, s.stay, e.clk.Now()); n != 1 {
		t.Error("want the stay exited at now")
	}
	s.wantCompleted(e.clk.Now())

	// The driver's receipt names the reservation; another driver sees none.
	rsv := &rsvEnv{lotEnv: e.lotEnv}
	_, body := rsv.do(rsv.token(s.d), http.MethodGet, "/v1/payments", nil)
	var cash map[string]any
	for _, it := range body["data"].([]any) {
		if m := it.(map[string]any); m["kind"] == "parking_fee" {
			cash = m
		}
	}
	if cash == nil || cash["method"] != "cash" || cash["reservation_id"] != s.id || cash["receipt_no"] != paid.ReceiptNo {
		t.Errorf("parking receipt = %v, want the cash payment of reservation %s", cash, s.id)
	}
	other := testsupport.SeedDrivers(t, e.db, 1)[0]
	if _, body := rsv.do(rsv.token(other), http.MethodGet, "/v1/payments", nil); len(body["data"].([]any)) != 0 {
		t.Errorf("another driver's receipts = %v, want none", body["data"])
	}
	e.invariants()
}

func TestGateQR_ZeroFeeExitSetsUser(t *testing.T) {
	e := newQREnv(t)
	s := e.inside()
	e.clk.Advance(5 * time.Minute)

	wantOpen(t, s.exitCred(s.qr), s.stay)
	if p := s.exitPass(); p.Result != "ack" {
		t.Fatalf("exit pass = %+v, want ack", p)
	}
	if total, withUser := s.parkingUsers(); total != 1 || withUser != 1 {
		t.Errorf("zero payments = %d, with the reservation's user = %d, want 1 and 1", total, withUser)
	}
	if n := e.count(`select count(*) from payments where method = 'none' and amount_cents = 0`); n != 1 {
		t.Error("want one zero payment")
	}
	s.wantCompleted(e.clk.Now())
	e.invariants()
}

// FR-PAY-7: the 150.00 reservation fee does not reduce the hourly charge.
func TestGateQR_FeeNotCredited(t *testing.T) {
	e := newQREnv(t)
	s := e.inside()
	e.clk.Advance(30 * time.Minute)
	if got := s.scan(s.qr); got.amount() != 5000 {
		t.Errorf("scan = %+v, want the full first hour 5000", got)
	}
	e.invariants()
}

// void, late entry pass, pay, exit: the reservation ends completed.
func TestGateQR_LateEntryThenExitCompletes(t *testing.T) {
	e := newQREnv(t)
	s := e.booked()
	s.stay = wantQROpen(t, e.present(s.qr))
	e.clk.Advance(61 * time.Second)
	if n := e.void(); n != 1 {
		t.Fatalf("voided = %d, want 1", n)
	}
	if p := e.dev.pass(e.barrier, s.stay); p.Result != "ack" {
		t.Fatalf("late entry pass = %+v, want ack", p)
	}
	e.clk.Advance(2*time.Hour + 10*time.Minute)

	got := s.scan(s.qr)
	if got.Result != "ack" || got.amount() != 10000 {
		t.Fatalf("scan = %+v, want ack 10000", got)
	}
	if paid := s.cash(s.stay, 10000); paid.Result != "ack" {
		t.Fatalf("cash = %+v", paid)
	}
	wantOpen(t, s.exitCred(s.qr), s.stay)
	if p := s.exitPass(); p.Result != "ack" {
		t.Fatalf("exit pass = %+v", p)
	}
	s.wantCompleted(e.clk.Now())
	if total, withUser := s.parkingUsers(); total != 1 || withUser != 1 {
		t.Errorf("parking payments = %d, with the user = %d, want 1 and 1", total, withUser)
	}
	e.invariants()
}

// The late pass of a cancelled reservation's stay: the car still leaves and
// the reservation stays cancelled (Complete is a no-op).
func TestGateQR_ExitAfterCancelledLatePass(t *testing.T) {
	e := newQREnv(t)
	s := e.booked()
	s.stay = wantQROpen(t, e.present(s.qr))
	e.clk.Advance(61 * time.Second)
	if n := e.void(); n != 1 {
		t.Fatalf("voided = %d, want 1", n)
	}
	rsv := &rsvEnv{lotEnv: e.lotEnv}
	if code, body := rsv.post(rsv.token(s.d), "/v1/reservations/"+s.id+"/cancel", nil); code != http.StatusOK {
		t.Fatalf("cancel = %d %v", code, body)
	}
	if p := e.dev.pass(e.barrier, s.stay); p.Result != "ack" {
		t.Fatalf("late entry pass = %+v, want ack", p)
	}
	e.clk.Advance(5 * time.Minute)

	wantOpen(t, s.exitCred(s.qr), s.stay)
	if p := s.exitPass(); p.Result != "ack" || p.Reason != "" {
		t.Fatalf("exit pass = %+v, want ack", p)
	}
	if n := e.count(`select count(*) from stays where public_id = $1 and status = 'exited'`, s.stay); n != 1 {
		t.Error("want the stay exited")
	}
	if n := e.count(`select count(*) from reservations where public_id = $1 and status = 'cancelled' and completed_at is null`, s.id); n != 1 {
		t.Error("want the reservation still cancelled")
	}
	e.invariants()
}

func TestGateQR_PayStationUnknownToken(t *testing.T) {
	e := newQREnv(t)
	s := e.inside()
	forged := s.qr[:len(s.qr)-2] + "xx"
	for name, tok := range map[string]string{"forged": forged, "garbage": "not-a-token"} {
		if got := s.scan(tok); got.Result != "rejected" || got.Reason != "gate.credential_invalid" {
			t.Errorf("%s scan = %+v, want rejected gate.credential_invalid", name, got)
		}
		if got := s.exitCred(tok); got.Result != "deny" || got.Reason != "gate.credential_invalid" {
			t.Errorf("%s exit = %+v, want deny gate.credential_invalid", name, got)
		}
	}
	e.invariants()
}
