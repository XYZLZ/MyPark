package availability_test

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/XYZLZ/MyPark/apps/server/internal/domain/errcodes"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/availability"
)

func TestCompute(t *testing.T) {
	tests := []struct {
		name          string
		in            availability.SnapshotInput
		available     int
		effective     int
		status        string
		effectiveHold int
	}{
		{"room left", availability.SnapshotInput{Capacity: 280, Occupied: 268}, 12, 280, "open", 0},
		{"out of service fills it", availability.SnapshotInput{Capacity: 280, OutOfService: 3, Occupied: 277}, 0, 277, "full", 0},
		{"over capacity floors at zero", availability.SnapshotInput{Capacity: 280, Occupied: 300}, 0, 280, "full", 0},
		{"closed with room is closed", availability.SnapshotInput{Capacity: 280, Occupied: 10, LotClosed: true}, 270, 280, "closed", 0},
		{"closed beats full", availability.SnapshotInput{Capacity: 280, Occupied: 280, LotClosed: true}, 0, 280, "closed", 0},
		{"holds reduce available", availability.SnapshotInput{Capacity: 280, Occupied: 270, Holds: 5}, 5, 280, "open", 5},
		{"holds can make it full", availability.SnapshotInput{Capacity: 280, Occupied: 275, Holds: 5}, 0, 280, "full", 5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := availability.Compute(tt.in)
			if got.Available != tt.available || got.EffectiveCapacity != tt.effective ||
				string(got.Status) != tt.status || got.EffectiveHolds != tt.effectiveHold {
				t.Errorf("Compute(%+v) = %+v; want available %d, effective %d, status %s, holds %d",
					tt.in, got, tt.available, tt.effective, tt.status, tt.effectiveHold)
			}
		})
	}
}

func TestCompute_CopiesIdentityFields(t *testing.T) {
	id := uuid.New()
	at := time.Date(2026, 11, 20, 12, 0, 0, 0, time.FixedZone("x", -4*3600))
	got := availability.Compute(availability.SnapshotInput{
		LotPublicID: id, Capacity: 280, Occupied: 1, UpcomingArrivals2h: 2, Version: 7, UpdatedAt: at,
	})
	if got.LotID != id || got.Capacity != 280 || got.Occupied != 1 || got.UpcomingReservations2h != 2 || got.Version != 7 {
		t.Errorf("snapshot = %+v", got)
	}
	if !got.UpdatedAt.Equal(at) || got.UpdatedAt.Location() != time.UTC {
		t.Errorf("updated_at = %v, want the same instant in UTC", got.UpdatedAt)
	}
}

// The REST status and the gate decision read one rule: with nothing pending,
// a lot shows open exactly when the entry would admit, closed when the gate
// denies with gate.lot_closed, and full when it denies with gate.lot_full.
func TestCompute_AgreesWithCanAdmitDriveUp(t *testing.T) {
	for _, closed := range []bool{false, true} {
		for _, capacity := range []int{10, 280} {
			for _, oos := range []int{0, 1, 3} {
				for _, occ := range []int{0, capacity - oos - 1, capacity - oos, capacity + 1} {
					for _, holds := range []int{0, 1, 5} {
						ok, code := availability.CanAdmitDriveUp(availability.AdmitInput{
							LotClosed: closed, Capacity: capacity, OutOfService: oos, Occupied: occ, Holds: holds,
						})
						snap := availability.Compute(availability.SnapshotInput{
							LotClosed: closed, Capacity: capacity, OutOfService: oos, Occupied: occ, Holds: holds,
						})
						want := availability.StatusOpen
						switch {
						case code == errcodes.GateLotClosed:
							want = availability.StatusClosed
						case code == errcodes.GateLotFull:
							want = availability.StatusFull
						}
						if snap.Status != want || ok != (snap.Status == availability.StatusOpen) {
							t.Fatalf("closed=%v cap=%d oos=%d occ=%d holds=%d: gate (%v, %q) vs status %q",
								closed, capacity, oos, occ, holds, ok, code, snap.Status)
						}
						if !closed && ok != (snap.Available > 0) {
							t.Fatalf("admit %v but available %d", ok, snap.Available)
						}
					}
				}
			}
		}
	}
}
