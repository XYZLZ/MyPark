package payments_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/XYZLZ/MyPark/apps/server/internal/features/alerts"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/payments"
)

type fakeRefunds struct {
	claims  []payments.RefundClaim
	applied bool // what the fenced finalizers report
	retried []string
	retryAt time.Time
	failed  []string
	leased  time.Time
	limit   int
}

func (f *fakeRefunds) Claim(_ context.Context, _, lease time.Time, limit int) ([]payments.RefundClaim, error) {
	f.leased, f.limit = lease, limit
	return f.claims, nil
}

func (f *fakeRefunds) Succeed(context.Context, payments.RefundClaim, string, time.Time) (bool, error) {
	return f.applied, nil
}

func (f *fakeRefunds) Retry(_ context.Context, _ payments.RefundClaim, msg string, at time.Time) (bool, error) {
	f.retried, f.retryAt = append(f.retried, msg), at
	return f.applied, nil
}

func (f *fakeRefunds) Fail(_ context.Context, _ payments.RefundClaim, msg string, _ time.Time) (bool, error) {
	f.failed = append(f.failed, msg)
	return f.applied, nil
}

type refundGW struct {
	payments.Gateway
	err error
}

func (g refundGW) Refund(context.Context, payments.RefundRequest) (payments.RefundResult, error) {
	return payments.RefundResult{ProviderRef: "ref"}, g.err
}

type countNotice struct{ n int }

func (c *countNotice) RefundSucceeded(context.Context, payments.RefundDone) error { c.n++; return nil }

type countAlerts struct{ n int }

func (c *countAlerts) Open(context.Context, int64, alerts.Kind, map[string]string, time.Time) error {
	c.n++
	return nil
}

func processor(f *fakeRefunds, gw payments.Gateway, n *countNotice, a *countAlerts) *payments.RefundProcessor {
	return &payments.RefundProcessor{
		Tx: passTx{}, Refunds: f, Gateway: gw, Alerts: a, Notice: n, Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

func claim(attempts int) payments.RefundClaim {
	return payments.RefundClaim{ID: 1, PublicID: uuid.New(), Attempts: attempts, AmountCents: 15000}
}

func TestRefunds_ClaimsWithBatchAndLease(t *testing.T) {
	f := &fakeRefunds{}
	if n, err := processor(f, refundGW{}, &countNotice{}, &countAlerts{}).Run(t.Context(), nowT); n != 0 || err != nil {
		t.Fatalf("Run = %d, %v", n, err)
	}
	if f.limit != 20 || !f.leased.Equal(nowT.Add(60*time.Second)) {
		t.Errorf("claimed limit %d lease %v, want 20 and now+60s", f.limit, f.leased)
	}
}

func TestRefunds_FailureRecordsOnlyTheClass(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want string
	}{
		{context.DeadlineExceeded, "timeout"},
		{errors.New("card 4242424242424242 refused"), "gateway_error"},
	} {
		f := &fakeRefunds{claims: []payments.RefundClaim{claim(3)}, applied: true}
		if n, _ := processor(f, refundGW{err: tc.err}, &countNotice{}, &countAlerts{}).Run(t.Context(), nowT); n != 0 {
			t.Errorf("a failed refund counted as settled")
		}
		if len(f.retried) != 1 || f.retried[0] != tc.want || !f.retryAt.Equal(nowT.Add(40*time.Second)) {
			t.Errorf("retry = %v at %v, want %q at now+40s", f.retried, f.retryAt, tc.want)
		}
	}
}

func TestRefunds_LastAttemptFailsAndAlertsOnce(t *testing.T) {
	f, a := &fakeRefunds{claims: []payments.RefundClaim{claim(10)}, applied: true}, &countAlerts{}
	processor(f, refundGW{err: context.DeadlineExceeded}, &countNotice{}, a).Run(t.Context(), nowT) //nolint:errcheck // asserted on the fakes
	if len(f.failed) != 1 || len(f.retried) != 0 || a.n != 1 {
		t.Errorf("failed %v retried %v alerts %d, want one failure, one alert", f.failed, f.retried, a.n)
	}
	// A fence that no longer matches opens no alert.
	f, a = &fakeRefunds{claims: []payments.RefundClaim{claim(10)}}, &countAlerts{}
	processor(f, refundGW{err: context.DeadlineExceeded}, &countNotice{}, a).Run(t.Context(), nowT) //nolint:errcheck // asserted on the fakes
	if a.n != 0 {
		t.Errorf("a stale failure opened %d alerts", a.n)
	}
}

func TestRefunds_StaleSuccessSendsNoEmail(t *testing.T) {
	f, n := &fakeRefunds{claims: []payments.RefundClaim{claim(2)}}, &countNotice{}
	if got, _ := processor(f, refundGW{}, n, &countAlerts{}).Run(t.Context(), nowT); got != 0 || n.n != 0 {
		t.Errorf("settled %d, emails %d, want none when the fence did not match", got, n.n)
	}
	f.applied = true
	if got, _ := processor(f, refundGW{}, n, &countAlerts{}).Run(t.Context(), nowT); got != 1 || n.n != 1 {
		t.Errorf("settled %d, emails %d, want 1 and 1", got, n.n)
	}
}

// slowGW answers after a real delay and counts its calls.
type slowGW struct {
	payments.Gateway
	delay time.Duration
	calls int
}

func (g *slowGW) Refund(ctx context.Context, _ payments.RefundRequest) (payments.RefundResult, error) {
	g.calls++
	select {
	case <-time.After(g.delay):
	case <-ctx.Done():
		return payments.RefundResult{}, ctx.Err()
	}
	return payments.RefundResult{ProviderRef: "ref"}, nil
}

// countingRefunds counts the successes finalized.
type countingRefunds struct {
	fakeRefunds
	succeeded int
}

func (c *countingRefunds) Succeed(context.Context, payments.RefundClaim, string, time.Time) (bool, error) {
	c.succeeded++
	return true, nil
}

// F1: a claim whose lease may have run out is never called nor finalized.
func TestRefunds_LateClaimsAreLeftToExpire(t *testing.T) {
	f := &countingRefunds{fakeRefunds: fakeRefunds{claims: []payments.RefundClaim{claim(1), claim(1), claim(1), claim(1), claim(1)}}}
	gw := &slowGW{delay: 250 * time.Millisecond}
	p := processor(&f.fakeRefunds, gw, &countNotice{}, &countAlerts{})
	p.Refunds = f
	p.Lease, p.CallTimeout = 1200*time.Millisecond, 300*time.Millisecond // calls may start within the first 600 ms
	n, err := p.Run(t.Context(), nowT)
	if err != nil {
		t.Fatal(err)
	}
	if gw.calls != 3 || f.succeeded != 3 || n != 3 {
		t.Errorf("gateway calls %d, finalized %d, settled %d; want 3 each (the last two claims expire unprocessed)", gw.calls, f.succeeded, n)
	}
	if len(f.retried)+len(f.failed) != 0 {
		t.Error("an unprocessed claim must not be finalized as a failure")
	}
	if !f.leased.Equal(nowT.Add(1200*time.Millisecond)) {
		t.Errorf("lease = %v, want now + Lease", f.leased)
	}
}
