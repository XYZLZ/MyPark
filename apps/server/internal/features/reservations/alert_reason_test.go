package reservations

import (
	"testing"

	"github.com/XYZLZ/MyPark/apps/server/internal/features/payments"
)

func TestAlertReason(t *testing.T) {
	notSubmitted := payments.FeePayment{Attempt: payments.Attempt{Status: payments.StatusFailed, FailureCode: payments.FailureNotSubmitted}}
	declined := payments.FeePayment{Attempt: payments.Attempt{Status: payments.StatusFailed, FailureCode: "card_declined"}}
	live := func(status string) payments.FeePayment {
		return payments.FeePayment{Attempt: payments.Attempt{Status: status}}
	}
	for _, tc := range []struct {
		name  string
		fee   payments.FeePayment
		live  payments.FeePayment
		found bool
		want  string
	}{
		{"not_submitted next to a succeeded fee", notSubmitted, live(payments.StatusSucceeded), true, alertDuplicateCharge},
		{"not_submitted next to a refunded fee", notSubmitted, live(payments.StatusRefunded), true, alertDuplicateCharge},
		{"not_submitted next to a pending fee", notSubmitted, live(payments.StatusPending), true, alertOrphanCharge},
		{"not_submitted whose live fee went away", notSubmitted, payments.FeePayment{}, false, alertOrphanCharge},
		{"declined payment, no live fee", declined, payments.FeePayment{}, false, alertGatewayInconsistent},
		{"declined payment, live fee", declined, live(payments.StatusSucceeded), true, alertGatewayInconsistent},
	} {
		if got := alertReason(tc.fee, tc.live, tc.found); got != tc.want {
			t.Errorf("%s: reason = %s, want %s", tc.name, got, tc.want)
		}
	}
}
