package reservations

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/XYZLZ/MyPark/apps/server/internal/domain/apperr"
	"github.com/XYZLZ/MyPark/apps/server/internal/domain/errcodes"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/payments"
)

const (
	// defaultChargeTimeout applies when the dependency leaves ChargeTimeout unset.
	defaultChargeTimeout = 5 * time.Second
	// finishTimeout bounds the transaction that records a charge result (M3B-56).
	finishTimeout = 3 * time.Second
)

// PayResult is the outcome of a pay call. Code is the HTTP status: 200 (the
// reservation is paid), 202 (the charge is pending) or 402 (declined). Other
// outcomes are errors.
type PayResult struct {
	Code        int
	Reservation View
	PaymentID   uuid.UUID
	FailureCode string // on 402
	Replayed    bool
}

// Pay charges the reservation fee (design 7.6, 9.3). Transaction 1 locks the
// reservation and records a pending attempt; the gateway is called with no
// transaction open; transaction 2 (Apply) records the result on a context
// detached from the request. A replay of the same key answers from the stored
// attempt and never charges again. The card is only ever held in memory.
func (s *Service) Pay(ctx context.Context, userID int64, id uuid.UUID, key string, req payments.PayRequest) (PayResult, error) {
	card := payments.Card{Number: req.CardNumber, ExpMonth: req.ExpMonth, ExpYear: req.ExpYear, CVC: req.CVC}
	brand, last4 := payments.Brand(card.Number), card.Last4()

	var (
		att      payments.Attempt
		replayed bool
		view     View
		amount   int64
	)
	err := s.tx.InTx(ctx, func(ctx context.Context) error {
		if err := s.repo.SetTxLimits(ctx); err != nil {
			return err
		}
		rec, err := s.repo.Lock(ctx, userID, id)
		if errors.Is(err, ErrNotFound) {
			return apperr.NotFound(errcodes.ReservationNotFound)
		}
		if err != nil {
			return err
		}
		view, amount = rec.View, rec.FeeCents
		fp := payments.Fingerprint(rec.ID, brand, last4, card.ExpMonth, card.ExpYear)
		// The replay comes before the state checks: a paid reservation is
		// confirmed, and the replay of its 200 must not become a 409.
		a, found, err := s.fees.FindAttempt(ctx, rec.RowID, key, fp)
		if err != nil {
			return err
		}
		if found {
			att, replayed = a, true
			return nil
		}
		if rec.Status != StatusPendingPayment {
			return apperr.Conflict(errcodes.ReservationInvalidState).
				WithDetails(map[string]any{"status": string(rec.Status)})
		}
		now := s.clock.Now()
		if !now.Before(rec.PaymentDueAt) {
			return apperr.Conflict(errcodes.ReservationPaymentExpired)
		}
		att, replayed, err = s.fees.StartAttempt(ctx, payments.AttemptInput{
			ReservationID: rec.RowID, UserID: userID, ReservationPublicID: rec.ID, AmountCents: rec.FeeCents,
			ClientKey: key, Fingerprint: fp, Brand: brand, Last4: last4, Now: now,
		})
		return err
	})
	if err != nil {
		return PayResult{}, err
	}
	if replayed {
		res, err := outcome(att, view, true)
		if err != nil {
			err = &ReplayedError{Err: err}
		}
		return res, err
	}

	// No transaction and no pooled connection is held across the charge.
	// A client disconnect must not cancel a charge in flight: that would leave
	// the outcome unknown for a card that may already be charged.
	timeout := s.chargeTimeout
	if timeout <= 0 {
		timeout = defaultChargeTimeout
	}
	cctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeout)
	res, chargeErr := s.gateway.Charge(cctx, payments.ChargeRequest{
		IdempotencyKey: att.GatewayKey, AmountCents: amount, Card: &card,
	})
	cancel()

	// The request context may be gone by now; the result must still be
	// recorded (M3B-56). The reconcile job is the backstop if this fails.
	fctx, stop := context.WithTimeout(context.WithoutCancel(ctx), finishTimeout)
	defer stop()
	if chargeErr != nil {
		// Outcome unknown: write nothing. The attempt stays pending and the
		// reconcile job asks the gateway later.
		s.log.WarnContext(fctx, "reservations: charge outcome unknown", "payment_id", att.PublicID, "err", chargeErr)
	} else if err := s.Apply(fctx, att.ID, res); err != nil {
		return PayResult{}, err
	}
	return s.current(fctx, userID, id, att)
}

// current answers from the stored state of the attempt and the reservation, so
// that a result the reconcile job already applied is reported as it is.
func (s *Service) current(ctx context.Context, userID int64, id uuid.UUID, att payments.Attempt) (PayResult, error) {
	var (
		fee  payments.FeePayment
		view View
	)
	err := s.tx.InTx(ctx, func(ctx context.Context) error {
		if err := s.repo.SetTxLimits(ctx); err != nil {
			return err
		}
		var err error
		if fee, err = s.fees.FeeByID(ctx, att.ID); err != nil {
			return err
		}
		rec, err := s.repo.Get(ctx, userID, id)
		view = rec.View
		if err == nil {
			err = s.withLatestPayment(ctx, rec.RowID, &view)
		}
		return err
	})
	if err != nil {
		return PayResult{}, err
	}
	return outcome(fee.Attempt, view, false)
}

// outcome maps an attempt and its reservation to the pay answer (design 7.6).
// A succeeded payment whose reservation is not paid any more (late success,
// cancelled since) is 409 invalid_state with the status (design 7.6): the
// money is being refunded.
func outcome(att payments.Attempt, view View, replayed bool) (PayResult, error) {
	res := PayResult{Reservation: view, PaymentID: att.PublicID, Replayed: replayed}
	switch att.Status {
	case payments.StatusPending:
		res.Code = http.StatusAccepted
	case payments.StatusFailed:
		res.Code, res.FailureCode = http.StatusPaymentRequired, att.FailureCode
	default:
		switch view.Status {
		case StatusConfirmed, StatusCheckedIn, StatusCompleted:
			res.Code = http.StatusOK
		default:
			return PayResult{}, apperr.Conflict(errcodes.ReservationInvalidState).
				WithDetails(map[string]any{"status": string(view.Status)})
		}
	}
	return res, nil
}

// ReplayedError marks an error answer that replays a stored attempt, so the
// handler still sends Idempotent-Replayed: true with it.
type ReplayedError struct{ Err error }

func (e *ReplayedError) Error() string { return e.Err.Error() }
func (e *ReplayedError) Unwrap() error { return e.Err }
