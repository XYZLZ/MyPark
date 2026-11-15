package availability_test

import (
	"net/netip"
	"sync"
	"testing"

	"github.com/XYZLZ/MyPark/apps/server/internal/features/availability"
)

func TestStreamCaps(t *testing.T) {
	caps := availability.NewStreamCaps(2, 3)
	a := netip.MustParseAddr("198.51.100.1")
	b := netip.MustParseAddr("198.51.100.2")
	c := netip.MustParseAddr("198.51.100.3")

	relA1, ok := caps.Acquire(a)
	if !ok {
		t.Fatal("A #1 refused")
	}
	if _, ok := caps.Acquire(a); !ok {
		t.Fatal("A #2 refused")
	}
	if _, ok := caps.Acquire(a); ok {
		t.Fatal("A #3 accepted over the per-IP cap of 2")
	}
	if _, ok := caps.Acquire(b); !ok {
		t.Fatal("B refused under both caps")
	}
	if _, ok := caps.Acquire(c); ok {
		t.Fatal("C accepted over the total cap of 3")
	}
	relA1()
	relA1() // idempotent: must not free a second slot
	relC, ok := caps.Acquire(c)
	if !ok {
		t.Fatal("C refused after a release")
	}
	if _, ok := caps.Acquire(c); ok {
		t.Fatal("a double release freed two slots")
	}
	relC()
}

// An IPv4 address and its IPv4-mapped IPv6 form are one client.
func TestStreamCaps_MappedIPv4SharesBucket(t *testing.T) {
	caps := availability.NewStreamCaps(1, 10)
	if _, ok := caps.Acquire(netip.MustParseAddr("198.51.100.1")); !ok {
		t.Fatal("first refused")
	}
	if _, ok := caps.Acquire(netip.MustParseAddr("::ffff:198.51.100.1")); ok {
		t.Fatal("mapped form of the same IP got its own bucket")
	}
}

func TestStreamCaps_Concurrent(t *testing.T) {
	caps := availability.NewStreamCaps(1000, 50)
	ip := netip.MustParseAddr("198.51.100.1")
	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		accepted int
		releases []func()
	)
	for range 200 {
		wg.Go(func() {
			if rel, ok := caps.Acquire(ip); ok {
				mu.Lock()
				accepted++
				releases = append(releases, rel)
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	if accepted != 50 {
		t.Fatalf("accepted %d streams, want exactly the total cap 50", accepted)
	}
	for _, rel := range releases {
		wg.Go(rel)
	}
	wg.Wait()
	if _, ok := caps.Acquire(ip); !ok {
		t.Fatal("refused after every stream was released")
	}
}
