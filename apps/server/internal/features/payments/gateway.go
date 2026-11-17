package payments

import (
	"context"
	"errors"
)

// Gateway is the payment provider port. Both calls are idempotent on their
// key. Any error other than ErrChargeUnknown (timeouts, transport failures)
// means the outcome is unknown.
type Gateway interface {
	Charge(ctx context.Context, req ChargeRequest) (ChargeResult, error)
	Refund(ctx context.Context, req RefundRequest) (RefundResult, error)
}

// ChargeOutcome is the recorded result of a charge.
type ChargeOutcome int

// Charge outcomes. The zero value is deliberately not a valid outcome.
const (
	ChargeSucceeded ChargeOutcome = iota + 1
	ChargeDeclined
)

// ChargeRequest asks the gateway to charge a card.
type ChargeRequest struct {
	IdempotencyKey string // "rsv-fee:<payment public_id>", required
	AmountCents    int64
	Card           *Card // nil on a re-query
}

// ChargeResult is the recorded outcome of a charge.
type ChargeResult struct {
	Outcome     ChargeOutcome
	DeclineCode string // card_declined | card_not_supported
	ProviderRef string
}

// RefundRequest asks the gateway to refund a charge in full.
type RefundRequest struct {
	IdempotencyKey string // "refund:<refund public_id>"
	ChargeKey      string // the charge's idempotency key
	AmountCents    int64
}

// RefundResult is the recorded outcome of a refund.
type RefundResult struct{ ProviderRef string }

// ErrChargeUnknown means a re-query named a charge key the gateway never saw.
var ErrChargeUnknown = errors.New("payments: charge key never seen")

// ErrSuccessAfterFinal means a charge success arrived for a payment that had
// already failed or been refunded: money may have been taken that no row owns.
var ErrSuccessAfterFinal = errors.New("payments: charge succeeded after the payment was final")
