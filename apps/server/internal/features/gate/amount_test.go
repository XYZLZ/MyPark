package gate_test

import (
	"testing"

	"github.com/XYZLZ/MyPark/apps/server/internal/features/gate"
)

func TestReceiptNo(t *testing.T) {
	for id, want := range map[int64]string{1: "MP-00000001", 42: "MP-00000042", 99999999: "MP-99999999", 123456789: "MP-123456789"} {
		if got := gate.ReceiptNo(id); got != want {
			t.Errorf("ReceiptNo(%d) = %q, want %q", id, got, want)
		}
	}
}
