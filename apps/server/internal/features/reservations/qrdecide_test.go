package reservations

import (
	"testing"
	"time"

	"github.com/XYZLZ/MyPark/apps/server/internal/domain/errcodes"
)

// TestDecideQR_Table covers every row of design 11.1 at the window edges.
func TestDecideQR_Table(t *testing.T) {
	w := Window{Start: time.Date(2026, 12, 1, 13, 50, 0, 0, time.UTC), End: time.Date(2026, 12, 1, 14, 25, 0, 0, time.UTC)}
	inside := w.Start.Add(8 * time.Minute)
	deny := func(c errcodes.Code) QROutcome { return QROutcome{Action: QRDeny, Reason: c} }
	in := func(st Status, now time.Time, occ, pend int, stay string) QRInput {
		return QRInput{Status: st, Window: w, Now: now, Occupied: occ, Pending: pend, EffectiveCapacity: 280, StayStatus: stay}
	}
	cases := []struct {
		name string
		in   QRInput
		want QROutcome
	}{
		{"confirmed before lower", in(StatusConfirmed, w.Start.Add(-time.Nanosecond), 0, 0, ""), deny(errcodes.ReservationTooEarly)},
		{"confirmed at lower", in(StatusConfirmed, w.Start, 0, 0, ""), QROutcome{Action: QRCheckIn}},
		{"confirmed at upper", in(StatusConfirmed, w.End, 0, 0, ""), deny(errcodes.ReservationWindowClosed)},
		{"confirmed after upper", in(StatusConfirmed, w.End.Add(time.Hour), 280, 0, ""), deny(errcodes.ReservationWindowClosed)},
		{"confirmed inside, occupied fills", in(StatusConfirmed, inside, 280, 0, ""), QROutcome{Action: QRPhysicallyFull}},
		{"confirmed inside, pending fills (M3B-23)", in(StatusConfirmed, inside, 279, 1, ""), QROutcome{Action: QRPhysicallyFull}},
		{"confirmed inside, over capacity", in(StatusConfirmed, inside, 281, 0, ""), QROutcome{Action: QRPhysicallyFull}},
		{"confirmed inside, room", in(StatusConfirmed, inside, 279, 0, ""), QROutcome{Action: QRCheckIn}},
		{"confirmed inside, voided stay", in(StatusConfirmed, inside, 0, 0, "voided"), QROutcome{Action: QRReopen}},
		{"confirmed inside, voided stay, full", in(StatusConfirmed, inside, 280, 0, "voided"), QROutcome{Action: QRPhysicallyFull}},
		{"confirmed with an open stay", in(StatusConfirmed, inside, 0, 0, "active"), deny(errcodes.GateCredentialInvalid)},
		{"checked_in, entry_pending inside", in(StatusCheckedIn, inside, 280, 0, "entry_pending"), QROutcome{Action: QRRefresh}},
		{"checked_in, entry_pending at upper", in(StatusCheckedIn, w.End, 0, 0, "entry_pending"), deny(errcodes.ReservationWindowClosed)},
		{"checked_in, active", in(StatusCheckedIn, inside, 0, 0, "active"), deny(errcodes.GateCredentialInUse)},
		{"checked_in, paid", in(StatusCheckedIn, w.End.Add(time.Hour), 0, 0, "paid"), deny(errcodes.GateCredentialInUse)},
		{"checked_in, voided", in(StatusCheckedIn, inside, 0, 0, "voided"), deny(errcodes.GateCredentialInvalid)},
		{"expired_no_show", in(StatusExpiredNoShow, inside, 0, 0, ""), deny(errcodes.ReservationWindowClosed)},
		{"pending_payment", in(StatusPendingPayment, inside, 0, 0, ""), deny(errcodes.GateCredentialInvalid)},
		{"cancelled", in(StatusCancelled, inside, 0, 0, ""), deny(errcodes.GateCredentialInvalid)},
		{"completed", in(StatusCompleted, inside, 0, 0, "exited"), deny(errcodes.GateCredentialInvalid)},
		{"expired_unpaid", in(StatusExpiredUnpaid, inside, 0, 0, ""), deny(errcodes.GateCredentialInvalid)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := DecideQR(tc.in); got != tc.want {
				t.Errorf("DecideQR = %+v, want %+v", got, tc.want)
			}
		})
	}
}
