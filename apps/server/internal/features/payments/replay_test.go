package payments_test

import (
	"testing"

	"github.com/XYZLZ/MyPark/apps/server/internal/domain/errcodes"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/payments"
)

func TestDecideReplay(t *testing.T) {
	a, b := []byte{1, 2, 3}, []byte{1, 2, 4}
	if replay, code := payments.DecideReplay(a, []byte{1, 2, 3}); !replay || code != "" {
		t.Errorf("equal = (%v, %q), want (true, \"\")", replay, code)
	}
	if replay, code := payments.DecideReplay(a, b); replay || code != errcodes.IdempotencyKeyReused {
		t.Errorf("different = (%v, %q), want (false, %q)", replay, code, errcodes.IdempotencyKeyReused)
	}
	if replay, code := payments.DecideReplay(nil, b); replay || code != errcodes.IdempotencyKeyReused {
		t.Errorf("nil stored = (%v, %q)", replay, code)
	}
}
