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
	RefundFacilityUnavailable = "facility_unavailable"
)

// GatewayKey is the gateway idempotency key of a fee payment (M3B-9).
func GatewayKey(paymentPublicID uuid.UUID) string { return "rsv-fee:" + paymentPublicID.String() }

// RefundKey is the gateway idempotency key of a refund.
func RefundKey(refundPublicID uuid.UUID) string { return "refund:" + refundPublicID.String() }
