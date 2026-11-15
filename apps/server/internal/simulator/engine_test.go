package simulator

import (
	"context"
	"testing"
	"time"
)

var t0 = time.Date(2026, 11, 24, 7, 0, 0, 0, time.UTC)

func TestEngine_OrderAndTieBreak(t *testing.T) {
	c := NewStepClock(t0)
	e := NewEngine(c, 1)
	var order []string
	e.At(t0.Add(2*time.Second), func(context.Context) { order = append(order, "A") })
	e.At(t0.Add(time.Second), func(context.Context) {
		order = append(order, "B")
		e.At(t0.Add(time.Second), func(context.Context) { order = append(order, "D") })
	})
	e.At(t0.Add(time.Second), func(context.Context) { order = append(order, "C") })

	if err := e.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	want := []string{"B", "C", "D", "A"}
	if len(order) != len(want) {
		t.Fatalf("order = %v, want %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("order = %v, want %v", order, want)
		}
	}
	if !e.Now().Equal(t0.Add(2 * time.Second)) {
		t.Errorf("Now = %v", e.Now())
	}
}

func TestEngine_SameSeedSameDraws(t *testing.T) {
	draws := func(seed uint64) []float64 {
		e := NewEngine(NewStepClock(t0), seed)
		out := make([]float64, 100)
		for i := range out {
			out[i] = e.Rand().Float64()
		}
		return out
	}
	a, b, c := draws(42), draws(42), draws(43)
	same := true
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("seed 42 diverges at %d", i)
		}
		if a[i] != c[i] {
			same = false
		}
	}
	if same {
		t.Error("seeds 42 and 43 give the same draws")
	}
}

func TestEngine_RunStopsOnCancel(t *testing.T) {
	e := NewEngine(NewStepClock(t0), 1)
	ctx, cancel := context.WithCancel(context.Background())
	ran := 0
	e.At(t0, func(context.Context) { ran++; cancel() })
	e.At(t0.Add(time.Second), func(context.Context) { ran++ })
	if err := e.Run(ctx); err != context.Canceled {
		t.Fatalf("Run = %v, want context.Canceled", err)
	}
	if ran != 1 {
		t.Errorf("ran %d steps, want 1", ran)
	}
}

func TestEngine_EmptyQueueReturnsNil(t *testing.T) {
	if err := NewEngine(NewStepClock(t0), 1).Run(context.Background()); err != nil {
		t.Fatal(err)
	}
}

// parkClock blocks every wait until ctx is done, to show what wakes the engine.
type parkClock struct {
	*StepClock
	waiting chan time.Time
}

func (c parkClock) WaitUntil(ctx context.Context, t time.Time) error {
	if t.After(c.Now().Add(time.Hour)) {
		c.waiting <- t
		<-ctx.Done()
		return ctx.Err()
	}
	return c.StepClock.WaitUntil(ctx, t)
}

func TestEngine_EarlierEventWakesAWait(t *testing.T) {
	c := parkClock{NewStepClock(t0), make(chan time.Time, 1)}
	e := NewEngine(c, 1)
	e.At(t0.Add(5*time.Hour), func(context.Context) { t.Error("the far event ran") })
	ran := make(chan struct{})
	go func() { _ = e.Run(t.Context()) }()
	<-c.waiting // the engine is parked on the far event
	e.At(t0.Add(time.Minute), func(context.Context) { close(ran) })
	<-ran
	if got := (<-c.waiting); !got.Equal(t0.Add(5 * time.Hour)) {
		t.Errorf("engine went back to wait for %v", got)
	}
}

func TestEngine_WakeSignalsAnEventAfterAnEmptyRun(t *testing.T) {
	e := NewEngine(NewStepClock(t0), 1)
	if err := e.Run(t.Context()); err != nil {
		t.Fatal(err)
	}
	e.At(t0, func(context.Context) {})
	select {
	case <-e.Wake():
	default:
		t.Fatal("At after an empty Run did not signal Wake")
	}
}

func TestEngine_AfterStepRunsAfterEveryStep(t *testing.T) {
	e := NewEngine(NewStepClock(t0), 1)
	n := 0
	e.SetAfterStep(func() { n++ })
	e.At(t0, func(context.Context) {})
	e.At(t0.Add(time.Second), func(context.Context) {})
	if err := e.Run(t.Context()); err != nil || n != 2 {
		t.Fatalf("Run = %v, hook ran %d times, want 2", err, n)
	}
}

func TestEngine_RebaseShiftsQueuedEventsToTheNewClock(t *testing.T) {
	e := NewEngine(NewStepClock(t0), 1)
	var at []time.Time
	e.At(t0.Add(time.Hour), func(context.Context) { at = append(at, e.Now()) })
	e.At(t0.Add(2*time.Hour), func(context.Context) { at = append(at, e.Now()) })
	t1 := t0.Add(10 * time.Hour)
	e.Rebase(NewStepClock(t1))
	if err := e.Run(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(at) != 2 || !at[0].Equal(t1.Add(time.Hour)) || !at[1].Equal(t1.Add(2*time.Hour)) {
		t.Errorf("steps ran at %v, want t1+1h and t1+2h", at)
	}
}

func TestEngine_NudgeSignalsWake(t *testing.T) {
	e := NewEngine(NewStepClock(t0), 1)
	e.Nudge()
	select {
	case <-e.Wake():
	default:
		t.Fatal("Nudge did not signal Wake")
	}
}
