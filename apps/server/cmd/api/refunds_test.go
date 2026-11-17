package main

import (
	"context"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	alertspg "github.com/XYZLZ/MyPark/apps/server/internal/features/alerts/postgres"
	notificationspg "github.com/XYZLZ/MyPark/apps/server/internal/features/notifications/postgres"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/payments"
	paymentspg "github.com/XYZLZ/MyPark/apps/server/internal/features/payments/postgres"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/reservations"
	reservationspg "github.com/XYZLZ/MyPark/apps/server/internal/features/reservations/postgres"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/database"
	"github.com/XYZLZ/MyPark/apps/server/internal/testsupport"
)

// refundGateway wraps the simulated gateway for the refund job: it counts the
// calls per key, fails the first failN calls, and can hold the first call.
type refundGateway struct {
	payments.Gateway
	mu      sync.Mutex
	perKey  map[string]int
	calls   int
	failN   int
	holdFst chan struct{} // when non-nil, the first call waits for it to close
	entered chan struct{} // closed when the first call starts
}

func (g *refundGateway) Refund(ctx context.Context, req payments.RefundRequest) (payments.RefundResult, error) {
	g.mu.Lock()
	g.calls++
	n := g.calls
	if g.perKey == nil {
		g.perKey = map[string]int{}
	}
	g.perKey[req.IdempotencyKey]++
	g.mu.Unlock()
	if n == 1 && g.holdFst != nil {
		close(g.entered)
		<-g.holdFst
	}
	if n <= g.failN {
		return payments.RefundResult{}, context.DeadlineExceeded
	}
	return g.Gateway.Refund(ctx, req)
}

func (e *feeEnv) processor(gw payments.Gateway) *payments.RefundProcessor {
	tx := database.NewTxRunner(e.db.App)
	fees := paymentspg.NewRepo(e.db.App)
	return &payments.RefundProcessor{
		Tx: tx, Refunds: paymentspg.NewRefundRepo(e.db.App), Gateway: gw, Alerts: alertspg.NewRecorder(),
		Notice: reservations.NewService(reservations.Deps{
			Tx: tx, Repo: reservationspg.NewRepo(e.db.App), Clock: e.clk, Log: slogOf(e.logs),
			Fees: fees, Notify: notificationspg.NewNotifier(e.clk),
		}),
		Log: slogOf(e.logs),
	}
}

func (e *feeEnv) simGateway(g *refundGateway) *refundGateway {
	g.Gateway = e.gw.inner
	return g
}

// cancelled leaves a cancelled reservation with a pending refund.
func (e *feeEnv) cancelled() (id, tok string) {
	e.t.Helper()
	id, tok = e.paid(arrival1502)
	if code, body := e.cancel(tok, id); code != http.StatusOK {
		e.t.Fatalf("cancel = %d %v", code, body)
	}
	return id, tok
}

func TestRefunds_SuccessFlow(t *testing.T) {
	e := newFeeEnv(t)
	id, tok := e.cancelled()
	now := e.clk.Now()

	n, err := e.processor(e.simGateway(&refundGateway{})).Run(t.Context(), now)
	if err != nil || n != 1 {
		t.Fatalf("Run = %d, %v; want 1", n, err)
	}
	if c := e.count(`select count(*) from refunds where status = 'succeeded' and provider_ref is not null
	                 and succeeded_at = $1 and attempts = 1`, now); c != 1 {
		t.Error("want one succeeded refund with provider_ref and succeeded_at = now")
	}
	if c := e.count(`select count(*) from payments where status = 'refunded' and refunded_at = $1`, now); c != 1 {
		t.Error("want the payment refunded at now")
	}
	if c := e.count(`select count(*) from outbox_messages o join refunds r on o.dedupe_key = 'refund_succeeded:' || r.public_id
	                 where o.template = 'refund_succeeded' and o.payload->>'reservation_id' = $1
	                 and o.payload->>'amount_cents' = '15000'`, id); c != 1 {
		t.Error("want one refund_succeeded email keyed by the refund public id")
	}
	code, body := (&rsvEnv{lotEnv: e.lotEnv}).do(tok, http.MethodGet, "/v1/reservations/"+id, nil)
	if code != http.StatusOK || field(body, "data.refund.status") != "succeeded" || field(body, "data.refund.succeeded_at") == nil {
		t.Errorf("GET reservation = %d refund %v", code, field(body, "data.refund"))
	}
	if n, _ := e.processor(e.simGateway(&refundGateway{})).Run(t.Context(), now.Add(time.Hour)); n != 0 {
		t.Errorf("second Run = %d, want 0", n)
	}
	testsupport.AssertSlotInvariant(t, e.db.App, e.lotID)
}

func TestRefunds_BackoffSchedule(t *testing.T) {
	e := newFeeEnv(t)
	e.cancelled()
	gw := e.simGateway(&refundGateway{failN: 5})
	p := e.processor(gw)
	now := e.clk.Now()
	for attempts := 1; attempts <= 5; attempts++ {
		if n, err := p.Run(t.Context(), now); err != nil || n != 0 {
			t.Fatalf("Run %d = %d, %v; want 0 settled", attempts, n, err)
		}
		wait, _ := payments.RefundBackoff(attempts)
		if c := e.count(`select count(*) from refunds where status = 'pending' and attempts = $1 and next_attempt_at = $2
		                 and last_error = 'timeout'`, attempts, now.Add(wait)); c != 1 {
			t.Fatalf("after attempt %d: want attempts %d, next_attempt_at now+%v, last_error timeout", attempts, attempts, wait)
		}
		before := gw.calls
		if _, err := p.Run(t.Context(), now.Add(wait-time.Second)); err != nil || gw.calls != before {
			t.Fatalf("a Run before next_attempt_at called the gateway (%d -> %d, %v)", before, gw.calls, err)
		}
		now = now.Add(wait)
	}
	if n, err := p.Run(t.Context(), now); err != nil || n != 1 {
		t.Fatalf("Run after the failures = %d, %v; want 1", n, err)
	}
}

func TestRefunds_TenFailuresAlert(t *testing.T) {
	e := newFeeEnv(t)
	id, _ := e.cancelled()
	p := e.processor(e.simGateway(&refundGateway{failN: 100}))
	now := e.clk.Now()
	for range payments.RefundMaxAttempts {
		if _, err := p.Run(t.Context(), now); err != nil {
			t.Fatal(err)
		}
		now = now.Add(31 * time.Minute)
	}
	failedAt := now.Add(-31 * time.Minute)
	if c := e.count(`select count(*) from refunds where status = 'failed' and attempts = 10 and failed_at = $1`, failedAt); c != 1 {
		t.Fatal("want the refund failed on attempt 10 at the Run time")
	}
	if c := e.count(`select count(*) from reconciliation_alerts where kind = 'refund_failed' and opened_at = $1
	                 and details->>'reservation_id' = $2 and details->>'reason' = 'max_attempts'
	                 and (select count(*) from jsonb_object_keys(details)) = 4
	                 and details->>'refund_id' = (select public_id::text from refunds)
	                 and details->>'payment_id' = (select public_id::text from payments)`, failedAt, id); c != 1 {
		t.Error("want one refund_failed alert with public ids and reason max_attempts only")
	}
	if c := e.count(`select count(*) from outbox_messages where template = 'refund_succeeded'`); c != 0 {
		t.Errorf("a failed refund sent %d emails", c)
	}
	if c := e.count(`select count(*) from payments where status = 'succeeded'`); c != 1 {
		t.Error("the payment must stay succeeded")
	}
	before := p.Gateway.(*refundGateway).calls
	if n, _ := p.Run(t.Context(), now.Add(time.Hour)); n != 0 || p.Gateway.(*refundGateway).calls != before {
		t.Error("a failed refund must not be claimed again")
	}
}

func TestRefunds_ParallelRunnersOneCallPerAttempt(t *testing.T) {
	e := newFeeEnv(t)
	for range 5 {
		e.cancelled()
	}
	gw := e.simGateway(&refundGateway{})
	now := e.clk.Now()
	var total atomic.Int32
	var wg sync.WaitGroup
	start := make(chan struct{})
	for range 2 {
		wg.Go(func() {
			<-start
			n, err := e.processor(gw).Run(t.Context(), now)
			if err != nil {
				t.Error(err)
			}
			total.Add(int32(n))
		})
	}
	close(start)
	wg.Wait()
	if total.Load() != 5 || len(gw.perKey) != 5 {
		t.Fatalf("settled %d refunds over %d keys, want 5 and 5", total.Load(), len(gw.perKey))
	}
	for key, n := range gw.perKey {
		if n != 1 {
			t.Errorf("%s was refunded %d times, want once", key, n)
		}
	}
	if c := e.count(`select count(*) from refunds where status = 'succeeded' and attempts = 1`); c != 5 {
		t.Errorf("succeeded refunds = %d, want 5", c)
	}
	if c := e.count(`select count(*) from outbox_messages where template = 'refund_succeeded'`); c != 5 {
		t.Errorf("emails = %d, want 5", c)
	}
}

func TestRefunds_LeaseExpiryNeverDuplicates(t *testing.T) {
	e := newFeeEnv(t)
	e.cancelled()
	gw := e.simGateway(&refundGateway{holdFst: make(chan struct{}), entered: make(chan struct{})})
	now := e.clk.Now()
	aDone := make(chan int, 1)
	go func() {
		n, err := e.processor(gw).Run(t.Context(), now) // claims attempt 1, then blocks in Refund
		if err != nil {
			t.Error(err)
		}
		aDone <- n
	}()
	<-gw.entered
	n, err := e.processor(gw).Run(t.Context(), now.Add(61*time.Second)) // the lease expired: attempt 2
	if err != nil || n != 1 {
		t.Fatalf("runner B = %d, %v; want 1", n, err)
	}
	close(gw.holdFst)
	if got := <-aDone; got != 0 {
		t.Errorf("runner A settled %d, want 0 (its fence no longer matches)", got)
	}
	if c := e.count(`select count(*) from refunds where status = 'succeeded' and attempts = 2`); c != 1 {
		t.Error("want exactly one succeeded refund, finalized by attempt 2")
	}
	if c := e.count(`select count(*) from payments where status = 'refunded'`); c != 1 {
		t.Error("want one refunded payment")
	}
	if c := e.count(`select count(*) from outbox_messages where template = 'refund_succeeded'`); c != 1 {
		t.Errorf("emails = %d, want 1", c)
	}
	if c := e.count(`select count(*) from sim_gateway_charges s join refunds r on r.provider_ref = s.refund_provider_ref
	                 where s.refund_idempotency_key = r.idempotency_key`); c != 1 {
		t.Error("want the simulated charge refunded once, under the refund's key and ref")
	}
	if len(gw.perKey) != 1 {
		t.Errorf("gateway keys = %v, want the one refund key", gw.perKey)
	}
}

func TestRefunds_NeverTwoLiveRefunds(t *testing.T) {
	e := newFeeEnv(t)
	id, tok := e.paid(arrival1502)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for range 2 {
		wg.Go(func() {
			<-start
			if code, body := e.cancel(tok, id); code != http.StatusOK {
				t.Errorf("cancel = %d %v", code, body)
			}
		})
	}
	close(start)
	wg.Wait()
	if c := e.count(`select count(*) from refunds`); c != 1 {
		t.Errorf("refund rows = %d, want 1", c)
	}
}

// F2: a stale runner's failure must not overwrite the newer attempt's retry.
func TestRefunds_StaleAttemptFinalizeIsFencedOut(t *testing.T) {
	e := newFeeEnv(t)
	e.cancelled()
	// Call 1 (runner A) is held, then fails; call 2 (runner B) fails at once.
	gw := e.simGateway(&refundGateway{failN: 2, holdFst: make(chan struct{}), entered: make(chan struct{})})
	now := e.clk.Now()
	aDone := make(chan struct{})
	go func() {
		defer close(aDone)
		if _, err := e.processor(gw).Run(t.Context(), now); err != nil {
			t.Error(err)
		}
	}()
	<-gw.entered
	bNow := now.Add(61 * time.Second)
	if n, err := e.processor(gw).Run(t.Context(), bNow); err != nil || n != 0 {
		t.Fatalf("runner B = %d, %v", n, err)
	}
	close(gw.holdFst)
	<-aDone
	wait, _ := payments.RefundBackoff(2)
	if c := e.count(`select count(*) from refunds where status = 'pending' and attempts = 2 and next_attempt_at = $1`, bNow.Add(wait)); c != 1 {
		t.Errorf("want attempts 2 and next_attempt_at = B's now + %v, untouched by runner A", wait)
	}
}

// F3: a finalize error after a gateway success counts as a failed attempt.
func TestRefunds_FinalizeErrorsExhaustAndAlert(t *testing.T) {
	e := newFeeEnv(t)
	id, _ := e.cancelled()
	// Break the invariant: the parent payment is no longer 'succeeded'.
	if _, err := e.db.Super.Exec(t.Context(), `update payments set status = 'refunded', refunded_at = $1`, e.clk.Now()); err != nil {
		t.Fatal(err)
	}
	gw := e.simGateway(&refundGateway{})
	p := e.processor(gw)
	now := e.clk.Now()
	for range payments.RefundMaxAttempts + 2 {
		if _, err := p.Run(t.Context(), now); err != nil {
			t.Fatal(err)
		}
		now = now.Add(31 * time.Minute)
	}
	if c := e.count(`select count(*) from refunds where status = 'failed' and attempts = 10 and last_error = 'finalize_failed'`); c != 1 {
		t.Fatal("want the refund failed after 10 attempts with last_error finalize_failed")
	}
	if c := e.count(`select count(*) from reconciliation_alerts where kind = 'refund_failed' and details->>'reason' = 'finalize_failed'
	                 and details->>'reservation_id' = $1 and (select count(*) from jsonb_object_keys(details)) = 4`, id); c != 1 {
		t.Error("want exactly one finalize_failed alert with public ids only")
	}
	if c := e.count(`select count(*) from outbox_messages where template = 'refund_succeeded'`); c != 0 {
		t.Errorf("emails = %d, want none", c)
	}
	if gw.calls != payments.RefundMaxAttempts {
		t.Errorf("gateway calls = %d, want %d (no reclaim after the limit)", gw.calls, payments.RefundMaxAttempts)
	}
	// A pending refund already at the limit is never claimed.
	e.cancelled()
	if _, err := e.db.Super.Exec(t.Context(), `update refunds set attempts = 10 where status = 'pending'`); err != nil {
		t.Fatal(err)
	}
	before := gw.calls
	if n, _ := p.Run(t.Context(), now.Add(time.Hour)); n != 0 || gw.calls != before {
		t.Error("a refund at the attempt limit was claimed")
	}
}

// A runner that died on its last attempt leaves a pending refund at the limit:
// the sweep fails it and opens one alert, with no gateway call and no email.
func TestRefunds_SweepStrandedAtTheLimit(t *testing.T) {
	e := newFeeEnv(t)
	stranded, _ := e.cancelled()
	e.cancelled() // second refund: its lease has not passed
	now := e.clk.Now()
	if _, err := e.db.Super.Exec(t.Context(),
		`update refunds r set attempts = 10, next_attempt_at = case when p.reservation_id = (select id from reservations where public_id = $1)
		                                                      then $2::timestamptz - interval '1 second' else $2::timestamptz + interval '30 seconds' end
		 from payments p where p.id = r.payment_id`, stranded, now); err != nil {
		t.Fatal(err)
	}
	gw := e.simGateway(&refundGateway{})
	p := e.processor(gw)
	for range 2 {
		if n, err := p.Run(t.Context(), now); err != nil || n != 0 {
			t.Fatalf("Run = %d, %v", n, err)
		}
	}
	if c := e.count(`select count(*) from refunds r join payments p on p.id = r.payment_id join reservations v on v.id = p.reservation_id
	                 where v.public_id = $1 and r.status = 'failed' and r.failed_at = $2 and r.attempts = 10`, stranded, now); c != 1 {
		t.Error("want the stranded refund failed at now")
	}
	if c := e.count(`select count(*) from refunds where status = 'pending' and attempts = 10`); c != 1 {
		t.Error("the refund whose lease has not passed must be untouched")
	}
	if c := e.count(`select count(*) from reconciliation_alerts where kind = 'refund_failed' and details->>'reason' = 'max_attempts'
	                 and details->>'reservation_id' = $1 and (select count(*) from jsonb_object_keys(details)) = 4`, stranded); c != 1 {
		t.Error("want exactly one max_attempts alert, even after a second run")
	}
	if c := e.count(`select count(*) from reconciliation_alerts`); c != 1 {
		t.Errorf("alerts = %d, want 1", c)
	}
	if gw.calls != 0 || e.count(`select count(*) from outbox_messages where template = 'refund_succeeded'`) != 0 {
		t.Error("the sweep must not call the gateway or send an email")
	}
}
