package availability_test

import (
	"testing"

	"github.com/XYZLZ/MyPark/apps/server/internal/domain/errcodes"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/availability"
)

func TestCanAdmitDriveUp(t *testing.T) {
	tests := []struct {
		name string
		in   availability.AdmitInput
		ok   bool
		code errcodes.Code
	}{
		{"last space free", availability.AdmitInput{Capacity: 280, Occupied: 279}, true, ""},
		{"last space pending", availability.AdmitInput{Capacity: 280, Occupied: 279, Pending: 1}, false, errcodes.GateLotFull},
		{"out of service shrinks capacity", availability.AdmitInput{Capacity: 280, OutOfService: 2, Occupied: 278}, false, errcodes.GateLotFull},
		{"closed wins over room", availability.AdmitInput{LotClosed: true, Capacity: 280}, false, errcodes.GateLotClosed},
		{"closed wins over full", availability.AdmitInput{LotClosed: true, Capacity: 280, Occupied: 280}, false, errcodes.GateLotClosed},
		{"a hold takes the last space", availability.AdmitInput{Capacity: 280, Occupied: 279, Holds: 1}, false, errcodes.GateLotFull},
		{"over capacity stays full", availability.AdmitInput{Capacity: 280, Occupied: 281}, false, errcodes.GateLotFull},
		{"empty lot", availability.AdmitInput{Capacity: 280}, true, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ok, code := availability.CanAdmitDriveUp(tt.in)
			if ok != tt.ok || code != tt.code {
				t.Errorf("CanAdmitDriveUp(%+v) = %v, %q; want %v, %q", tt.in, ok, code, tt.ok, tt.code)
			}
		})
	}
}
