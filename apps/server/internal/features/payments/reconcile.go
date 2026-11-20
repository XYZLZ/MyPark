package payments

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/database"
)

const (
	// ReconcileBatch is how many pending attempts one run handles (M3B-58).
	ReconcileBatch = 20
	// reconcileMargin is added to the charge timeout before an attempt is
	// considered abandoned by its pay request (M3B-10).
	reconcileMargin = 10 * time.Second
	// defaultChargeTimeout applies when ChargeTimeout is unset.
	defaultChargeTimeout = 5 * time.Second
)

// Reconciler finishes fee attempts whose pay request never recorded a result
// (design 9.4, 12.1): it asks the gateway by key, then applies the answer.
type Reconciler struct {
	Tx            database.TxRunnerPort
	Fees          Fees
	Gateway       Gateway
	Outcomes      OutcomeApplier
	ChargeTimeout time.Duration // PAYMENT_CHARGE_TIMEOUT
	Log           *slog.Logger

	// Batch defaults to ReconcileBatch; tests shorten it.
	Batch int
}

// Run reconciles up to Batch (ReconcileBatch) pending fee payments created before
// now - (ChargeTimeout + 10 s) and returns how many it settled. Each row costs
// one gateway call, made with no transaction open and bounded by ChargeTimeout.
// A row that cannot be settled now (the gateway failed, or Apply refused) is
// logged and left for the next run; it never stops the batch.
func (r *Reconciler) Run(ctx context.Context, now time.Time) (int, error) {
	timeout := r.ChargeTimeout
	if timeout <= 0 {
		timeout = defaultChargeTimeout
	}
	cutoff := now.Add(-(timeout + reconcileMargin))
	batch := r.Batch
	if batch <= 0 {
		batch = ReconcileBatch
	}
	rows, err := r.Fees.PendingFeesBefore(ctx, cutoff, batch)
	if err != nil {
		return 0, err
	}
	settled := 0
	for _, fee := range rows {
		if ctx.Err() != nil {
			return settled, ctx.Err()
		}
		if r.settle(ctx, fee, timeout, cutoff, now) {
			settled++
		}
	}
	return settled, nil
}

// settle reconciles one payment and reports whether it was settled.
func (r *Reconciler) settle(ctx context.Context, fee FeePayment, timeout time.Duration, cutoff, now time.Time) bool {
	cctx, cancel := context.WithTimeout(ctx, timeout)
	res, err := r.Gateway.Charge(cctx, ChargeRequest{IdempotencyKey: fee.GatewayKey, AmountCents: fee.AmountCents})
	cancel()
	switch {
	case errors.Is(err, ErrChargeUnknown):
		// The gateway never saw the key, and the attempt is older than any
		// charge in flight: nothing was taken. MarkNotSubmitted takes the
		// reservation lock and re-checks the age.
		var marked bool
		err := r.Tx.InTx(ctx, func(ctx context.Context) error {
			var err error
			marked, err = r.Fees.MarkNotSubmitted(ctx, fee.ID, cutoff, now)
			return err
		})
		if err != nil {
			r.Log.WarnContext(ctx, "payments: reconcile: mark not submitted failed", "payment_id", fee.PublicID, "err", err)
			return false
		}
		return marked
	case err != nil:
		r.Log.WarnContext(ctx, "payments: reconcile: gateway unavailable, retrying next tick", "payment_id", fee.PublicID, "err", err)
		return false
	}
	// Apply runs its own transaction (a nested one reuses the outer).
	if err := r.Outcomes.Apply(ctx, fee.ID, res); err != nil {
		r.Log.ErrorContext(ctx, "payments: reconcile: apply failed", "payment_id", fee.PublicID, "err", err)
		r.alertApplyFailed(ctx, fee, res, err, now)
		return false
	}
	return true
}

// alertApplyFailed opens the refund_failed alert (reason apply_failed) of a
// gateway success that Apply refused: the money was taken and nothing records
// it. The row is retried every run; the alert is opened once per payment, and
// the work list puts the alerted row last so it cannot starve younger ones.
// A success meeting a final payment already alerts inside Apply.
func (r *Reconciler) alertApplyFailed(ctx context.Context, fee FeePayment, res ChargeResult, applyErr error, now time.Time) {
	if res.Outcome != ChargeSucceeded || errors.Is(applyErr, ErrSuccessAfterFinal) {
		return
	}
	var opened bool
	err := r.Tx.InTx(ctx, func(ctx context.Context) error {
		var err error
		opened, err = r.Fees.AlertApplyFailed(ctx, fee.ID, now)
		return err
	})
	switch {
	case err != nil:
		r.Log.ErrorContext(ctx, "payments: reconcile: apply_failed alert not recorded", "payment_id", fee.PublicID, "err", err)
	case opened:
		r.Log.ErrorContext(ctx, "payments: reconcile: charge taken but not applied, alert opened", "payment_id", fee.PublicID)
	}
}
