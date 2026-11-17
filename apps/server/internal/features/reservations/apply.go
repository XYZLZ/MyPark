package reservations

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/XYZLZ/MyPark/apps/server/internal/features/alerts"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/notifications"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/payments"
)

// Apply records a gateway result on a fee payment and its reservation. A
// decline only fails the payment. A success is always either honoured
// (confirm) or refunded (late and defensive branches, design 9.4), and
// follows the lock order of design 8.1: lot, reservation, slots, payment,
// refund, outbox. It is idempotent: a success meeting a payment that already
// succeeded or was refunded is a quiet no-op, which makes it safe for the pay
// request and the reconcile job to race. Only a success meeting a failed
// payment is loud (payments.ErrSuccessAfterFinal).
func (s *Service) Apply(ctx context.Context, paymentID int64, res payments.ChargeResult) error {
	switch res.Outcome {
	case payments.ChargeDeclined:
		return s.tx.InTx(ctx, func(ctx context.Context) error {
			if err := s.repo.SetTxLimits(ctx); err != nil {
				return err
			}
			_, err := s.fees.Finalize(ctx, paymentID, res, s.clock.Now())
			return err
		})
	case payments.ChargeSucceeded:
		err := s.tx.InTx(ctx, func(ctx context.Context) error {
			if err := s.repo.SetTxLimits(ctx); err != nil {
				return err
			}
			return s.applySuccess(ctx, paymentID, res)
		})
		if errors.Is(err, payments.ErrSuccessAfterFinal) {
			// The refused apply rolled back, so the alert goes in its own
			// transaction. The error still reaches the caller.
			s.alertUnrefundedCharge(ctx, paymentID)
		}
		return err
	}
	return fmt.Errorf("reservations: apply payment %d: unknown outcome %d", paymentID, res.Outcome)
}

func (s *Service) applySuccess(ctx context.Context, paymentID int64, res payments.ChargeResult) error {
	now := s.clock.Now()
	fee, err := s.fees.FeeByID(ctx, paymentID)
	if err != nil {
		return err
	}
	if fee.Status == payments.StatusSucceeded || fee.Status == payments.StatusRefunded {
		return nil // idempotent re-apply
	}
	// The window never changes, so this unlocked read decides whether the
	// lot is locked before the reservation (design 8.1, M3B-13).
	pre, err := s.repo.ForApply(ctx, fee.ReservationID)
	if err != nil {
		return err
	}
	holdsNow := pre.Window.Contains(now)
	if holdsNow {
		if err := s.repo.LockLot(ctx, pre.LotID); err != nil {
			return err
		}
	}
	rsv, err := s.repo.LockForApply(ctx, fee.ReservationID)
	if err != nil {
		return err
	}
	// Every success finalizes under the reservation lock, so a payment still
	// pending here cannot be finalized by anyone else until this commits.
	if fee, err = s.fees.FeeByID(ctx, paymentID); err != nil {
		return err
	}
	if fee.Status == payments.StatusFailed && fee.FailureCode == payments.FailureNotSubmitted {
		return s.lateCharge(ctx, rsv, fee, res, now)
	}
	if done, err := s.settled(ctx, fee, rsv); done {
		return err
	}

	switch {
	case rsv.Status == StatusPendingPayment && now.Before(rsv.Window.End):
		err = s.confirm(ctx, rsv, fee, res, holdsNow, now)
	case rsv.Status == StatusPendingPayment:
		err = s.lateSuccess(ctx, rsv, fee, res, now)
	default:
		// The guards of M3B-10 make this unreachable, but money must never
		// be kept by accident.
		s.log.ErrorContext(ctx, "reservations: charge succeeded on a reservation that is not payable",
			"reservation_id", rsv.PublicID, "status", string(rsv.Status), "payment_id", fee.PublicID)
		err = s.finalizeAndRefund(ctx, fee, res, now)
	}
	return err
}

// finalize marks the payment succeeded. It must apply: the reservation lock
// and the pending re-read above leave no one else able to finish it.
func (s *Service) finalize(ctx context.Context, fee payments.FeePayment, res payments.ChargeResult, now time.Time) error {
	applied, err := s.fees.Finalize(ctx, fee.ID, res, now)
	if err != nil {
		return err
	}
	if !applied {
		return fmt.Errorf("reservations: payment %d was finalized under the reservation lock", fee.ID)
	}
	return nil
}

func (s *Service) finalizeAndRefund(ctx context.Context, fee payments.FeePayment, res payments.ChargeResult, now time.Time) error {
	if err := s.finalize(ctx, fee, res, now); err != nil {
		return err
	}
	_, err := s.fees.QueueRefund(ctx, fee.ID, fee.AmountCents, payments.RefundFacilityUnavailable, now)
	return err
}

// confirm is design 9.4 step 3.
func (s *Service) confirm(ctx context.Context, rsv ApplyRow, fee payments.FeePayment, res payments.ChargeResult, holdsNow bool, now time.Time) error {
	var nonce [16]byte
	if _, err := io.ReadFull(s.rand, nonce[:]); err != nil {
		return fmt.Errorf("reservations: qr nonce: %w", err)
	}
	if err := s.repo.Confirm(ctx, rsv.ID, nonce, now); err != nil {
		return err
	}
	if err := s.finalize(ctx, fee, res, now); err != nil {
		return err
	}
	if err := s.enqueue(ctx, rsv, notifications.TplReservationConfirmed, map[string]string{
		"reservation_id": rsv.PublicID.String(),
		"arrival_at":     lotTime(rsv.ArrivalAt),
		"window":         lotTime(rsv.Window.Start) + " - " + lotTime(rsv.Window.End),
		"plate_display":  rsv.PlateDisplay,
		"fee_cents":      strconv.FormatInt(rsv.FeeCents, 10),
		"qr_token":       EncodeQR(s.qrKey, rsv.PublicID, nonce),
	}); err != nil {
		return err
	}
	if holdsNow {
		return s.repo.BumpLot(ctx, rsv.LotID, now)
	}
	return nil
}

// lateSuccess is design 9.4 step 4: the money arrived after the hold window
// closed, so the reservation expires and the fee is refunded in full. The
// reservation never held a space at "now", so the lot version stays.
func (s *Service) lateSuccess(ctx context.Context, rsv ApplyRow, fee payments.FeePayment, res payments.ChargeResult, now time.Time) error {
	s.log.WarnContext(ctx, "reservations: late payment success, expiring and refunding",
		"reservation_id", rsv.PublicID, "payment_id", fee.PublicID)
	if err := s.expireUnpaid(ctx, rsv, now); err != nil {
		return err
	}
	if err := s.finalizeAndRefund(ctx, fee, res, now); err != nil {
		return err
	}
	return s.enqueueCancelled(ctx, rsv, refundPending, payments.RefundFacilityUnavailable)
}

// expireUnpaid moves a pending_payment reservation to expired_unpaid and
// releases its slot loads (the reservation row is locked by the caller).
func (s *Service) expireUnpaid(ctx context.Context, rsv ApplyRow, now time.Time) error {
	if err := s.repo.ExpireUnpaidLate(ctx, rsv.ID, payments.RefundFacilityUnavailable, now); err != nil {
		return err
	}
	if _, err := s.repo.EnsureAndLockSlots(ctx, rsv.LotID, rsv.Window); err != nil {
		return err
	}
	return s.repo.DecSlots(ctx, rsv.LotID, rsv.Window)
}

// enqueueCancelled queues the reservation_cancelled email (design 12.6).
// refundStatus is "pending" or "none"; reason is the refund reason that applies
// (cancelled_before_arrival, none_after_arrival or facility_unavailable).
func (s *Service) enqueueCancelled(ctx context.Context, rsv ApplyRow, refundStatus, reason string) error {
	return s.enqueue(ctx, rsv, notifications.TplReservationCancelled, map[string]string{
		"reservation_id": rsv.PublicID.String(),
		"refund_status":  refundStatus,
		"refund_reason":  reason,
		"fee_cents":      strconv.FormatInt(rsv.FeeCents, 10),
	})
}

// enqueue writes a reservation email to the outbox, deduplicated per
// template and reservation.
func (s *Service) enqueue(ctx context.Context, rsv ApplyRow, tpl notifications.Template, data map[string]string) error {
	c, err := s.repo.UserContact(ctx, rsv.UserID)
	if err != nil {
		return err
	}
	uid := rsv.UserID
	return s.notify.Enqueue(ctx, notifications.Email{
		Template: tpl, Locale: c.Locale, To: c.Email, UserID: &uid, Data: data,
		DedupeKey: string(tpl) + ":" + rsv.PublicID.String(),
	})
}

// settled reports whether a success has nothing left to apply. A payment that
// already succeeded or was refunded is an idempotent re-apply (quiet nil): the
// charge was honoured once, and a refund follows a success by design. One that
// failed meets a success it can no longer honour: that is logged at error
// (public ids only) and returned as payments.ErrSuccessAfterFinal.
func (s *Service) settled(ctx context.Context, fee payments.FeePayment, rsv ApplyRow) (bool, error) {
	switch fee.Status {
	case payments.StatusPending:
		return false, nil
	case payments.StatusSucceeded, payments.StatusRefunded:
		return true, nil
	}
	s.log.ErrorContext(ctx, "reservations: charge succeeded on a payment that is already final",
		"payment_id", fee.PublicID, "payment_status", fee.Status, "reservation_id", rsv.PublicID)
	return true, payments.ErrSuccessAfterFinal
}

// lotZone is the lot's civil time (America/Santo_Domingo, no DST): the emails
// show times the driver can read off a clock in the city.
var lotZone = time.FixedZone("America/Santo_Domingo", -4*60*60)

func lotTime(t time.Time) string { return t.In(lotZone).Format("2006-01-02 15:04") }

// lateCharge handles a success for a payment the reconcile job had marked
// not_submitted: the pay request stalled past the cutoff and its charge landed
// afterwards (design 9.4 step 5, money is never kept by accident). The payment
// becomes succeeded and is refunded in full. A reservation still pending_payment
// is expired through the late-success branch (the succeeded fee would otherwise
// block a new attempt on the live-fee index); any other status is left alone,
// as the driver may have paid again with a new key. It runs under
// the reservation lock, in the lock order of design 8.1.
func (s *Service) lateCharge(ctx context.Context, rsv ApplyRow, fee payments.FeePayment, res payments.ChargeResult, now time.Time) error {
	// The unique live-fee index forbids a second live attempt; a driver who
	// paid again holds it, so this row cannot become succeeded. Stay loud.
	if _, live, err := s.fees.LiveFee(ctx, fee.ReservationID); err != nil {
		return err
	} else if live {
		s.log.ErrorContext(ctx, "reservations: late charge on a not_submitted payment, another fee is live",
			"payment_id", fee.PublicID, "reservation_id", rsv.PublicID)
		return payments.ErrSuccessAfterFinal
	}
	// Reservation and slots come before the payment (lock order, design 8.1).
	expire := rsv.Status == StatusPendingPayment
	if expire {
		if err := s.expireUnpaid(ctx, rsv, now); err != nil {
			return err
		}
	}
	ok, err := s.fees.MarkLateSuccess(ctx, fee.ID, res, now)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("reservations: payment %d changed under the reservation lock", fee.ID)
	}
	s.log.WarnContext(ctx, "reservations: charge landed after the payment was marked not_submitted, refunding",
		"payment_id", fee.PublicID, "reservation_id", rsv.PublicID)
	if _, err = s.fees.QueueRefund(ctx, fee.ID, fee.AmountCents, payments.RefundFacilityUnavailable, now); err != nil || !expire {
		return err
	}
	return s.enqueueCancelled(ctx, rsv, refundPending, payments.RefundFacilityUnavailable)
}

// refundPending and refundNone are the refund_status values of the
// reservation_cancelled email.
const (
	refundPending = "pending"
	refundNone    = "none"
)

// Reasons of the alert for a charge no refund covers (public codes, M4 shows them).
const (
	alertDuplicateCharge     = "duplicate_charge"     // another live fee of the reservation succeeded
	alertOrphanCharge        = "orphan_charge"        // another live fee is still pending
	alertGatewayInconsistent = "gateway_inconsistent" // a declined payment later reported success
)

// alertUnrefundedCharge opens an admin alert for a charge the gateway took that
// no refund covers: a success meeting a payment that cannot become succeeded
// (payments.ErrSuccessAfterFinal). The row holds public ids and short codes
// only. It runs on its own budget, detached from the request: the refused
// apply may have used up the context it came with, and the alert is what makes
// the loss visible. A failure here is logged, never returned: the caller
// already has the loud error.
func (s *Service) alertUnrefundedCharge(ctx context.Context, paymentID int64) {
	if s.alerts == nil {
		return
	}
	actx, stop := context.WithTimeout(context.WithoutCancel(ctx), finishTimeout)
	defer stop()
	var publicID uuid.UUID
	err := s.tx.InTx(actx, func(ctx context.Context) error {
		if err := s.repo.SetTxLimits(ctx); err != nil {
			return err
		}
		fee, err := s.fees.FeeByID(ctx, paymentID)
		if err != nil {
			return err
		}
		publicID = fee.PublicID
		rsv, err := s.repo.ForApply(ctx, fee.ReservationID)
		if err != nil {
			return err
		}
		details := map[string]string{
			"reservation_id": rsv.PublicID.String(),
			"payment_id":     fee.PublicID.String(),
		}
		live, found, err := s.fees.LiveFee(ctx, fee.ReservationID)
		if err != nil {
			return err
		}
		details["reason"] = alertReason(fee, live, found)
		if found {
			details["live_fee_status"] = live.Status
		}
		return s.alerts.Open(ctx, rsv.LotID, alerts.KindRefundFailed, details, s.clock.Now())
	})
	if err != nil {
		s.log.ErrorContext(actx, "reservations: could not record the unrefunded charge alert", "payment_id", publicID, "err", err)
	}
}

// alertReason names why a success could not be honoured. A not_submitted
// payment lost a race with its own late charge: next to a succeeded or
// refunded live fee that is a duplicate, otherwise (the live fee is pending,
// or stopped being live after the refused apply) it is an orphan. Any other
// payment is a gateway that contradicts itself.
func alertReason(fee, live payments.FeePayment, liveFound bool) string {
	if fee.Status != payments.StatusFailed || fee.FailureCode != payments.FailureNotSubmitted {
		return alertGatewayInconsistent
	}
	if liveFound && live.Status != payments.StatusPending {
		return alertDuplicateCharge
	}
	return alertOrphanCharge
}
