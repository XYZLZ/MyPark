package payments

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// Attempt is one fee payment as the pay flow sees it.
type Attempt struct {
	ID          int64
	PublicID    uuid.UUID
	Status      string // pending | succeeded | failed | refunded
	FailureCode string // set when failed
	GatewayKey  string // the gateway idempotency key, "rsv-fee:<public id>"
}

// FeePayment is an Attempt with the reservation it pays and its amount.
type FeePayment struct {
	Attempt
	ReservationID int64
	AmountCents   int64
}

// AttemptInput describes a new fee attempt. Brand and Last4 are the only card
// data ever stored (FR-PAY-9); Fingerprint binds the client key to the request.
type AttemptInput struct {
	ReservationID, UserID int64
	ReservationPublicID   uuid.UUID
	AmountCents           int64
	ClientKey             string
	Fingerprint           []byte
	Brand, Last4          string
	Now                   time.Time
}

// Refund is a queued refund.
type Refund struct {
	ID          int64
	PublicID    uuid.UUID
	PaymentID   int64
	AmountCents int64
	Reason      string
	Status      string
	// SucceededAt is set once the gateway returned the money.
	SucceededAt *time.Time
}

// Fees is the storage port of reservation-fee payments. Every method runs on
// the transaction carried by the context; the pay flow, Apply and cancel own
// the lock order (reservations before payments before refunds).
type Fees interface {
	// FindAttempt looks the (reservation, client key) pair up. A match with
	// the same fingerprint is a replay (found true); one with another
	// fingerprint is 409 idempotency.key_reused.
	FindAttempt(ctx context.Context, reservationID int64, clientKey string, fingerprint []byte) (Attempt, bool, error)
	// StartAttempt inserts a pending attempt. The unique indexes decide the
	// races: the same client key replays the stored attempt, another live
	// attempt is 409 payment.in_progress.
	StartAttempt(ctx context.Context, in AttemptInput) (a Attempt, replayed bool, err error)
	// FeeByID is an unlocked read; reservation_id never changes.
	FeeByID(ctx context.Context, paymentID int64) (FeePayment, error)
	// Finalize applies a gateway result to a pending payment: succeeded or
	// failed. It is fenced on status = 'pending' and reports whether it applied.
	Finalize(ctx context.Context, paymentID int64, res ChargeResult, now time.Time) (applied bool, err error)
	// PendingFeesBefore lists up to limit pending fee payments created before
	// cutoff, oldest first. It is an unlocked read: the reconcile job calls
	// the gateway with no transaction open.
	PendingFeesBefore(ctx context.Context, cutoff time.Time, limit int) ([]FeePayment, error)
	// MarkNotSubmitted fails a pending payment the gateway never saw. It acts
	// only on an attempt created before cutoff (M3B-10: a younger one may still
	// have a charge in flight) and must run in a transaction, where it takes the
	// reservation lock before touching the payment (design 8.1).
	MarkNotSubmitted(ctx context.Context, paymentID int64, cutoff, now time.Time) (bool, error)
	// MarkLateSuccess turns a failed not_submitted payment into succeeded: its
	// charge reached the gateway after all. Fenced on that exact state.
	MarkLateSuccess(ctx context.Context, paymentID int64, res ChargeResult, now time.Time) (bool, error)
	// LiveFee is the pending, succeeded or refunded fee of the reservation.
	LiveFee(ctx context.Context, reservationID int64) (FeePayment, bool, error)
	// LatestFee is the newest fee attempt of the reservation, whatever its status.
	LatestFee(ctx context.Context, reservationID int64) (FeePayment, bool, error)
	// QueueRefund inserts a pending full refund of the payment.
	QueueRefund(ctx context.Context, paymentID, amountCents int64, reason string, now time.Time) (Refund, error)
	// LatestRefund is the newest refund of the payment, if any.
	LatestRefund(ctx context.Context, paymentID int64) (Refund, bool, error)
}

// OutcomeApplier applies a gateway result to a payment and its reservation. The
// reservations service implements it; the pay flow and the reconcile job call it.
type OutcomeApplier interface {
	Apply(ctx context.Context, paymentID int64, res ChargeResult) error
}

// RefundClaim is a pending refund leased to one runner (design 12.5). Attempts
// is the attempt number the claim took; finalizing is fenced on it.
type RefundClaim struct {
	ID          int64
	PublicID    uuid.UUID
	PaymentID   int64
	AmountCents int64
	Attempts    int
	// GatewayKey is the refund's idempotency key, ChargeKey the charge's.
	GatewayKey, ChargeKey string
	// Public ids and the lot, for the alert and the log.
	PaymentPublicID, ReservationPublicID uuid.UUID
	ReservationID, LotID                 int64
}

// Refunds is the storage port of the refund job. Every method runs on the
// transaction carried by the context.
type Refunds interface {
	// Claim leases up to limit refunds due at now: pending, next_attempt_at <= now,
	// skipping rows another runner holds. Each claim takes the next attempt number
	// and a lease until leaseUntil, so no other runner is handed it meanwhile.
	Claim(ctx context.Context, now, leaseUntil time.Time, limit int) ([]RefundClaim, error)
	// Succeed finishes a refund: the refund succeeds and its payment goes
	// succeeded -> refunded. It is fenced on (pending, attempts) and reports
	// whether it applied; false changes nothing.
	Succeed(ctx context.Context, c RefundClaim, providerRef string, now time.Time) (bool, error)
	// Retry records a failed attempt and schedules the next one, fenced as Succeed.
	Retry(ctx context.Context, c RefundClaim, lastError string, nextAttempt time.Time) (bool, error)
	// Fail ends a refund for good (attempt limit reached), fenced as Succeed.
	Fail(ctx context.Context, c RefundClaim, lastError string, now time.Time) (bool, error)
}

// RefundDone describes a refund that just succeeded.
type RefundDone struct {
	RefundPublicID uuid.UUID
	ReservationID  int64
	AmountCents    int64
}

// RefundNotifier tells the driver a refund went through. The reservations
// service implements it; it runs in the success transaction.
type RefundNotifier interface {
	RefundSucceeded(ctx context.Context, r RefundDone) error
}
