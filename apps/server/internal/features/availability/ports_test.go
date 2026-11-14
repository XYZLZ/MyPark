package availability_test

import (
	"testing"
	"time"

	"github.com/XYZLZ/MyPark/apps/server/internal/features/availability"
)

// M3 swaps this adapter; until then no reservation holds capacity (M2C-1).
var _ availability.ReservationHolds = availability.NoReservations{}

func TestNoReservations_AlwaysZero(t *testing.T) {
	var h availability.ReservationHolds = availability.NoReservations{}
	now := time.Date(2026, 11, 14, 10, 0, 0, 0, time.UTC)
	if n, err := h.EffectiveHolds(t.Context(), 1, now); n != 0 || err != nil {
		t.Errorf("EffectiveHolds = %d, %v, want 0, nil", n, err)
	}
	if n, err := h.UpcomingArrivals(t.Context(), 1, now, 2*time.Hour); n != 0 || err != nil {
		t.Errorf("UpcomingArrivals = %d, %v, want 0, nil", n, err)
	}
}
