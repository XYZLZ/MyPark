package payments

import (
	"bytes"

	"github.com/XYZLZ/MyPark/apps/server/internal/domain/errcodes"
)

// DecideReplay compares the stored fingerprint of an Idempotency-Key with the
// one just computed: equal means replay the stored outcome, different means
// the key was reused for another request.
func DecideReplay(stored, requested []byte) (replay bool, code errcodes.Code) {
	if bytes.Equal(stored, requested) {
		return true, ""
	}
	return false, errcodes.IdempotencyKeyReused
}
