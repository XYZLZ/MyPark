package simulator

import (
	"context"
	"testing"
	"time"

	"github.com/XYZLZ/MyPark/apps/server/internal/testsupport/fakeclock"
)

func TestStepClock_WaitUntilJumps(t *testing.T) {
	c := NewStepClock(t0)
	target := t0.Add(3 * time.Hour)
	if err := c.WaitUntil(context.Background(), target); err != nil {
		t.Fatal(err)
	}
	if !c.Now().Equal(target) {
		t.Errorf("Now = %v, want %v", c.Now(), target)
	}
	if err := c.WaitUntil(context.Background(), t0); err != nil {
		t.Fatal(err)
	}
	if !c.Now().Equal(target) {
		t.Errorf("clock went backwards to %v", c.Now())
	}
}

func TestStepClock_CanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := NewStepClock(t0).WaitUntil(ctx, t0.Add(time.Second)); err != context.Canceled {
		t.Errorf("err = %v", err)
	}
}

func TestRealClock_SpeedMapping(t *testing.T) {
	wall := fakeclock.New(time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC))
	var slept []time.Duration
	c := NewRealClock(t0, 30, wall, WithSleep(func(_ context.Context, d time.Duration) error {
		slept = append(slept, d)
		return nil
	}))
	if err := c.WaitUntil(context.Background(), t0.Add(30*time.Second)); err != nil {
		t.Fatal(err)
	}
	if len(slept) != 1 || slept[0] != time.Second {
		t.Fatalf("slept = %v, want [1s]", slept)
	}
	wall.Advance(2 * time.Second)
	if want := t0.Add(60 * time.Second); !c.Now().Equal(want) {
		t.Errorf("Now = %v, want %v", c.Now(), want)
	}
	// An instant already in the past waits for nothing.
	if err := c.WaitUntil(context.Background(), t0); err != nil {
		t.Fatal(err)
	}
	if len(slept) != 1 {
		t.Errorf("slept for a past instant: %v", slept)
	}
}

func TestRealClock_DefaultSleepHonorsCancel(t *testing.T) {
	wall := fakeclock.New(t0)
	c := NewRealClock(t0, 1, wall)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := c.WaitUntil(ctx, t0.Add(time.Hour)); err != context.Canceled {
		t.Errorf("err = %v", err)
	}
}
