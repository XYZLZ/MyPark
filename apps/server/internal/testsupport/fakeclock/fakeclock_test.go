package fakeclock_test

import (
	"sync"
	"testing"
	"time"

	"github.com/XYZLZ/MyPark/apps/server/internal/testsupport/fakeclock"
)

func TestClock_AdvanceIsRaceFree(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	c := fakeclock.New(start)
	var wg sync.WaitGroup
	for range 10 {
		wg.Go(func() { c.Advance(time.Second) })
	}
	wg.Wait()
	if want := start.Add(10 * time.Second); !c.Now().Equal(want) {
		t.Errorf("Now = %v, want %v", c.Now(), want)
	}
	c.Set(start)
	if !c.Now().Equal(start) {
		t.Error("Set did not apply")
	}
}
