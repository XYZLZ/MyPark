package payments

import "time"

// RefundMaxAttempts is how many gateway attempts a refund gets before it
// fails and an alert opens.
const RefundMaxAttempts = 10

const (
	refundBackoffBase = 10 * time.Second
	refundBackoffMax  = 30 * time.Minute
)

// RefundBackoff returns the wait after the attempts-th failed attempt:
// min(10s * 2^(attempts-1), 30m). From RefundMaxAttempts on the refund has
// failed for good (wait 0, failed true).
func RefundBackoff(attempts int) (wait time.Duration, failed bool) {
	if attempts >= RefundMaxAttempts {
		return 0, true
	}
	wait = refundBackoffBase
	for range max(attempts, 1) - 1 {
		wait *= 2
		if wait >= refundBackoffMax {
			return refundBackoffMax, false
		}
	}
	return wait, false
}
