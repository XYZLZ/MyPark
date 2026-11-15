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
