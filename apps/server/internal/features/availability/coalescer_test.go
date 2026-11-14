package availability_test

import (
	"testing"
	"time"

	"github.com/XYZLZ/MyPark/apps/server/internal/features/availability"
)

func TestCoalescer_LeadingAndTrailing(t *testing.T) {
	t0 := time.Date(2026, 11, 24, 14, 0, 0, 0, time.UTC)
	at := func(ms int) time.Time { return t0.Add(time.Duration(ms) * time.Millisecond) }
	var c availability.Coalescer

	if push, trailing := c.Bump(at(0)); !push || !trailing.IsZero() {
		t.Fatalf("Bump@0 = %v, %v; want leading push", push, trailing)
	}
	if push, trailing := c.Bump(at(100)); push || !trailing.Equal(at(250)) {
		t.Fatalf("Bump@100 = %v, %v; want trailing at 250ms", push, trailing)
	}
	if push, trailing := c.Bump(at(200)); push || !trailing.IsZero() {
		t.Fatalf("Bump@200 = %v, %v; want no new timer", push, trailing)
	}
	if !c.Fire(at(250)) {
		t.Fatal("Fire@250 = false")
	}
	if c.Fire(at(300)) {
		t.Fatal("Fire without a pending bump = true")
	}
	if push, _ := c.Bump(at(600)); !push {
		t.Fatal("Bump@600 = no push; want leading push")
	}
}
