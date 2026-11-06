package errcodes_test

import (
	"testing"

	"github.com/XYZLZ/MyPark/apps/server/internal/domain/errcodes"
)

func TestAll_SortedAndComplete(t *testing.T) {
	want := []errcodes.Code{
		"health.not_ready",
		"http.method_not_allowed",
		"http.not_found",
		"internal.unexpected",
	}
	all := errcodes.All()
	if len(all) != len(want) {
		t.Fatalf("All() = %v, want %v", all, want)
	}
	for i := range want {
		if all[i] != want[i] {
			t.Errorf("All()[%d] = %q, want %q", i, all[i], want[i])
		}
	}
}

func TestAll_ReturnsCopy(t *testing.T) {
	a := errcodes.All()
	a[0] = "mutated"
	if errcodes.All()[0] == "mutated" {
		t.Fatal("All() exposes internal slice")
	}
}
