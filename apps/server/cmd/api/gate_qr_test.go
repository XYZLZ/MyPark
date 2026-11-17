package main

import (
	"context"
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
	forged := []byte(good)
	forged[40] ^= 'A' ^ 'B'
	if forged[40] == good[40] {
		forged[40] = 'C'
	}
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
		{"forged mac", e.barrier, string(forged)},
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
