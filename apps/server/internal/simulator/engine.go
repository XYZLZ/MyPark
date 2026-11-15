package simulator

import (
	"container/heap"
	"context"
	"math/rand/v2"
	"sync"
	"time"
)

// pcgStream decorrelates the two PCG words derived from one seed.
const pcgStream = 0x9E3779B97F4A7C15

type event struct {
	at   time.Time
	seq  uint64
	step func(ctx context.Context)
}

// queue is a min-heap keyed by (at, seq): ties run in scheduling order.
type queue []event

func (q queue) Len() int { return len(q) }
func (q queue) Less(i, j int) bool {
	if !q[i].at.Equal(q[j].at) {
		return q[i].at.Before(q[j].at)
	}
	return q[i].seq < q[j].seq
}
func (q queue) Swap(i, j int) { q[i], q[j] = q[j], q[i] }
func (q *queue) Push(x any)   { *q = append(*q, x.(event)) }
func (q *queue) Pop() any {
	old := *q
	e := old[len(old)-1]
	old[len(old)-1] = event{}
	*q = old[:len(old)-1]
	return e
}

// Engine is a deterministic discrete-event loop (FR-SIM-7). Every random draw
// happens in the Run goroutine, in step order, so one seed gives one event
// sequence. Server responses never feed the RNG.
type Engine struct {
	clock Clock
	rng   *rand.Rand

	mu  sync.Mutex
	q   queue
	seq uint64
}

// NewEngine returns an engine on virtual clock c, seeded with seed.
func NewEngine(c Clock, seed uint64) *Engine {
	return &Engine{clock: c, rng: rand.New(rand.NewPCG(seed, seed^pcgStream))}
}

// At schedules step at virtual time t. Steps may call At.
func (e *Engine) At(t time.Time, step func(ctx context.Context)) {
	e.mu.Lock()
	defer e.mu.Unlock()
	heap.Push(&e.q, event{at: t, seq: e.seq, step: step})
	e.seq++
}

func (e *Engine) pop() (event, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.q) == 0 {
		return event{}, false
	}
	return heap.Pop(&e.q).(event), true
}

// Run pops events in order, waits for the clock to reach each, and runs its
// step, until the queue is empty (nil) or ctx is done (ctx.Err()).
func (e *Engine) Run(ctx context.Context) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		ev, ok := e.pop()
		if !ok {
			return nil
		}
		if err := e.clock.WaitUntil(ctx, ev.at); err != nil {
			return err
		}
		ev.step(ctx)
	}
}

// Now returns the virtual time.
func (e *Engine) Now() time.Time { return e.clock.Now() }

// Rand returns the engine's seeded generator. Use it only from steps.
func (e *Engine) Rand() *rand.Rand { return e.rng }
