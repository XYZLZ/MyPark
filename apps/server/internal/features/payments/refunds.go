package payments

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/XYZLZ/MyPark/apps/server/internal/features/alerts"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/database"
)

const (
	// RefundBatch is how many refunds one run claims (M3B-58).
	RefundBatch = 20
	// refundLease hides a claimed refund from other runners (M3B-29).
	refundLease = 60 * time.Second
	// refundCallTimeout bounds one gateway call, well inside the lease.
	refundCallTimeout = 10 * time.Second
	// maxLastError is the refunds.last_error limit in bytes.
	maxLastError = 500
	// classFinalize is the last_error and alert reason of a finalize error
	// after the gateway had succeeded.
	classFinalize = "finalize_failed"
)

// RefundProcessor sends queued refunds to the gateway (design 12.5, M3B-29).
// A run claims due refunds in a short transaction, calls the gateway with no
// transaction open, then finalizes each one in a transaction fenced on the
// attempt it claimed. A crash or a slow call leaves the lease to expire; the
// next claim calls the gateway again with the same key, which answers with the
// first result, so money is never refunded twice.
type RefundProcessor struct {
	Tx      database.TxRunnerPort
	Refunds Refunds
	Gateway Gateway
	Alerts  alerts.Recorder
	Notice  RefundNotifier
	Log     *slog.Logger

	// Lease and CallTimeout default to 60 s and 10 s; tests shorten them.
	Lease, CallTimeout time.Duration
}

// Run processes the refunds due at now and returns how many it settled as
// succeeded. A refund that cannot be finalized now is logged and left for the
// next run; it never stops the batch. A failed sweep is logged and reported
// with errors.Join at the end, but never stops the claims either.
func (p *RefundProcessor) Run(ctx context.Context, now time.Time) (int, error) {
	lease, callTimeout := p.limits()
	sweepErr := p.sweep(ctx, now)
	if sweepErr != nil {
		sweepErr = fmt.Errorf("payments: sweep stranded refunds: %w", sweepErr)
		p.Log.ErrorContext(ctx, "payments: refund sweep failed, claiming anyway", "err", sweepErr)
	}
	var claims []RefundClaim
	err := p.Tx.InTx(ctx, func(ctx context.Context) error {
		var err error
		claims, err = p.Refunds.Claim(ctx, now, now.Add(lease), RefundBatch)
		return err
	})
	if err != nil {
		return 0, errors.Join(sweepErr, fmt.Errorf("payments: claim refunds: %w", err))
	}
	// A claim is only started while its lease still has room for the call and
	// the finalize; the rest expire and the next tick reclaims them. Calling
	// after the lease ran out would race a second worker and burn attempts.
	start, stop := context.WithTimeout(ctx, lease-2*callTimeout)
	defer stop()
	settled := 0
	for _, c := range claims {
		if ctx.Err() != nil {
			return settled, errors.Join(sweepErr, ctx.Err())
		}
		if start.Err() != nil {
			p.Log.WarnContext(ctx, "payments: refund lease running out, left for the next tick", "refund_id", c.PublicID)
			continue
		}
		ok, err := p.process(ctx, c, now, callTimeout)
		if err != nil {
			p.Log.ErrorContext(ctx, "payments: refund not finalized, retrying after the lease",
				"refund_id", c.PublicID, "payment_id", c.PaymentPublicID, "attempt", c.Attempts, "err", err)
			continue
		}
		if ok {
			settled++
		}
	}
	return settled, sweepErr
}

// sweep ends the refunds a dead runner left at the attempt limit: failed plus
// one alert each, in one transaction, with no gateway call and no email.
func (p *RefundProcessor) sweep(ctx context.Context, now time.Time) error {
	return p.Tx.InTx(ctx, func(ctx context.Context) error {
		stuck, err := p.Refunds.SweepStranded(ctx, now, RefundBatch)
		if err != nil {
			return err
		}
		for _, c := range stuck {
			if err := p.alert(ctx, c, "max_attempts", now); err != nil {
				return err
			}
		}
		return nil
	})
}

// process makes the gateway call and finalizes one claim. It reports whether
// the refund succeeded.
func (p *RefundProcessor) process(ctx context.Context, c RefundClaim, now time.Time, callTimeout time.Duration) (bool, error) {
	cctx, cancel := context.WithTimeout(ctx, callTimeout)
	res, err := p.Gateway.Refund(cctx, RefundRequest{IdempotencyKey: c.GatewayKey, ChargeKey: c.ChargeKey, AmountCents: c.AmountCents})
	cancel()
	if err != nil {
		return false, p.failed(ctx, c, errorClass(err), now)
	}
	var applied bool
	err = p.Tx.InTx(ctx, func(ctx context.Context) error {
		var err error
		if applied, err = p.Refunds.Succeed(ctx, c, res.ProviderRef, now); err != nil || !applied {
			return err
		}
		return p.Notice.RefundSucceeded(ctx, RefundDone{
			RefundPublicID: c.PublicID, ReservationID: c.ReservationID, AmountCents: c.AmountCents,
		})
	})
	if err != nil {
		// The money moved but the books did not: count a failed attempt, so the
		// refund ends in an alert instead of being reclaimed forever.
		if ferr := p.failed(ctx, c, classFinalize, now); ferr != nil {
			return false, errors.Join(err, ferr)
		}
		return false, err
	}
	return applied, nil
}

// failed records a failed attempt: a retry with backoff, or the end of the
// refund with an alert once the attempt limit is reached.
func (p *RefundProcessor) failed(ctx context.Context, c RefundClaim, class string, now time.Time) error {
	wait, final := RefundBackoff(c.Attempts)
	class = class[:min(len(class), maxLastError)]
	reason := "max_attempts"
	if class == classFinalize {
		reason = classFinalize
	}
	return p.Tx.InTx(ctx, func(ctx context.Context) error {
		if !final {
			_, err := p.Refunds.Retry(ctx, c, class, now.Add(wait))
			return err
		}
		applied, err := p.Refunds.Fail(ctx, c, class, now)
		if err != nil || !applied {
			return err
		}
		return p.alert(ctx, c, reason, now)
	})
}

// alert opens the refund_failed alert of a refund that ended failed.
func (p *RefundProcessor) alert(ctx context.Context, c RefundClaim, reason string, now time.Time) error {
	return p.Alerts.Open(ctx, c.LotID, alerts.KindRefundFailed, map[string]string{
		"reservation_id": c.ReservationPublicID.String(),
		"refund_id":      c.PublicID.String(),
		"payment_id":     c.PaymentPublicID.String(),
		"reason":         reason,
	}, now)
}

// limits returns the lease and the call timeout in force.
func (p *RefundProcessor) limits() (lease, call time.Duration) {
	lease, call = p.Lease, p.CallTimeout
	if lease <= 0 {
		lease = refundLease
	}
	if call <= 0 {
		call = refundCallTimeout
	}
	return lease, call
}

// errorClass names a gateway failure without echoing the error, which could
// carry request data.
func errorClass(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	return "gateway_error"
}
