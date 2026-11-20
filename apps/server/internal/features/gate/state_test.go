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
		{gate.StatusVoided, gate.StatusEntryPending}: true, // reservation QR re-presented (M3B-24)
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
	if n != 7 {
		t.Fatalf("allowed pairs = %d, want 7", n)
	}
	if gate.CanTransition("", gate.StatusActive) || gate.CanTransition(gate.StatusActive, "parked") {
		t.Error("an unknown status must never transition")
	}
}

// A voided reservation stay is reused when its reservation, back to
// confirmed after the void (M3B-60), is checked in again (design 10.4).
func TestCanTransition_VoidedToEntryPending(t *testing.T) {
	if !gate.CanTransition(gate.StatusVoided, gate.StatusEntryPending) {
		t.Error("voided -> entry_pending must be allowed")
	}
	if gate.CanTransition(gate.StatusExited, gate.StatusEntryPending) || gate.CanTransition(gate.StatusActive, gate.StatusEntryPending) {
		t.Error("only a voided stay may return to entry_pending")
	}
}
