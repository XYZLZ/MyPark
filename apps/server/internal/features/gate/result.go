package gate

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/XYZLZ/MyPark/apps/server/internal/domain/authz"
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

	// LateEntryStay is set by a late entry pass (M2C-31). It never reaches
	// the device or storage: the caller logs it with LogCommitted once the
	// event's transaction has committed.
	LateEntryStay uuid.UUID `json:"-"`
}

// LogCommitted writes the logs a handled event defers until its transaction
// commits, so a rolled-back event never logs something that did not happen.
// Call it only after a successful commit.
func LogCommitted(ctx context.Context, log *slog.Logger, d authz.Device, res Result) {
	if res.LateEntryStay != uuid.Nil {
		log.WarnContext(ctx, "gate: late entry pass on a voided stay",
			"lot_id", d.LotID, "stay_id", res.LateEntryStay.String(), "device_id", d.PublicID.String())
	}
}
