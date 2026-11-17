package payments

import "github.com/google/uuid"

// Payment statuses and failure codes as stored.
const (
	StatusPending   = "pending"
	StatusSucceeded = "succeeded"
	StatusFailed    = "failed"
	StatusRefunded  = "refunded"

	FailureNotSubmitted = "not_submitted"
)

// Refund reasons stored on refunds and reservations.
const (
	RefundFacilityUnavailable    = "facility_unavailable"
	RefundCancelledBeforeArrival = "cancelled_before_arrival"

	// ReasonNoneAfterArrival is the reservation refund_reason of a cancel at or
	// after arrival: no refund row exists for it.
	ReasonNoneAfterArrival = "none_after_arrival"
)

// GatewayKey is the gateway idempotency key of a fee payment (M3B-9).
func GatewayKey(paymentPublicID uuid.UUID) string { return "rsv-fee:" + paymentPublicID.String() }

// RefundKey is the gateway idempotency key of a refund.
func RefundKey(refundPublicID uuid.UUID) string { return "refund:" + refundPublicID.String() }
