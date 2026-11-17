package reservations

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/XYZLZ/MyPark/apps/server/internal/features/notifications"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/payments"
)

// Apply records a gateway result on a fee payment and its reservation. A
// decline only fails the payment. A success is always either honoured
// (confirm) or refunded (late and defensive branches, design 9.4), and
// follows the lock order of design 8.1: lot, reservation, slots, payment,
// refund, outbox. It is idempotent: a payment that is no longer pending is
// left alone, which makes it safe for the pay request and the reconcile job to
// race.
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
		return s.tx.InTx(ctx, func(ctx context.Context) error {
			if err := s.repo.SetTxLimits(ctx); err != nil {
				return err
			}
			return s.applySuccess(ctx, paymentID, res)
		})
	}
	return fmt.Errorf("reservations: apply payment %d: unknown outcome %d", paymentID, res.Outcome)
}

func (s *Service) applySuccess(ctx context.Context, paymentID int64, res payments.ChargeResult) error {
	now := s.clock.Now()
	fee, err := s.fees.FeeByID(ctx, paymentID)
	if err != nil {
		return err
	}
	if fee.Status != payments.StatusPending {
		return nil
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
	if fee.Status != payments.StatusPending {
		return nil
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
		"qr_token": EncodeQR(s.qrKey, rsv.PublicID, nonce),
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
	if err := s.repo.ExpireUnpaidLate(ctx, rsv.ID, payments.RefundFacilityUnavailable, now); err != nil {
		return err
	}
	if _, err := s.repo.EnsureAndLockSlots(ctx, rsv.LotID, rsv.Window); err != nil {
		return err
	}
	if err := s.repo.DecSlots(ctx, rsv.LotID, rsv.Window); err != nil {
		return err
	}
	if err := s.finalizeAndRefund(ctx, fee, res, now); err != nil {
		return err
	}
	return s.enqueue(ctx, rsv, notifications.TplReservationCancelled, map[string]string{
		"reason": payments.RefundFacilityUnavailable,
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
