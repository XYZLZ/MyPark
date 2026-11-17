package reservations

import (
	"time"

	"github.com/XYZLZ/MyPark/apps/server/internal/domain/errcodes"
)

// QRAction is what the entry barrier does with a reservation QR (design 11.1).
type QRAction int

// The QR outcomes.
const (
	// QRDeny denies with QROutcome.Reason.
	QRDeny QRAction = iota
	// QRCheckIn checks the confirmed reservation in and inserts its stay.
	QRCheckIn
	// QRReopen checks the confirmed reservation in again and reuses its
	// voided stay (voided -> entry_pending, M3B-24, M3B-60).
	QRReopen
	// QRRefresh re-opens for a checked-in reservation whose stay is still
	// entry_pending, refreshing accepted_at (M3B-24).
	QRRefresh
	// QRPhysicallyFull cancels the reservation with a refund (design 11.5).
	QRPhysicallyFull
)

// QRInput is what the decision reads, all under the lot and reservation
// locks. StayStatus is the reservation's stay status, "" when it has none.
type QRInput struct {
	Status                               Status
	Window                               Window
	Now                                  time.Time
	Occupied, Pending, EffectiveCapacity int
	StayStatus                           string
}

// QROutcome is the decision; Reason is set only for QRDeny.
type QROutcome struct {
	Action QRAction
	Reason errcodes.Code
}

func qrDeny(c errcodes.Code) QROutcome { return QROutcome{Action: QRDeny, Reason: c} }

// DecideQR implements the design 11.1 table. The physically-full predicate
// is occupied + pending >= effective capacity (M3B-23); the QR skips the
// public availability check (spec 5.2 rule 2), so holds are not an input.
func DecideQR(in QRInput) QROutcome {
	switch in.Status {
	case StatusConfirmed:
		return decideConfirmed(in)
	case StatusCheckedIn:
		switch {
		case in.StayStatus == "active" || in.StayStatus == "paid":
			return qrDeny(errcodes.GateCredentialInUse)
		case in.StayStatus != "entry_pending":
			return qrDeny(errcodes.GateCredentialInvalid)
		case !in.Window.Contains(in.Now):
			return qrDeny(errcodes.ReservationWindowClosed)
		}
		return QROutcome{Action: QRRefresh}
	case StatusExpiredNoShow:
		return qrDeny(errcodes.ReservationWindowClosed)
	}
	return qrDeny(errcodes.GateCredentialInvalid)
}

func decideConfirmed(in QRInput) QROutcome {
	switch {
	case in.Now.Before(in.Window.Start):
		return qrDeny(errcodes.ReservationTooEarly)
	case !in.Now.Before(in.Window.End):
		return qrDeny(errcodes.ReservationWindowClosed)
	case in.StayStatus != "" && in.StayStatus != "voided":
		// A confirmed reservation owns no open stay; never insert a second one.
		return qrDeny(errcodes.GateCredentialInvalid)
	case in.Occupied+in.Pending >= in.EffectiveCapacity:
		return QROutcome{Action: QRPhysicallyFull}
	case in.StayStatus == "voided":
		return QROutcome{Action: QRReopen}
	}
	return QROutcome{Action: QRCheckIn}
}
