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
//
// At may be called from any goroutine. An event earlier than the one Run is
// waiting for wakes the wait, and Wake signals an event added after Run
// returned on an empty queue, so a caller can keep the engine alive.
type Engine struct {
	clock Clock
	rng   *rand.Rand
	wake  chan struct{}
	after func()

	mu       sync.Mutex
	q        queue
	seq      uint64
	waitAt   time.Time          // the event Run is waiting for; zero when not waiting
	waitStop context.CancelFunc // ends that wait
}

// NewEngine returns an engine on virtual clock c, seeded with seed.
func NewEngine(c Clock, seed uint64) *Engine {
	return &Engine{clock: c, rng: rand.New(rand.NewPCG(seed, seed^pcgStream)), wake: make(chan struct{}, 1)}
}

// At schedules step at virtual time t. Steps may call At.
func (e *Engine) At(t time.Time, step func(ctx context.Context)) {
	e.mu.Lock()
	heap.Push(&e.q, event{at: t, seq: e.seq, step: step})
	e.seq++
	if e.waitStop != nil && t.Before(e.waitAt) {
		e.waitStop()
	}
	e.mu.Unlock()
	select {
	case e.wake <- struct{}{}:
	default:
	}
}

// Wake receives a value after At was called. A stale value is harmless: the
// receiver finds the queue empty again and waits for the next one.
func (e *Engine) Wake() <-chan struct{} { return e.wake }

// SetAfterStep sets a function Run calls after every step, in the Run
// goroutine. Call it before Run.
func (e *Engine) SetAfterStep(f func()) { e.after = f }

// arm returns the earliest event's time and a context that At cancels when an
// earlier event arrives. It reports false on an empty queue.
func (e *Engine) arm(ctx context.Context) (time.Time, context.Context, context.CancelFunc, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.q) == 0 {
		return time.Time{}, nil, nil, false
	}
	wctx, cancel := context.WithCancel(ctx)
	e.waitAt, e.waitStop = e.q[0].at, cancel
	return e.waitAt, wctx, cancel, true
}

func (e *Engine) pop() event {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.waitAt, e.waitStop = time.Time{}, nil
	return heap.Pop(&e.q).(event)
}

// Run pops events in order, waits for the clock to reach each, and runs its
// step, until the queue is empty (nil) or ctx is done (ctx.Err()).
func (e *Engine) Run(ctx context.Context) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		at, wctx, cancel, ok := e.arm(ctx)
		if !ok {
			return nil
		}
		err := e.clock.WaitUntil(wctx, at)
		cancel()
		if err != nil {
			if ctx.Err() == nil && wctx.Err() != nil {
				continue // an earlier event arrived: wait for that one instead
			}
			return err
		}
		// The queue head is due: the event waited for, or an earlier one.
		e.pop().step(ctx)
		if e.after != nil {
			e.after()
		}
	}
}

// Now returns the virtual time.
func (e *Engine) Now() time.Time { return e.clock.Now() }

// Rand returns the engine's seeded generator. Use it only from steps.
func (e *Engine) Rand() *rand.Rand { return e.rng }
