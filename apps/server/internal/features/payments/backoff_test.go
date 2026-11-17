package payments_test

import (
	"testing"
	"time"

	"github.com/XYZLZ/MyPark/apps/server/internal/features/payments"
)

func TestRefundBackoff(t *testing.T) {
	want := []time.Duration{10 * time.Second, 20 * time.Second, 40 * time.Second, 80 * time.Second,
		160 * time.Second, 320 * time.Second, 640 * time.Second, 1280 * time.Second, 30 * time.Minute}
	for i, w := range want {
		attempts := i + 1
		got, failed := payments.RefundBackoff(attempts)
		if failed || got != w {
			t.Errorf("RefundBackoff(%d) = (%v, %v), want (%v, false)", attempts, got, failed, w)
		}
	}
	for _, attempts := range []int{payments.RefundMaxAttempts, 11, 1000} {
		if got, failed := payments.RefundBackoff(attempts); !failed || got != 0 {
			t.Errorf("RefundBackoff(%d) = (%v, %v), want (0, true)", attempts, got, failed)
		}
	}
	// Defensive: a claim always increments first, but 0 must not overflow.
	if got, failed := payments.RefundBackoff(0); failed || got != 10*time.Second {
		t.Errorf("RefundBackoff(0) = (%v, %v)", got, failed)
	}
}
