package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/XYZLZ/MyPark/apps/server/internal/features/payments"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/reservations"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/paymentsim"
	"github.com/XYZLZ/MyPark/apps/server/internal/testsupport"
)

// gatewayDouble wraps the simulated gateway: it counts charges and can hold
// them on a channel, so a test controls when the charge answers.
type gatewayDouble struct {
	inner   payments.Gateway
	charges atomic.Int32
	// gate, when non-nil, blocks every Charge until it is closed.
	gate    chan struct{}
	entered chan struct{}
}

func (g *gatewayDouble) Charge(ctx context.Context, req payments.ChargeRequest) (payments.ChargeResult, error) {
	g.charges.Add(1)
	if g.entered != nil {
		select {
		case g.entered <- struct{}{}:
		default:
		}
	}
	if g.gate != nil {
		select {
		case <-g.gate:
		case <-ctx.Done():
			return payments.ChargeResult{}, ctx.Err()
		}
	}
	return g.inner.Charge(ctx, req)
}

func (g *gatewayDouble) Refund(ctx context.Context, req payments.RefundRequest) (payments.RefundResult, error) {
	return g.inner.Refund(ctx, req)
}

// feeEnv is the production handler with the simulated gateway behind a double
// whose sleep never waits, and the clock at rsvNow.
type feeEnv struct {
	*lotEnv
	gw   *gatewayDouble
	logs *testsupport.SyncBuffer
}

func newFeeEnv(t *testing.T, mods ...func(*deps)) *feeEnv {
	t.Helper()
	logs := &testsupport.SyncBuffer{}
	gw := &gatewayDouble{}
	mods = append([]func(*deps){func(d *deps) {
		d.log = slog.New(slog.NewTextHandler(logs, nil))
		gw.inner = paymentsim.New(d.pool, paymentsim.Config{
			Clock: d.clock, Sleep: func(context.Context, time.Duration) error { return nil },
		})
		d.gateway = gw
	}}, mods...)
	e := newLotEnv(t, mods...)
	e.clk.Set(rsvNow)
	return &feeEnv{lotEnv: e, gw: gw, logs: logs}
}

func (e *feeEnv) token(d testsupport.Driver) string {
	e.t.Helper()
	return (&rsvEnv{lotEnv: e.lotEnv}).token(d)
}

// reserve creates a pending_payment reservation for the driver and returns its id.
func (e *feeEnv) reserve(d testsupport.Driver, arrival string) string {
	e.t.Helper()
	code, body := (&rsvEnv{lotEnv: e.lotEnv}).post(e.token(d), "/v1/reservations",
		map[string]any{"lot_id": e.publicID, "vehicle_id": d.VehicleID.String(), "arrival_at": arrival})
	if code != http.StatusCreated {
		e.t.Fatalf("reserve = %d %v", code, body)
	}
	return field(body, "data.id").(string)
}

func goodCard() map[string]any {
	return map[string]any{"card_number": "4242424242424242", "exp_month": 12, "exp_year": 2030, "cvc": "123"}
}

func cardWith(number string) map[string]any {
	c := goodCard()
	c["card_number"] = number
	return c
}

func (e *feeEnv) payReq(token, id, key string, body any) *httptest.ResponseRecorder {
	req := jsonReq(http.MethodPost, "/v1/reservations/"+id+"/pay", body)
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	return e.serve(req, token)
}

func (e *feeEnv) pay(token, id, key string, body any) (int, map[string]any, http.Header) {
	e.t.Helper()
	rec := e.payReq(token, id, key, body)
	return rec.Code, decodeBody(e.t, rec), rec.Header()
}

func (e *feeEnv) status(id string) string {
	e.t.Helper()
	var s string
	if err := e.db.App.QueryRow(e.t.Context(), `select status from reservations where public_id = $1`, id).Scan(&s); err != nil {
		e.t.Fatal(err)
	}
	return s
}

func TestPay_Success(t *testing.T) {
	e := newFeeEnv(t)
	d := testsupport.SeedDrivers(t, e.db, 1)[0]
	id := e.reserve(d, arrival1502)
	tok := e.token(d)

	code, body, hdr := e.pay(tok, id, "key-success-1", goodCard())
	if code != http.StatusOK || field(body, "data.status") != "confirmed" {
		t.Fatalf("pay = %d %v, want 200 confirmed", code, body)
	}
	if hdr.Get("Idempotent-Replayed") != "" {
		t.Error("a first answer must not carry Idempotent-Replayed")
	}
	var status, ref, brand, last4 string
	err := e.db.App.QueryRow(t.Context(),
		`select p.status, p.provider_ref, p.card_brand, p.card_last4 from payments p join reservations r on r.id = p.reservation_id
		 where r.public_id = $1`, id).Scan(&status, &ref, &brand, &last4)
	if err != nil || status != "succeeded" || ref == "" || brand != "visa" || last4 != "4242" {
		t.Errorf("payment = %q %q %q %q (%v), want succeeded with ref, visa 4242", status, ref, brand, last4, err)
	}
	if n := e.count(`select count(*) from reservations where public_id = $1 and qr_nonce is not null and confirmed_at is not null`, id); n != 1 {
		t.Error("confirmed reservation must carry qr_nonce and confirmed_at")
	}

	var payload, to string
	if err := e.db.Super.QueryRow(t.Context(),
		`select payload->>'qr_token', to_address from outbox_messages where template = 'reservation_confirmed' and dedupe_key = $1`,
		"reservation_confirmed:"+id).Scan(&payload, &to); err != nil {
		t.Fatalf("outbox row: %v", err)
	}

	rec := e.serve(jsonReq(http.MethodGet, "/v1/reservations/"+id+"/qr", nil), tok)
	qr := decodeBody(t, rec)
	if rec.Code != http.StatusOK || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("qr = %d cache=%q %v", rec.Code, rec.Header().Get("Cache-Control"), qr)
	}
	token, _ := field(qr, "data.token").(string)
	gotID, _, ok := reservations.DecodeQR(e.cfg.QRHMACKey(), token)
	if !ok || gotID.String() != id {
		t.Errorf("qr token decodes to %v ok=%v, want %s", gotID, ok, id)
	}
	if token != payload {
		t.Error("the emailed token and GET /qr must be the same token")
	}
	for path, want := range map[string]any{
		"data.entry_valid_from": "2026-12-01T14:50:00Z", "data.entry_valid_until": "2026-12-01T15:25:00Z",
	} {
		if got := field(qr, path); got != want {
			t.Errorf("%s = %v, want %v", path, got, want)
		}
	}
	testsupport.AssertSlotInvariant(t, e.db.App, e.lotID)
}

func TestQR_OnlyConfirmedOrCheckedIn(t *testing.T) {
	e := newFeeEnv(t)
	d := testsupport.SeedDrivers(t, e.db, 1)[0]
	id := e.reserve(d, arrival1502)
	rec := e.serve(jsonReq(http.MethodGet, "/v1/reservations/"+id+"/qr", nil), e.token(d))
	body := decodeBody(t, rec)
	if rec.Code != http.StatusConflict || errCode(body) != "reservation.invalid_state" || field(body, "error.details.status") != "pending_payment" {
		t.Errorf("qr of an unpaid reservation = %d %v, want 409 invalid_state pending_payment", rec.Code, body)
	}
	if strings.Contains(rec.Body.String(), `"token"`) {
		t.Error("no token for an unpaid reservation")
	}
}

func TestPay_ConfirmInsideWindowBumpsVersion(t *testing.T) {
	version := func(e *feeEnv) int64 {
		var v int64
		if err := e.db.App.QueryRow(t.Context(), `select version from lot_occupancy where lot_id = $1`, e.lotID).Scan(&v); err != nil {
			t.Fatal(err)
		}
		return v
	}
	t.Run("window contains now", func(t *testing.T) {
		e := newFeeEnv(t)
		d := testsupport.SeedDrivers(t, e.db, 1)[0]
		// Normalized to 14:05: the window 13:50 to 14:25 contains 13:58:10.
		id := e.reserve(d, "2026-12-01T14:02:00Z")
		conn, err := e.db.App.Acquire(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Release()
		if _, err := conn.Exec(t.Context(), `listen lot_changed`); err != nil {
			t.Fatal(err)
		}
		before := version(e)
		if code, body, _ := e.pay(e.token(d), id, "key-inside-01", goodCard()); code != http.StatusOK {
			t.Fatalf("pay = %d %v", code, body)
		}
		if got := version(e); got != before+1 {
			t.Errorf("version = %d, want %d", got, before+1)
		}
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		defer cancel()
		n, err := conn.Conn().WaitForNotification(ctx)
		if err != nil || n.Payload != fmt.Sprintf("%d:%d", e.lotID, before+1) {
			t.Errorf("notification = %+v %v, want lot_changed %d:%d", n, err, e.lotID, before+1)
		}
		testsupport.AssertSlotInvariant(t, e.db.App, e.lotID)
	})
	t.Run("window in the future", func(t *testing.T) {
		e := newFeeEnv(t)
		d := testsupport.SeedDrivers(t, e.db, 1)[0]
		id := e.reserve(d, arrival1502)
		before := version(e)
		if code, body, _ := e.pay(e.token(d), id, "key-future-01", goodCard()); code != http.StatusOK {
			t.Fatalf("pay = %d %v", code, body)
		}
		if got := version(e); got != before {
			t.Errorf("version = %d, want unchanged %d", got, before)
		}
	})
}

func TestPay_DeclineThenRetry(t *testing.T) {
	e := newFeeEnv(t)
	d := testsupport.SeedDrivers(t, e.db, 1)[0]
	id := e.reserve(d, arrival1502)
	tok := e.token(d)

	code, body, _ := e.pay(tok, id, "key-decline-1", cardWith("4000000000000002"))
	if code != http.StatusPaymentRequired || errCode(body) != "payment.declined" ||
		field(body, "error.details.failure_code") != "card_declined" {
		t.Fatalf("declined pay = %d %v, want 402 payment.declined card_declined", code, body)
	}
	pid, _ := field(body, "error.details.payment_id").(string)
	if _, err := uuid.Parse(pid); err != nil {
		t.Errorf("details.payment_id = %q, want a payment uuid", pid)
	}
	if got := e.status(id); got != "pending_payment" {
		t.Errorf("status after a decline = %s, want pending_payment", got)
	}
	if code, body, _ := e.pay(tok, id, "key-decline-2", goodCard()); code != http.StatusOK {
		t.Fatalf("retry with a new key = %d %v, want 200", code, body)
	}
	if n := e.count(`select count(*) from payments where status = 'failed' and failure_code = 'card_declined'`); n != 1 {
		t.Errorf("failed payments = %d, want 1", n)
	}
	testsupport.AssertSlotInvariant(t, e.db.App, e.lotID)
}

func TestPay_ReplaySameKey(t *testing.T) {
	e := newFeeEnv(t)
	ds := testsupport.SeedDrivers(t, e.db, 2)
	t.Run("succeeded", func(t *testing.T) {
		id := e.reserve(ds[0], arrival1502)
		tok := e.token(ds[0])
		if code, body, _ := e.pay(tok, id, "key-replay-ok1", goodCard()); code != http.StatusOK {
			t.Fatalf("first pay = %d %v", code, body)
		}
		code, body, hdr := e.pay(tok, id, "key-replay-ok1", goodCard())
		if code != http.StatusOK || field(body, "data.status") != "confirmed" || hdr.Get("Idempotent-Replayed") != "true" {
			t.Errorf("replay = %d %v replayed=%q, want 200 confirmed + header", code, body, hdr.Get("Idempotent-Replayed"))
		}
		if e.gw.charges.Load() != 1 {
			t.Errorf("gateway charges = %d, want 1", e.gw.charges.Load())
		}
	})
	t.Run("declined", func(t *testing.T) {
		id := e.reserve(ds[1], arrival1502)
		tok := e.token(ds[1])
		_, first, _ := e.pay(tok, id, "key-replay-no1", cardWith("4000000000000002"))
		code, body, hdr := e.pay(tok, id, "key-replay-no1", cardWith("4000000000000002"))
		if code != http.StatusPaymentRequired || hdr.Get("Idempotent-Replayed") != "true" ||
			field(body, "error.details.payment_id") != field(first, "error.details.payment_id") {
			t.Errorf("replay = %d %v replayed=%q, want the same 402 + header", code, body, hdr.Get("Idempotent-Replayed"))
		}
		if e.gw.charges.Load() != 2 {
			t.Errorf("gateway charges = %d, want 2 (one per payment)", e.gw.charges.Load())
		}
		if n := e.count(`select count(*) from sim_gateway_charges`); n != 2 {
			t.Errorf("gateway rows = %d, want 2", n)
		}
	})
	testsupport.AssertSlotInvariant(t, e.db.App, e.lotID)
}

func TestPay_SameKeyDifferentCard(t *testing.T) {
	e := newFeeEnv(t)
	d := testsupport.SeedDrivers(t, e.db, 1)[0]
	id := e.reserve(d, arrival1502)
	tok := e.token(d)
	if code, body, _ := e.pay(tok, id, "key-card-0001", cardWith("4000000000000002")); code != http.StatusPaymentRequired {
		t.Fatalf("first pay = %d %v", code, body)
	}
	code, body, _ := e.pay(tok, id, "key-card-0001", goodCard())
	if code != http.StatusConflict || errCode(body) != "idempotency.key_reused" {
		t.Errorf("same key, other card = %d %v, want 409 idempotency.key_reused", code, body)
	}
	if e.gw.charges.Load() != 1 {
		t.Errorf("gateway charges = %d, want 1", e.gw.charges.Load())
	}
}

func (e *feeEnv) count(query string, args ...any) int {
	e.t.Helper()
	return (&rsvEnv{lotEnv: e.lotEnv}).count(query, args...)
}
