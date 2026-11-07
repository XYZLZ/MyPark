package vehicles_test

import (
	"testing"

	"github.com/XYZLZ/MyPark/apps/server/internal/features/vehicles"
)

func TestLimitReached(t *testing.T) {
	for _, tc := range []struct {
		active, max int
		want        bool
	}{{4, 5, false}, {5, 5, true}, {6, 5, true}, {0, 1, false}} {
		if got := vehicles.LimitReached(tc.active, tc.max); got != tc.want {
			t.Errorf("LimitReached(%d, %d) = %v, want %v", tc.active, tc.max, got, tc.want)
		}
	}
}
