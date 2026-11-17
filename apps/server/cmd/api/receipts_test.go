package main

import (
	"fmt"
	"net/http"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/XYZLZ/MyPark/apps/server/internal/testsupport"
)

// seedPayment inserts a reservation fee of the driver straight into the
// database, created at the given time. refunded also inserts a succeeded refund.
func (e *rsvEnv) seedPayment(d testsupport.Driver, status string, created time.Time, refunded bool) (public uuid.UUID, rowID int64) {
	e.t.Helper()
	rsv := e.seedRes(d, "completed", rsvAt(-time.Hour)) // inactive: one active reservation per user
	key := uuid.NewString()
	var failureCode, providerRef, succeededAt, failedAt, refundedAt any
	switch status {
	case "failed":
		failureCode, failedAt = "card_declined", created
	case "succeeded", "refunded":
		providerRef, succeededAt = "prov-"+key, created
	}
	if status == "refunded" {
		refundedAt = created.Add(time.Minute)
	}
	err := e.db.Super.QueryRow(e.t.Context(),
		`insert into payments (kind, method, amount_cents, status, idempotency_key, reservation_id, user_id,
		                       client_key, request_fingerprint, card_brand, card_last4, provider_ref,
		                       succeeded_at, failed_at, failure_code, refunded_at, created_at)
		 select 'reservation_fee', 'gateway_sim', 15000, $1::text, 'rsv-fee:'||$2::text, r.id, $3, 'key-'||$2::text,
		        decode(repeat('00', 32), 'hex'), 'visa', '4242', $4, $5, $6, $7, $8, $9
		 from reservations r where r.public_id = $10
		 returning public_id, id`,
		status, key, d.UserID, providerRef, succeededAt, failedAt, failureCode, refundedAt, created, rsv).Scan(&public, &rowID)
	if err != nil {
		e.t.Fatalf("seed %s payment: %v", status, err)
	}
	if refunded {
		if _, err := e.db.Super.Exec(e.t.Context(),
			`insert into refunds (payment_id, amount_cents, reason, status, idempotency_key, next_attempt_at,
			                      provider_ref, succeeded_at)
			 values ($1, 15000, 'cancelled_before_arrival', 'succeeded', 'refund:'||$2::text, $3, 'rf-'||$2::text, $3)`,
			rowID, key, created.Add(time.Minute)); err != nil {
			e.t.Fatalf("seed refund: %v", err)
		}
	}
	return public, rowID
}

func dataIDs(body map[string]any) []string {
	var out []string
	for _, it := range body["data"].([]any) {
		out = append(out, it.(map[string]any)["id"].(string))
	}
	return out
}

func TestReceipts_ListAndPaging(t *testing.T) {
	e := newRsvEnv(t)
	d := testsupport.SeedDrivers(t, e.db, 1)[0]
	tok := e.token(d)
	now := e.clk.Now()
	failed, _ := e.seedPayment(d, "failed", now.Add(-3*time.Hour), false)
	paid, paidRow := e.seedPayment(d, "succeeded", now.Add(-2*time.Hour), false)
	refunded, _ := e.seedPayment(d, "refunded", now.Add(-time.Hour), true)
	want := []string{refunded.String(), paid.String(), failed.String()}

	code, body := e.do(tok, http.MethodGet, "/v1/payments", nil)
	if code != http.StatusOK || !reflect.DeepEqual(dataIDs(body), want) {
		t.Fatalf("list = %d %v, want %v", code, dataIDs(body), want)
	}
	items := body["data"].([]any)
	if got := items[1].(map[string]any)["receipt_no"]; got != fmt.Sprintf("MP-%08d", paidRow) {
		t.Errorf("succeeded receipt_no = %v", got)
	}
	f := items[2].(map[string]any)
	if f["receipt_no"] != nil || f["failure_code"] != "card_declined" || f["status"] != "failed" || f["refund"] != nil {
		t.Errorf("failed item = %v, want null receipt_no and refund, card_declined", f)
	}
	for _, it := range items {
		m := it.(map[string]any)
		if m["card_brand"] != "visa" || m["card_last4"] != "4242" || m["reservation_id"] == nil {
			t.Errorf("item = %v, want visa 4242 and a reservation", m)
		}
		if _, leaked := m["provider_ref"]; leaked || len(m) != 14 {
			t.Errorf("item has %d keys (%v), want exactly the 14 receipt fields", len(m), m)
		}
	}

	code, p1 := e.do(tok, http.MethodGet, "/v1/payments?limit=2", nil)
	next, _ := field(p1, "meta.page.next_cursor").(string)
	if code != http.StatusOK || !reflect.DeepEqual(dataIDs(p1), want[:2]) || next == "" {
		t.Fatalf("page 1 = %d %v next=%q", code, dataIDs(p1), next)
	}
	code, p2 := e.do(tok, http.MethodGet, "/v1/payments?limit=2&cursor="+next, nil)
	if code != http.StatusOK || !reflect.DeepEqual(dataIDs(p2), want[2:]) || field(p2, "meta.page.next_cursor") != nil {
		t.Fatalf("page 2 = %d %v meta=%v", code, dataIDs(p2), p2["meta"])
	}
}

func TestReceipts_RefundEmbedded(t *testing.T) {
	e := newRsvEnv(t)
	d := testsupport.SeedDrivers(t, e.db, 1)[0]
	tok := e.token(d)
	id, row := e.seedPayment(d, "refunded", e.clk.Now().Add(-time.Hour), true)
	code, body := e.do(tok, http.MethodGet, "/v1/payments/"+id.String(), nil)
	if code != http.StatusOK {
		t.Fatalf("get = %d %v", code, body)
	}
	if got := field(body, "data.receipt_no"); got != fmt.Sprintf("MP-%08d", row) {
		t.Errorf("receipt_no = %v", got)
	}
	if field(body, "data.status") != "refunded" || field(body, "data.refunded_at") == nil ||
		field(body, "data.refund.status") != "succeeded" || field(body, "data.refund.amount_cents") != float64(15000) ||
		field(body, "data.refund.reason") != "cancelled_before_arrival" || field(body, "data.refund.succeeded_at") == nil {
		t.Errorf("refunded receipt = %v", body["data"])
	}
}

func TestReceipts_OwnershipOnly(t *testing.T) {
	e := newRsvEnv(t)
	ds := testsupport.SeedDrivers(t, e.db, 2)
	mine, _ := e.seedPayment(ds[0], "succeeded", e.clk.Now().Add(-time.Hour), false)
	e.seedPayment(ds[1], "succeeded", e.clk.Now().Add(-time.Hour), false)
	// An M2 drive-up cash payment has no user (M3B-27); a cash payment with
	// user_id set (a reservation stay) is the driver's receipt.
	var stay int64
	if err := e.db.Super.QueryRow(t.Context(),
		`insert into stays (lot_id, credential_kind, ticket_code_hash, status, tariff_version_id, accepted_at, updated_at)
		 select $1, 'ticket', sha256('own'::bytea), 'entry_pending', id, $2, $2 from tariff_versions where lot_id = $1
		 returning id`, e.lotID, e.clk.Now()).Scan(&stay); err != nil {
		t.Fatal(err)
	}
	var cash uuid.UUID
	for i, user := range []any{nil, ds[0].UserID} {
		if i == 1 {
			if err := e.db.Super.QueryRow(t.Context(), `insert into payments (kind, method, amount_cents, status, idempotency_key,
				stay_id, user_id, succeeded_at, created_at) values ('parking_fee', 'cash', 5000, 'succeeded', 'cash:mine', $1, $2, $3, $3)
				returning public_id`, stay, user, e.clk.Now()).Scan(&cash); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if _, err := e.db.Super.Exec(t.Context(), `insert into payments (kind, method, amount_cents, status, idempotency_key,
			stay_id, user_id, succeeded_at, created_at) values ('parking_fee', 'cash', 5000, 'succeeded', 'cash:drive-up', $1, $2, $3, $3)`,
			stay, user, e.clk.Now()); err != nil {
			t.Fatal(err)
		}
	}
	code, body := e.do(e.token(ds[0]), http.MethodGet, "/v1/payments", nil)
	if code != http.StatusOK || !reflect.DeepEqual(dataIDs(body), []string{cash.String(), mine.String()}) {
		t.Fatalf("list = %d %v, want only %s then %s", code, dataIDs(body), cash, mine)
	}
	if first := body["data"].([]any)[0].(map[string]any); first["reservation_id"] != nil || first["method"] != "cash" || first["card_last4"] != nil {
		t.Errorf("cash receipt = %v, want no reservation and no card", first)
	}
}

func TestPayments_GarbageIDsAre404(t *testing.T) {
	e := newRsvEnv(t)
	ds := testsupport.SeedDrivers(t, e.db, 2)
	foreign, _ := e.seedPayment(ds[1], "succeeded", e.clk.Now().Add(-time.Hour), false)
	tok := e.token(ds[0])
	var bodies []map[string]any
	for _, x := range []string{"abc", uuid.NewString(), foreign.String()} {
		code, body := e.do(tok, http.MethodGet, "/v1/payments/"+x, nil)
		if code != http.StatusNotFound || errCode(body) != "payment.not_found" {
			t.Fatalf("GET %s = %d %v, want 404 payment.not_found", x, code, body)
		}
		delete(body["meta"].(map[string]any), "request_id")
		bodies = append(bodies, body)
	}
	for _, b := range bodies[1:] {
		if !reflect.DeepEqual(bodies[0], b) {
			t.Errorf("404 bodies differ: %v vs %v", bodies[0], b)
		}
	}
	for _, q := range []string{"?cursor=!!!", "?cursor=" + "bm90fGFjdXJzb3I", "?limit=0", "?limit=51", "?limit=x"} {
		code, body := e.do(tok, http.MethodGet, "/v1/payments"+q, nil)
		if code != http.StatusUnprocessableEntity || errCode(body) != "validation.failed" {
			t.Errorf("list %s = %d %v, want 422 validation.failed", q, code, body)
		}
	}
}
