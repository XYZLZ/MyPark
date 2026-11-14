package gate

import (
	"time"

	"github.com/google/uuid"

	"github.com/XYZLZ/MyPark/apps/server/internal/features/tariffs"
)

// Outcome is the `result` of a device answer. A business refusal is a 200
// with OutcomeDeny or OutcomeRejected, never an HTTP error.
type Outcome string

// The four outcomes.
const (
	OutcomeAck      Outcome = "ack"
	OutcomeOpen     Outcome = "open"
	OutcomeDeny     Outcome = "deny"
	OutcomeRejected Outcome = "rejected"
)

// Result is the data of a 200 answer to a device event (design §7.2).
// TicketCode is plain only on its way to the device: it is sealed before
// the answer is stored.
type Result struct {
	Result       Outcome            `json:"result"`
	Reason       string             `json:"reason,omitempty"`
	StayID       *uuid.UUID         `json:"stay_id,omitempty"`
	TicketCode   string             `json:"ticket_code,omitempty"`
	Status       string             `json:"status,omitempty"`
	AmountCents  *int64             `json:"amount_cents,omitempty"`
	Breakdown    *tariffs.Breakdown `json:"breakdown,omitempty"`
	ReceiptNo    string             `json:"receipt_no,omitempty"`
	PaidAt       *time.Time         `json:"paid_at,omitempty"`
	ExitDeadline *time.Time         `json:"exit_deadline,omitempty"`
}
