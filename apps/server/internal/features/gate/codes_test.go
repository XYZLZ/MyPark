package gate_test

import (
	"testing"

	"github.com/XYZLZ/MyPark/apps/server/internal/features/gate"
)

func TestNormalizeTicketCode(t *testing.T) {
	tests := []struct {
		name, in, want string
		ok             bool
	}{
		{"lowercase and dashes", "7k3m-9qzx-0abc-1def", "7K3M9QZX0ABC1DEF", true},
		{"spaces", "7K3M 9QZX 0ABC 1DEF", "7K3M9QZX0ABC1DEF", true},
		{"O and L look-alikes", "7K3M9QZXOABCLDEF", "7K3M9QZX0ABC1DEF", true},
		{"I look-alike", "7K3M9QZX0ABCIDEF", "7K3M9QZX0ABC1DEF", true},
		{"fifteen chars", "7K3M9QZX0ABC1DE", "", false},
		{"seventeen chars", "7K3M9QZX0ABC1DEFG", "", false},
		{"U is not Crockford", "7K3M9QZX0ABC1DEU", "", false},
		{"non-ascii", "7K3M9QZX0ABC1DEé", "", false},
		{"empty", "", "", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := gate.NormalizeTicketCode(tc.in)
			if ok != tc.ok || got != tc.want {
				t.Fatalf("NormalizeTicketCode(%q) = %q, %v; want %q, %v", tc.in, got, ok, tc.want, tc.ok)
			}
		})
	}
}

func TestNormalizeCardUID(t *testing.T) {
	tests := []struct {
		name, in, want string
		ok             bool
	}{
		{"colons lowercase", "df:fe:6e:ad:ba:0a:4c", "DFFE6EADBA0A4C", true},
		{"dashes and spaces", "df-fe 6e ad", "DFFE6EAD", true},
		{"eight chars", "DFFE6EAD", "DFFE6EAD", true},
		{"twenty chars", "DFFE6EADBA0A4C112233", "DFFE6EADBA0A4C112233", true},
		{"twenty-one chars", "DFFE6EADBA0A4C1122334", "", false},
		{"seven chars", "DFFE6EA", "", false},
		{"not hex", "xyz", "", false},
		{"empty", "", "", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := gate.NormalizeCardUID(tc.in)
			if ok != tc.ok || got != tc.want {
				t.Fatalf("NormalizeCardUID(%q) = %q, %v; want %q, %v", tc.in, got, ok, tc.want, tc.ok)
			}
		})
	}
}

func TestAllEventTypes(t *testing.T) {
	all := gate.AllEventTypes()
	if len(all) != 9 {
		t.Fatalf("AllEventTypes() has %d entries, want 9", len(all))
	}
	seen := map[gate.EventType]bool{}
	for _, e := range all {
		if seen[e] {
			t.Fatalf("duplicate %q", e)
		}
		seen[e] = true
	}
}
