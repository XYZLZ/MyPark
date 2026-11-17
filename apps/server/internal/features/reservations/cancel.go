package reservations

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/XYZLZ/MyPark/apps/server/internal/domain/apperr"
	"github.com/XYZLZ/MyPark/apps/server/internal/domain/errcodes"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/payments"
)

// cancelledByDriver is the cancelled_by value of a driver cancel.
const cancelledByDriver = "driver"

// Cancel cancels the driver's reservation (FR-RES-11, design 7.8, M3B-34).
// Cancelling twice is a 200 with the stored state. It never acts while a fee
// payment is pending (M3B-10). When the hold window contains now it takes the
// lot lock first, whatever the status read before locking says: a concurrent
// confirm takes the same lock first (design 8.1), so the order cannot invert.
// Then it locks the reservation and its slots, reads the payment, and inserts
// the refund and the email.
func (s *Service) Cancel(ctx context.Context, userID int64, id uuid.UUID) (View, error) {
	var out View
	err := s.tx.InTx(ctx, func(ctx context.Context) error {
		if err := s.repo.SetTxLimits(ctx); err != nil {
			return err
		}
		// Unlocked reads decide the lot lock; the window never changes. This
		// early time serves only that decision.
		pre, err := s.repo.Get(ctx, userID, id)
		if errors.Is(err, ErrNotFound) {
			return apperr.NotFound(errcodes.ReservationNotFound)
		}
		if err != nil {
			return err
		}
		ap, err := s.repo.ForApply(ctx, pre.RowID)
		if err != nil {
			return err
		}
		inWindow := ap.Window.Contains(s.clock.Now())
		if inWindow {
			if err := s.repo.LockLot(ctx, ap.LotID); err != nil {
				return err
			}
		}
		rec, err := s.repo.Lock(ctx, userID, id)
		if err != nil {
			return err
		}
		// The refund decision and the timestamps use the time the locks were
		// won: a cancel that waited across arrival_at must not be refunded.
		now := s.clock.Now()
		switch rec.Status {
		case StatusPendingPayment, StatusConfirmed:
			if err := s.cancelLive(ctx, rec, ap, inWindow, now); err != nil {
				return err
			}
		case StatusCancelled:
			if rec.CancelledBy == nil || *rec.CancelledBy != cancelledByDriver {
				return invalidState(rec.Status)
			}
		default:
			return invalidState(rec.Status)
		}
		stored, err := s.repo.Get(ctx, userID, id)
		if err != nil {
			return err
		}
		out = stored.View
		return s.withLatestPayment(ctx, stored.RowID, &out)
	})
	return out, err
}

func invalidState(st Status) error {
	return apperr.Conflict(errcodes.ReservationInvalidState).WithDetails(map[string]any{"status": string(st)})
}

// cancelLive cancels a locked pending_payment or confirmed reservation.
// rsv carries the lot and window; inWindow means the lot lock is held.
func (s *Service) cancelLive(ctx context.Context, rec Record, rsv ApplyRow, inWindow bool, now time.Time) error {
	fee, live, err := s.fees.LiveFee(ctx, rec.RowID)
	if err != nil {
		return err
	}
	confirmed := rec.Status == StatusConfirmed
	switch {
	case !confirmed && live && fee.Status == payments.StatusPending:
		return apperr.Conflict(errcodes.PaymentInProgress)
	case !confirmed && live:
		return fmt.Errorf("reservations: pending_payment reservation %s has a %s fee", rec.ID, fee.Status)
	case confirmed && (!live || fee.Status != payments.StatusSucceeded):
		return fmt.Errorf("reservations: confirmed reservation %s has no succeeded fee", rec.ID)
	}

	refundReason, refund := "", false
	if confirmed {
		refund = now.Before(rec.ArrivalAt)
		refundReason = payments.ReasonNoneAfterArrival
		if refund {
			refundReason = payments.RefundCancelledBeforeArrival
		}
	}
	if err := s.repo.Cancel(ctx, rec.RowID, cancelledByDriver, refundReason, now); err != nil {
		return err
	}
	if _, err := s.repo.EnsureAndLockSlots(ctx, rsv.LotID, rec.Window); err != nil {
		return err
	}
	if err := s.repo.DecSlots(ctx, rsv.LotID, rec.Window); err != nil {
		return err
	}
	if !confirmed {
		return nil
	}
	status := refundNone
	if refund {
		status = refundPending
		if _, err := s.fees.QueueRefund(ctx, fee.ID, fee.AmountCents, payments.RefundCancelledBeforeArrival, now); err != nil {
			return err
		}
	}
	rsv.FeeCents = rec.FeeCents
	if err := s.enqueueCancelled(ctx, rsv, status, refundReason); err != nil {
		return err
	}
	if inWindow {
		return s.repo.BumpLot(ctx, rsv.LotID, now)
	}
	return nil
}
