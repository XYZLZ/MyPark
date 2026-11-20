package main

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/XYZLZ/MyPark/apps/server/internal/testsupport"
)

// Review Focus 3: a client sends one key per screen, not per reservation.
func TestPay_SameKeyDifferentReservations(t *testing.T) {
	e := newFeeEnv(t)
	ds := testsupport.SeedDrivers(t, e.db, 2)
	const key = "k-12345678"
	for _, d := range ds {
		id := e.reserve(d, arrival1502)
		if code, body, hdr := e.pay(e.token(d), id, key, goodCard()); code != http.StatusOK || hdr.Get("Idempotent-Replayed") != "" {
			t.Fatalf("pay with the shared key = %d %v replayed=%q, want a fresh 200", code, body, hdr.Get("Idempotent-Replayed"))
		}
	}
	if n := e.count(`select count(*) from payments where client_key = $1 and status = 'succeeded'`, key); n != 2 {
		t.Errorf("payments with the shared key = %d, want 2", n)
	}
	if n := e.count(`select count(*) from sim_gateway_charges`); n != 2 {
		t.Errorf("gateway rows = %d, want 2", n)
	}
	testsupport.AssertSlotInvariant(t, e.db.App, e.lotID)
}

func TestPay_InvalidStateAndExpired(t *testing.T) {
	e := newFeeEnv(t)
	ds := testsupport.SeedDrivers(t, e.db, 2)

	id := e.reserve(ds[0], arrival1502)
	if code, body, _ := e.pay(e.token(ds[0]), id, "key-state-0001", goodCard()); code != http.StatusOK {
		t.Fatalf("pay = %d %v", code, body)
	}
	code, body, _ := e.pay(e.token(ds[0]), id, "key-state-0002", goodCard())
	if code != http.StatusConflict || errCode(body) != "reservation.invalid_state" || field(body, "error.details.status") != "confirmed" {
		t.Errorf("pay a confirmed reservation = %d %v, want 409 invalid_state confirmed", code, body)
	}

	id2 := e.reserve(ds[1], arrival1502)
	e.clk.Set(rsvNow.Add(10 * time.Minute)) // payment_due_at is exactly now
	code, body, _ = e.pay(e.token(ds[1]), id2, "key-state-0003", goodCard())
	if code != http.StatusConflict || errCode(body) != "reservation.payment_expired" {
		t.Errorf("pay at payment_due_at = %d %v, want 409 reservation.payment_expired", code, body)
	}
	if e.gw.charges.Load() != 1 {
		t.Errorf("gateway charges = %d, want 1", e.gw.charges.Load())
	}
}

// Review Focus 2: card fields typed the way people type them.
func TestPay_CardJunkIs422WithoutEcho(t *testing.T) {
	e := newFeeEnv(t)
	d := testsupport.SeedDrivers(t, e.db, 1)[0]
	id := e.reserve(d, arrival1502)
	tok := e.token(d)

	// Spaces are stripped before validation.
	if code, body, _ := e.pay(tok, id, "key-junk-ok01", cardWith("4242 4242 4242 4242")); code != http.StatusOK {
		t.Fatalf("spaced PAN = %d %v, want 200", code, body)
	}

	d2 := testsupport.SeedDrivers(t, e.db, 1)[0]
	id2 := e.reserve(d2, arrival1502)
	cases := []struct {
		name, key string
		body      map[string]any
		field     string
		secrets   []string
	}{
		{"two digit year", "key-junk-0002", with(goodCard(), "exp_year", 28), "exp_year", []string{"4242424242424242"}},
		{"expired card", "key-junk-0003", with(with(goodCard(), "exp_month", 11), "exp_year", 2026), "exp_year", []string{"4242424242424242"}},
		{"bad month", "key-junk-0004", with(goodCard(), "exp_month", 13), "exp_month", []string{"4242424242424242"}},
		{"five digit cvc", "key-junk-0005", with(goodCard(), "cvc", "12345"), "cvc", []string{"12345", "4242424242424242"}},
		{"letters in cvc", "key-junk-0006", with(goodCard(), "cvc", "12a"), "cvc", []string{"12a"}},
		{"luhn", "key-junk-0007", cardWith("4242424242424241"), "card_number", []string{"4242424242424241"}},
		{"junk pan", "key-junk-0008", cardWith("4242.4242.4242.4242"), "card_number", []string{"4242.4242"}},
		{"missing key", "", goodCard(), "Idempotency-Key", nil},
		{"short key", "short", goodCard(), "Idempotency-Key", []string{"short"}},
		{"key with space", "key with space1", goodCard(), "Idempotency-Key", []string{"key with space1"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := e.payReq(e.token(d2), id2, c.key, c.body)
			body := decodeBody(t, rec)
			if rec.Code != http.StatusUnprocessableEntity || errCode(body) != "validation.failed" {
				t.Fatalf("= %d %v, want 422 validation.failed", rec.Code, body)
			}
			fields, _ := field(body, "error.details.fields").([]any)
			found := false
			for _, f := range fields {
				if m, _ := f.(map[string]any); m["field"] == c.field {
					found = true
				}
			}
			if !found {
				t.Errorf("details.fields = %v, want one naming %s", fields, c.field)
			}
			for _, s := range c.secrets {
				if strings.Contains(rec.Body.String(), s) {
					t.Errorf("the 422 body echoes %q", s)
				}
			}
		})
	}
	// The PAN and the CVC never reach the logs either.
	for _, s := range []string{"4242424242424242", "4242 4242", "4242.4242", "4242424242424241"} {
		if strings.Contains(e.logs.String(), s) {
			t.Errorf("logs contain %q", s)
		}
	}
	if n := e.count(`select count(*) from payments`); n != 1 {
		t.Errorf("payments = %d, want 1: junk must store and charge nothing", n)
	}
	if e.gw.charges.Load() != 1 {
		t.Errorf("gateway charges = %d, want 1", e.gw.charges.Load())
	}
}

func with(m map[string]any, k string, v any) map[string]any {
	out := map[string]any{}
	for kk, vv := range m {
		out[kk] = vv
	}
	out[k] = v
	return out
}

// Review Focus 2: hyphens are stripped like spaces, and a wrong JSON type is
// the project-wide 400 http.invalid_json without echoing the value.
func TestPay_HyphenPANAndWrongJSONType(t *testing.T) {
	e := newFeeEnv(t)
	ds := testsupport.SeedDrivers(t, e.db, 2)
	id := e.reserve(ds[0], arrival1502)
	if code, body, _ := e.pay(e.token(ds[0]), id, "key-hyphen-01", cardWith("4242-4242-4242-4242")); code != http.StatusOK {
		t.Fatalf("hyphenated PAN = %d %v, want 200", code, body)
	}

	id2 := e.reserve(ds[1], arrival1502)
	rec := e.payReq(e.token(ds[1]), id2, "key-string-01", with(goodCard(), "exp_year", "2028"))
	body := decodeBody(t, rec)
	if rec.Code != http.StatusBadRequest || errCode(body) != "http.invalid_json" {
		t.Fatalf("string exp_year = %d %v, want 400 http.invalid_json", rec.Code, body)
	}
	if d, _ := field(body, "error.details").(map[string]any); len(d) != 0 {
		t.Errorf("details = %v, want none", d)
	}
	if strings.Contains(rec.Body.String(), "2028") || strings.Contains(rec.Body.String(), "4242") {
		t.Error("the 400 body echoes request values")
	}
	if e.gw.charges.Load() != 1 {
		t.Errorf("gateway charges = %d, want 1", e.gw.charges.Load())
	}
}
