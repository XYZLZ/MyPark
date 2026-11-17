package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/XYZLZ/MyPark/apps/server/internal/testsupport"
)

// payAnswer is one parallel pay answer, collected without touching t from a goroutine.
type payAnswer struct {
	code     int
	body     map[string]any
	replayed string
}

// parallelPay sends one pay per key at once and returns a channel of answers.
func (e *feeEnv) parallelPay(token, id string, keys []string) <-chan payAnswer {
	out := make(chan payAnswer, len(keys))
	var wg sync.WaitGroup
	for _, k := range keys {
		wg.Go(func() {
			rec := e.payReq(token, id, k, goodCard())
			out <- payAnswer{code: rec.Code, body: bodyOf(rec), replayed: rec.Header().Get("Idempotent-Replayed")}
		})
	}
	go func() { wg.Wait(); close(out) }()
	return out
}

func (e *feeEnv) next(ch <-chan payAnswer) payAnswer {
	e.t.Helper()
	select {
	case r, ok := <-ch:
		if !ok {
			e.t.Fatal("answers ended early")
		}
		return r
	case <-time.After(15 * time.Second):
		e.t.Fatal("timed out waiting for a pay answer")
	case <-e.t.Context().Done():
		e.t.Fatal("test ended while waiting for a pay answer")
	}
	return payAnswer{}
}

func TestPay_SameKeyParallel(t *testing.T) {
	e := newFeeEnv(t)
	d := testsupport.SeedDrivers(t, e.db, 1)[0]
	id := e.reserve(d, arrival1502)
	tok := e.token(d)
	e.gw.gate = make(chan struct{})

	keys := make([]string, 10)
	for i := range keys {
		keys[i] = "key-parallel-1"
	}
	ch := e.parallelPay(tok, id, keys)

	// The nine followers answer while the leader's charge is held.
	var pending string
	for range 9 {
		r := e.next(ch)
		pid, _ := field(r.body, "data.payment.id").(string)
		if r.code != http.StatusAccepted || r.replayed != "true" || pid == "" ||
			field(r.body, "data.payment.status") != "pending" || field(r.body, "data.reservation.status") != "pending_payment" {
			t.Fatalf("follower = %d %v replayed=%q, want 202 pending with Idempotent-Replayed", r.code, r.body, r.replayed)
		}
		if pending != "" && pending != pid {
			t.Errorf("payment ids differ: %s and %s", pending, pid)
		}
		pending = pid
	}
	close(e.gw.gate)
	leader := e.next(ch)
	if leader.code != http.StatusOK || field(leader.body, "data.status") != "confirmed" {
		t.Fatalf("leader = %d %v, want 200 confirmed", leader.code, leader.body)
	}

	if n := e.gw.charges.Load(); n != 1 {
		t.Errorf("gateway charges = %d, want exactly 1", n)
	}
	if n := e.count(`select count(*) from payments`); n != 1 {
		t.Errorf("payment rows = %d, want 1", n)
	}
	if n := e.count(`select count(*) from sim_gateway_charges`); n != 1 {
		t.Errorf("gateway rows = %d, want 1", n)
	}
	var pubID string
	if err := e.db.App.QueryRow(t.Context(), `select public_id::text from payments`).Scan(&pubID); err != nil || pubID != pending {
		t.Errorf("payment public id = %s (%v), followers saw %s", pubID, err, pending)
	}
	if got := e.status(id); got != "confirmed" {
		t.Errorf("final status = %s, want confirmed", got)
	}
	code, body, hdr := e.pay(tok, id, "key-parallel-1", goodCard())
	if code != http.StatusOK || hdr.Get("Idempotent-Replayed") != "true" || field(body, "data.status") != "confirmed" {
		t.Errorf("later replay = %d %v replayed=%q, want 200 + header", code, body, hdr.Get("Idempotent-Replayed"))
	}
	testsupport.AssertSlotInvariant(t, e.db.App, e.lotID)
}

func TestPay_DifferentKeysParallel(t *testing.T) {
	e := newFeeEnv(t)
	d := testsupport.SeedDrivers(t, e.db, 1)[0]
	id := e.reserve(d, arrival1502)
	e.gw.gate = make(chan struct{})

	ch := e.parallelPay(e.token(d), id, []string{"key-diff-0001", "key-diff-0002", "key-diff-0003", "key-diff-0004", "key-diff-0005"})
	for range 4 {
		r := e.next(ch)
		if r.code != http.StatusConflict || errCode(r.body) != "payment.in_progress" {
			t.Fatalf("non-leader = %d %v, want 409 payment.in_progress", r.code, r.body)
		}
	}
	close(e.gw.gate)
	if r := e.next(ch); r.code != http.StatusOK {
		t.Fatalf("leader = %d %v, want 200", r.code, r.body)
	}
	if n := e.gw.charges.Load(); n != 1 {
		t.Errorf("gateway charges = %d, want 1", n)
	}
	if n := e.count(`select count(*) from payments where status in ('pending', 'succeeded', 'refunded')`); n != 1 {
		t.Errorf("live fee payments = %d, want 1", n)
	}
	testsupport.AssertSlotInvariant(t, e.db.App, e.lotID)
}

// bodyOf decodes a recorded JSON body without a *testing.T, for goroutines.
func bodyOf(rec *httptest.ResponseRecorder) map[string]any {
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return out
}
