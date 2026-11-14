package gate_test

import (
	"testing"

	"github.com/XYZLZ/MyPark/apps/server/internal/features/gate"
)

func TestCanTransition_FullTable(t *testing.T) {
	all := []gate.Status{gate.StatusEntryPending, gate.StatusActive, gate.StatusPaid, gate.StatusExited, gate.StatusVoided}
	allowed := map[[2]gate.Status]bool{
		{gate.StatusEntryPending, gate.StatusActive}: true,
		{gate.StatusEntryPending, gate.StatusVoided}: true,
		{gate.StatusVoided, gate.StatusActive}:       true, // late entry pass (M2C-31)
		{gate.StatusActive, gate.StatusPaid}:         true,
		{gate.StatusPaid, gate.StatusActive}:         true, // overstay or lost ticket
		{gate.StatusPaid, gate.StatusExited}:         true,
	}
	n := 0
	for _, from := range all {
		for _, to := range all {
			want := allowed[[2]gate.Status{from, to}]
			if got := gate.CanTransition(from, to); got != want {
				t.Errorf("CanTransition(%s, %s) = %v, want %v", from, to, got, want)
			}
			if want {
				n++
			}
		}
	}
	if n != 6 {
		t.Fatalf("allowed pairs = %d, want 6", n)
	}
	if gate.CanTransition("", gate.StatusActive) || gate.CanTransition(gate.StatusActive, "parked") {
		t.Error("an unknown status must never transition")
	}
}
