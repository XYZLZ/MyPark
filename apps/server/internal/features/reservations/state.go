package reservations

import (
	"errors"
	"fmt"
)

// ErrInvalidTransition is returned for a (status, event) pair not in design §10.1.
var ErrInvalidTransition = errors.New("invalid reservation transition")

// Status is a reservation status (the DB literals).
type Status string

// Reservation statuses.
const (
	StatusPendingPayment Status = "pending_payment"
	StatusConfirmed      Status = "confirmed"
	StatusCheckedIn      Status = "checked_in"
	StatusCompleted      Status = "completed"
	StatusCancelled      Status = "cancelled"
	StatusExpiredUnpaid  Status = "expired_unpaid"
	StatusExpiredNoShow  Status = "expired_no_show"
)

// Event is a trigger of a reservation transition.
type Event string

// Reservation events.
const (
	EvConfirm       Event = "confirm"         // charge succeeded inside the window
	EvExpireUnpaid  Event = "expire_unpaid"   // payment deadline passed (job)
	EvLateSuccess   Event = "late_success"    // charge succeeded after the window closed
	EvDriverCancel  Event = "driver_cancel"   // driver cancels
	EvCheckIn       Event = "check_in"        // valid QR inside the window
	EvUnadmitted    Event = "unadmitted"      // QR while physically full
	EvNoShow        Event = "no_show"         // window closed without arrival (job)
	EvComplete      Event = "complete"        // exit pass of its stay
	EvEntryVoided   Event = "entry_voided"    // void job voids its stay (M3B-60)
	EvLateEntryPass Event = "late_entry_pass" // late entry pass of a voided stay
)

var transitions = map[Status]map[Event]Status{
	StatusPendingPayment: {
		EvConfirm:      StatusConfirmed,
		EvExpireUnpaid: StatusExpiredUnpaid,
		EvLateSuccess:  StatusExpiredUnpaid,
		EvDriverCancel: StatusCancelled,
	},
	StatusConfirmed: {
		EvDriverCancel:  StatusCancelled,
		EvCheckIn:       StatusCheckedIn,
		EvUnadmitted:    StatusCancelled,
		EvNoShow:        StatusExpiredNoShow,
		EvLateEntryPass: StatusCheckedIn,
	},
	StatusCheckedIn: {
		EvComplete:    StatusCompleted,
		EvEntryVoided: StatusConfirmed,
	},
}

// Transition returns the status reached from `from` on `ev`. A pair outside
// design §10.1 returns from unchanged and ErrInvalidTransition.
func Transition(from Status, ev Event) (Status, error) {
	if to, ok := transitions[from][ev]; ok {
		return to, nil
	}
	return from, fmt.Errorf("%w: %s on %s", ErrInvalidTransition, ev, from)
}

// Active reports whether the reservation counts toward the per-user limit (FR-RES-3).
func (s Status) Active() bool {
	return s == StatusPendingPayment || s == StatusConfirmed
}

// HoldsSlots reports whether the reservation holds slot loads (M3B-16).
func (s Status) HoldsSlots() bool {
	return s.Active()
}
