package payments_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/XYZLZ/MyPark/apps/server/internal/features/payments"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/database"
)

var nowT = time.Date(2026, 12, 1, 14, 0, 0, 0, time.UTC)

// fakeFees is a payments.Fees over a slice; only what the reconciler calls works.
type fakeFees struct {
	payments.Fees
	pending   []payments.FeePayment
	cutoff    time.Time
	limit     int
	marked    []int64
	markedNow time.Time
}

func (f *fakeFees) PendingFeesBefore(_ context.Context, cutoff time.Time, limit int) ([]payments.FeePayment, error) {
	f.cutoff, f.limit = cutoff, limit
	return f.pending, nil
}

func (f *fakeFees) MarkNotSubmitted(_ context.Context, id int64, cutoff, now time.Time) (bool, error) {
	f.marked, f.markedNow = append(f.marked, id), now
	return true, nil
}

type fakeGateway struct {
	payments.Gateway
	byKey    map[string]error
	requests []payments.ChargeRequest
	deadline time.Duration
}

func (g *fakeGateway) Charge(ctx context.Context, req payments.ChargeRequest) (payments.ChargeResult, error) {
	g.requests = append(g.requests, req)
	if d, ok := ctx.Deadline(); ok {
		g.deadline = time.Until(d)
	}
	if err := g.byKey[req.IdempotencyKey]; err != nil {
		return payments.ChargeResult{}, err
	}
	return payments.ChargeResult{Outcome: payments.ChargeSucceeded, ProviderRef: "ref"}, nil
}

type fakeApplier struct {
	err     map[int64]error
	applied []int64
}

func (a *fakeApplier) Apply(_ context.Context, id int64, _ payments.ChargeResult) error {
	if err := a.err[id]; err != nil {
		return err
	}
	a.applied = append(a.applied, id)
	return nil
}

type passTx struct{}

func (passTx) InTx(ctx context.Context, fn func(context.Context) error) error { return fn(ctx) }

var _ database.TxRunnerPort = passTx{}

func row(id int64) payments.FeePayment {
	pub := uuid.New()
	return payments.FeePayment{Attempt: payments.Attempt{ID: id, PublicID: pub, Status: payments.StatusPending, GatewayKey: payments.GatewayKey(pub)}, AmountCents: 15000}
}

func TestReconciler_Run(t *testing.T) {
	ok, unknown, transport, final := row(1), row(2), row(3), row(4)
	fees := &fakeFees{pending: []payments.FeePayment{ok, unknown, transport, final}}
	gw := &fakeGateway{byKey: map[string]error{
		unknown.GatewayKey: payments.ErrChargeUnknown, transport.GatewayKey: context.DeadlineExceeded,
	}}
	app := &fakeApplier{err: map[int64]error{4: payments.ErrSuccessAfterFinal}}
	var logs strings.Builder
	r := &payments.Reconciler{
		Tx: passTx{}, Fees: fees, Gateway: gw, Outcomes: app, ChargeTimeout: 5 * time.Second,
		Log: slog.New(slog.NewTextHandler(&logs, nil)),
	}
	n, err := r.Run(t.Context(), nowT)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 { // one applied, one marked not submitted; the transport error and the loud apply are skipped
		t.Errorf("Run = %d, want 2", n)
	}
	if want := nowT.Add(-15 * time.Second); !fees.cutoff.Equal(want) || fees.limit != 20 {
		t.Errorf("selected with cutoff %v limit %d, want %v and 20", fees.cutoff, fees.limit, want)
	}
	if len(app.applied) != 1 || app.applied[0] != 1 || len(fees.marked) != 1 || fees.marked[0] != 2 || !fees.markedNow.Equal(nowT) {
		t.Errorf("applied %v, marked %v at %v", app.applied, fees.marked, fees.markedNow)
	}
	for _, req := range gw.requests {
		if req.Card != nil {
			t.Error("a reconcile charge must be a key-only re-query")
		}
	}
	if gw.deadline <= 0 || gw.deadline > 5*time.Second {
		t.Errorf("charge deadline = %v, want within ChargeTimeout", gw.deadline)
	}
	if !strings.Contains(logs.String(), transport.PublicID.String()) || !strings.Contains(logs.String(), final.PublicID.String()) {
		t.Errorf("skipped rows must be logged with public ids: %s", logs.String())
	}
}

func TestReconciler_UnsetTimeoutDefaults(t *testing.T) {
	fees := &fakeFees{}
	r := &payments.Reconciler{Tx: passTx{}, Fees: fees, Gateway: &fakeGateway{}, Outcomes: &fakeApplier{}, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	if _, err := r.Run(t.Context(), nowT); err != nil {
		t.Fatal(err)
	}
	if want := nowT.Add(-15 * time.Second); !fees.cutoff.Equal(want) {
		t.Errorf("cutoff = %v, want %v (default 5 s timeout + 10 s)", fees.cutoff, want)
	}
}

func TestReconciler_SelectFailureIsAnError(t *testing.T) {
	r := &payments.Reconciler{Tx: passTx{}, Fees: errFees{}, Gateway: &fakeGateway{}, Outcomes: &fakeApplier{}, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	if _, err := r.Run(t.Context(), nowT); err == nil {
		t.Error("want the select error")
	}
}

type errFees struct{ payments.Fees }

func (errFees) PendingFeesBefore(context.Context, time.Time, int) ([]payments.FeePayment, error) {
	return nil, errors.New("db down")
}
