package reservations

import (
	"errors"
	"testing"
)

type pair struct {
	from Status
	ev   Event
}

var allStatuses = []Status{StatusPendingPayment, StatusConfirmed, StatusCheckedIn, StatusCompleted, StatusCancelled, StatusExpiredUnpaid, StatusExpiredNoShow}
var allEvents = []Event{EvConfirm, EvExpireUnpaid, EvLateSuccess, EvDriverCancel, EvCheckIn, EvUnadmitted, EvNoShow, EvComplete, EvEntryVoided, EvLateEntryPass}

func TestTransition_FullTable(t *testing.T) {
	allowed := map[pair]Status{
		{StatusPendingPayment, EvConfirm}:      StatusConfirmed,
		{StatusPendingPayment, EvExpireUnpaid}: StatusExpiredUnpaid,
		{StatusPendingPayment, EvLateSuccess}:  StatusExpiredUnpaid,
		{StatusPendingPayment, EvDriverCancel}: StatusCancelled,
		{StatusConfirmed, EvDriverCancel}:      StatusCancelled,
		{StatusConfirmed, EvCheckIn}:           StatusCheckedIn,
		{StatusConfirmed, EvUnadmitted}:        StatusCancelled,
		{StatusConfirmed, EvNoShow}:            StatusExpiredNoShow,
		{StatusConfirmed, EvLateEntryPass}:     StatusCheckedIn,
		{StatusCheckedIn, EvComplete}:          StatusCompleted,
		{StatusCheckedIn, EvEntryVoided}:       StatusConfirmed,
	}
	if len(allStatuses)*len(allEvents) != 70 {
		t.Fatal("expected 7x10 matrix")
	}
	for _, s := range allStatuses {
		for _, e := range allEvents {
			got, err := Transition(s, e)
			want, ok := allowed[pair{s, e}]
			if ok {
				if err != nil || got != want {
					t.Errorf("%s+%s = %q,%v want %q", s, e, got, err, want)
				}
				continue
			}
			if !errors.Is(err, ErrInvalidTransition) {
				t.Errorf("%s+%s: err %v, want ErrInvalidTransition", s, e, err)
			}
			if got != s {
				t.Errorf("%s+%s: rejected transition returned %q, want unchanged", s, e, got)
			}
		}
	}
}

func TestStatus_ActiveAndHoldsSlots(t *testing.T) {
	for _, s := range allStatuses {
		want := s == StatusPendingPayment || s == StatusConfirmed
		if s.Active() != want || s.HoldsSlots() != want {
			t.Errorf("%s: Active=%v HoldsSlots=%v want %v", s, s.Active(), s.HoldsSlots(), want)
		}
	}
}
