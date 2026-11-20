package simulator_test

import (
	"testing"

	"github.com/XYZLZ/MyPark/apps/server/internal/simulator"
)

func TestDriverPool_RoundRobinSkips(t *testing.T) {
	p := &simulator.DriverPool{Range: simulator.PoolArrivals}
	for want := 1; want <= 3; want++ {
		if got, ok := p.Next(nil); !ok || got != want {
			t.Fatalf("Next = %d,%v, want %d", got, ok, want)
		}
	}
	// Skipped drivers are passed over and the rotation continues after the pick.
	got, ok := p.Next(func(n int) bool { return n == 4 || n == 5 })
	if !ok || got != 6 {
		t.Fatalf("Next skipping 4,5 = %d,%v, want 6", got, ok)
	}
	if got, _ := p.Next(nil); got != 7 {
		t.Errorf("after 6, Next = %d, want 7", got)
	}
	// Wrap-around.
	seen := map[int]bool{}
	for range 16 {
		n, _ := p.Next(nil)
		seen[n] = true
		if n < 1 || n > 16 {
			t.Fatalf("arrivals pool yielded %d", n)
		}
	}
	if len(seen) != 16 {
		t.Errorf("16 draws covered %d drivers, want 16", len(seen))
	}
	if n, ok := p.Next(func(int) bool { return true }); ok {
		t.Errorf("all skipped returned %d, want false", n)
	}
}

func TestDriverPool_NoShowNeverYieldsArrivalsDrivers(t *testing.T) {
	p := &simulator.DriverPool{Range: simulator.PoolNoShow}
	for range 40 {
		if n, ok := p.Next(nil); !ok || n < 17 || n > 20 {
			t.Fatalf("no-show pool yielded %d,%v", n, ok)
		}
	}
}
