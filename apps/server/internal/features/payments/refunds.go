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
}

// Run processes the refunds due at now and returns how many it settled as
// succeeded. A refund that cannot be finalized now is logged and left for the
// next run; it never stops the batch.
func (p *RefundProcessor) Run(ctx context.Context, now time.Time) (int, error) {
	var claims []RefundClaim
	err := p.Tx.InTx(ctx, func(ctx context.Context) error {
		var err error
		claims, err = p.Refunds.Claim(ctx, now, now.Add(refundLease), RefundBatch)
		return err
	})
	if err != nil {
		return 0, fmt.Errorf("payments: claim refunds: %w", err)
	}
	settled := 0
	for _, c := range claims {
		if ctx.Err() != nil {
			return settled, ctx.Err()
		}
		ok, err := p.process(ctx, c, now)
		if err != nil {
			p.Log.ErrorContext(ctx, "payments: refund not finalized, retrying after the lease",
				"refund_id", c.PublicID, "payment_id", c.PaymentPublicID, "attempt", c.Attempts, "err", err)
			continue
		}
		if ok {
			settled++
		}
	}
	return settled, nil
}

// process makes the gateway call and finalizes one claim. It reports whether
// the refund succeeded.
func (p *RefundProcessor) process(ctx context.Context, c RefundClaim, now time.Time) (bool, error) {
	cctx, cancel := context.WithTimeout(ctx, refundCallTimeout)
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
	return applied, err
}

// failed records a failed attempt: a retry with backoff, or the end of the
// refund with an alert once the attempt limit is reached.
func (p *RefundProcessor) failed(ctx context.Context, c RefundClaim, class string, now time.Time) error {
	wait, final := RefundBackoff(c.Attempts)
	class = class[:min(len(class), maxLastError)]
	return p.Tx.InTx(ctx, func(ctx context.Context) error {
		if !final {
			_, err := p.Refunds.Retry(ctx, c, class, now.Add(wait))
			return err
		}
		applied, err := p.Refunds.Fail(ctx, c, class, now)
		if err != nil || !applied {
			return err
		}
		return p.Alerts.Open(ctx, c.LotID, alerts.KindRefundFailed, map[string]string{
			"reservation_id": c.ReservationPublicID.String(),
			"refund_id":      c.PublicID.String(),
			"payment_id":     c.PaymentPublicID.String(),
			"reason":         "max_attempts",
		}, now)
	})
}

// errorClass names a gateway failure without echoing the error, which could
// carry request data.
func errorClass(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	return "gateway_error"
}
