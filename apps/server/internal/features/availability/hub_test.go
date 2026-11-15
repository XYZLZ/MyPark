package availability_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/XYZLZ/MyPark/apps/server/internal/features/availability"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/clock"
	"github.com/XYZLZ/MyPark/apps/server/internal/testsupport/fakeclock"
)

var errDown = errors.New("database down")

// fakeSource answers with the latest version the test published and records
// when each query ran on the fake clock. It can fail the next queries, and
// it can block queries until the test releases them.
type fakeSource struct {
	clk *fakeclock.Clock

	mu          sync.Mutex
	version     int64
	snapCalls   []time.Time
	spaceCalls  int
	failSnap    int // fail the next n Snapshot calls
	failSpaces  int // fail the next n Spaces calls
	blockSnap   bool
	blockSpaces bool
	started     chan struct{} // a blocked query has read its state
	release     chan struct{} // releases one blocked query
}

func (s *fakeSource) Snapshot(ctx context.Context, _ int64) (availability.Snapshot, error) {
	s.mu.Lock()
	s.snapCalls = append(s.snapCalls, s.clk.Now())
	snap := availability.Snapshot{Version: s.version, Status: availability.StatusOpen}
	fail, block := s.failSnap > 0, s.blockSnap
	if fail {
		s.failSnap--
	}
	s.mu.Unlock()
	if block {
		s.wait(ctx)
	}
	if fail {
		return availability.Snapshot{}, errDown
	}
	return snap, nil
}

func (s *fakeSource) Spaces(ctx context.Context, _ int64) ([]availability.SpaceState, error) {
	s.mu.Lock()
	s.spaceCalls++
	fail, block := s.failSpaces > 0, s.blockSpaces
	if fail {
		s.failSpaces--
	}
	s.mu.Unlock()
	if block {
		s.wait(ctx)
	}
	if fail {
		return nil, errDown
	}
	out := make([]availability.SpaceState, 280)
	for i := range out {
		out[i] = availability.SpaceState{Code: fmt.Sprintf("E%03d", i+1), State: "free"}
	}
	return out, nil
}

// wait signals a blocked query and waits for its release, or for Close.
func (s *fakeSource) wait(ctx context.Context) {
	select {
	case s.started <- struct{}{}:
	case <-ctx.Done():
		return
	}
	select {
	case <-s.release:
	case <-ctx.Done():
	}
}

func (s *fakeSource) set(f func(s *fakeSource)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f(s)
}

func (s *fakeSource) setVersion(v int64) { s.set(func(s *fakeSource) { s.version = v }) }

func (s *fakeSource) calls() []time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]time.Time(nil), s.snapCalls...)
}

func (s *fakeSource) spacesCalls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.spaceCalls
}

func newTestHub(t *testing.T) (*availability.Hub, *fakeSource, *fakeclock.Clock) {
	t.Helper()
	clk := fakeclock.New(time.Date(2026, 11, 24, 14, 0, 0, 0, time.UTC))
	src := &fakeSource{clk: clk, started: make(chan struct{}), release: make(chan struct{})}
	h := availability.NewHub(src, clk, slog.New(slog.NewTextHandler(io.Discard, nil)))
	t.Cleanup(h.Close)
	return h, src, clk
}

// bump publishes version v as the new lot state, notifies the hub and waits
// for the pushes it started.
func bump(h *availability.Hub, src *fakeSource, lot, v int64) {
	src.setVersion(v)
	h.LotChanged(lot, v)
	h.WaitIdle()
}

// advance moves fake time and waits for the pushes the timers started.
func advance(h *availability.Hub, clk *fakeclock.Clock, d time.Duration) {
	clk.Advance(d)
	h.WaitIdle()
}

func readVersion(t *testing.T, s *availability.Subscriber) int64 {
	t.Helper()
	select {
	case b := <-s.Availability():
		var snap availability.Snapshot
		if err := json.Unmarshal(b, &snap); err != nil {
			t.Fatalf("payload %q: %v", b, err)
		}
		return snap.Version
	default:
		t.Fatal("no availability payload")
		return 0
	}
}

// nextSpaces reads a spaces message the hub already sent (after WaitIdle).
func nextSpaces(t *testing.T, s *availability.Subscriber) availability.SpacesMsg {
	t.Helper()
	select {
	case msg := <-s.Spaces():
		return msg
	default:
		t.Fatal("no spaces message")
		return availability.SpacesMsg{}
	}
}

func assertSpacing(t *testing.T, calls []time.Time) {
	t.Helper()
	for i := 1; i < len(calls); i++ {
		if gap := calls[i].Sub(calls[i-1]); gap < availability.CoalesceWindow {
			t.Errorf("pushes %d and %d are %v apart, want ≥ %v", i-1, i, gap, availability.CoalesceWindow)
		}
	}
}

func TestHub_BurstCollapsesToTwoPushes(t *testing.T) {
	h, src, clk := newTestHub(t)
	sub, _ := h.Subscribe(1, false)

	bump(h, src, 1, 1)
	for v := int64(2); v <= 50; v++ {
		advance(h, clk, 4*time.Millisecond) // 49 bumps over 196 ms
		bump(h, src, 1, v)
	}
	advance(h, clk, time.Second)

	if got := len(src.calls()); got != 2 {
		t.Fatalf("Snapshot calls = %d, want 2 (leading + trailing)", got)
	}
	if v := readVersion(t, sub); v != 50 {
		t.Fatalf("subscriber read version %d, want the latest (50)", v)
	}
	if n := len(sub.Availability()); n != 0 {
		t.Fatalf("%d payloads left after the latest", n)
	}
}

func TestHub_AtMostFourPushesPerSecond(t *testing.T) {
	h, src, clk := newTestHub(t)
	h.Subscribe(1, false)

	for i := range 100 { // every 10 ms for 1 s
		bump(h, src, 1, int64(i+1))
		advance(h, clk, 10*time.Millisecond)
	}
	advance(h, clk, time.Second)

	calls := src.calls()
	if len(calls) < 4 {
		t.Fatalf("Snapshot calls = %d, want the bursts to keep pushing", len(calls))
	}
	assertSpacing(t, calls)
}

func TestHub_OneQueryPerLotNotPerSubscriber(t *testing.T) {
	h, src, _ := newTestHub(t)
	subs := make([]*availability.Subscriber, 100)
	for i := range subs {
		subs[i], _ = h.Subscribe(1, i%2 == 0)
	}

	bump(h, src, 1, 7)

	if got := len(src.calls()); got != 1 {
		t.Fatalf("Snapshot calls = %d, want 1", got)
	}
	var first []byte
	for i, s := range subs {
		select {
		case b := <-s.Availability():
			if first == nil {
				first = b
			} else if &b[0] != &first[0] {
				t.Fatalf("subscriber %d got its own encoding, want the shared payload", i)
			}
		default:
			t.Fatalf("subscriber %d got no payload", i)
		}
	}
}

func TestHub_SpacesDeltaAndOverflowResync(t *testing.T) {
	h, src, _ := newTestHub(t)
	sub, _ := h.Subscribe(1, true)
	plain, _ := h.Subscribe(1, false)

	for _, code := range []string{"E001", "E002", "E117"} {
		h.SpaceChanged(1, code, "occupied")
	}
	for _, code := range []string{"E001", "E002", "E117"} {
		msg := <-sub.Spaces()
		if msg.Full || len(msg.Spaces) != 1 || msg.Spaces[0] != (availability.SpaceState{Code: code, State: "occupied"}) {
			t.Fatalf("delta = %+v, want %s occupied", msg, code)
		}
	}
	if n := len(plain.Spaces()); n != 0 {
		t.Fatalf("subscriber without spaces got %d messages", n)
	}

	for i := range availability.SpacesQueueCap + 1 {
		h.SpaceChanged(1, fmt.Sprintf("E%03d", i%280+1), "free")
	}
	h.WaitIdle()
	msg := <-sub.Spaces()
	if !msg.Full || len(msg.Spaces) != 280 {
		t.Fatalf("after overflow got full=%v with %d spaces, want a full resync of 280", msg.Full, len(msg.Spaces))
	}
	if n := len(sub.Spaces()); n != 0 {
		t.Fatalf("%d messages queued after the resync", n)
	}
	if n := src.spacesCalls(); n != 1 {
		t.Fatalf("Spaces calls = %d, want 1", n)
	}

	h.SpaceChanged(1, "E005", "out_of_service")
	if msg := <-sub.Spaces(); msg.Full || msg.Spaces[0].Code != "E005" {
		t.Fatalf("after resync got %+v, want deltas again", msg)
	}
}

func TestHub_ResyncSendsFreshSnapshots(t *testing.T) {
	h, src, _ := newTestHub(t)
	src.setVersion(9)
	withSpaces, _ := h.Subscribe(1, true)
	plain, _ := h.Subscribe(1, false)
	other, _ := h.Subscribe(2, false)
	h.SpaceChanged(1, "E001", "occupied") // a stale delta the resync replaces

	h.Resync()
	h.WaitIdle()

	for name, s := range map[string]*availability.Subscriber{"withSpaces": withSpaces, "plain": plain, "other lot": other} {
		if v := readVersion(t, s); v != 9 {
			t.Errorf("%s: version %d, want 9", name, v)
		}
	}
	msg := <-withSpaces.Spaces()
	if !msg.Full || len(msg.Spaces) != 280 {
		t.Fatalf("resync spaces = full %v, %d spaces; want full 280", msg.Full, len(msg.Spaces))
	}
	if got := len(src.calls()); got != 2 {
		t.Errorf("Snapshot calls = %d, want one per lot (2)", got)
	}
	if n := src.spacesCalls(); n != 1 {
		t.Errorf("Spaces calls = %d, want 1 (only lot 1 wants spaces)", n)
	}
}

func TestHub_CloseEndsSubscribers(t *testing.T) {
	h, src, clk := newTestHub(t)
	sub, unsubscribe := h.Subscribe(1, true)
	gone, leave := h.Subscribe(2, false)
	leave()
	leave() // idempotent
	if _, ok := <-gone.Availability(); ok {
		t.Fatal("unsubscribed channel still open")
	}
	bump(h, src, 2, 1)
	if got := len(src.calls()); got != 0 {
		t.Fatalf("a lot without subscribers was queried %d times", got)
	}

	bump(h, src, 1, 1)
	bump(h, src, 1, 2) // arms the trailing timer
	h.Close()
	h.Close()

	select {
	case <-h.Done():
	default:
		t.Fatal("Done not closed")
	}
	for range sub.Availability() { // drains the pushed payload, then ends
	}
	if _, ok := <-sub.Spaces(); ok {
		t.Fatal("spaces channel still open")
	}
	unsubscribe()

	advance(h, clk, time.Hour)
	h.LotChanged(1, 3)
	h.SpaceChanged(1, "E001", "free")
	h.Resync()
	h.WaitIdle()
	if got := len(src.calls()); got != 1 {
		t.Fatalf("Snapshot calls = %d after Close, want only the leading push (1)", got)
	}

	late, unsub := h.Subscribe(1, true)
	defer unsub()
	if _, ok := <-late.Availability(); ok {
		t.Fatal("Subscribe after Close returned an open subscriber")
	}
	if _, ok := <-late.Spaces(); ok {
		t.Fatal("Subscribe after Close returned open spaces")
	}
}

func TestHub_RetriesFailedSnapshot(t *testing.T) {
	h, src, clk := newTestHub(t)
	sub, _ := h.Subscribe(1, false)
	src.set(func(s *fakeSource) { s.failSnap = 1 })

	bump(h, src, 1, 1) // fails: nothing to deliver
	if n := len(sub.Availability()); n != 0 {
		t.Fatalf("a failed snapshot delivered %d payloads", n)
	}
	src.setVersion(2) // the lot moves on while the database is down

	advance(h, clk, availability.CoalesceWindow)

	if v := readVersion(t, sub); v != 2 {
		t.Fatalf("after the retry the subscriber read version %d, want 2", v)
	}
	if got := len(src.calls()); got != 2 {
		t.Fatalf("Snapshot calls = %d, want the failure and one retry", got)
	}
	advance(h, clk, time.Hour)
	if got := len(src.calls()); got != 2 {
		t.Fatalf("Snapshot calls = %d after a successful retry, want no more", got)
	}
}

func TestHub_RetriesFailedSpacesFetch(t *testing.T) {
	h, src, clk := newTestHub(t)
	sub, _ := h.Subscribe(1, true)
	src.set(func(s *fakeSource) { s.failSpaces = 1 })

	for i := range availability.SpacesQueueCap + 1 {
		h.SpaceChanged(1, fmt.Sprintf("E%03d", i%280+1), "free")
	}
	h.WaitIdle()
	if n := len(sub.Spaces()); n != 0 {
		t.Fatalf("%d messages after a failed full fetch, want none", n)
	}
	h.SpaceChanged(1, "E009", "occupied") // still owed a full snapshot: not sent
	h.WaitIdle()
	if n := len(sub.Spaces()); n != 0 {
		t.Fatalf("a delta reached a subscriber that is owed a full snapshot")
	}

	advance(h, clk, availability.CoalesceWindow)

	msg := <-sub.Spaces()
	if !msg.Full || len(msg.Spaces) != 280 {
		t.Fatalf("after the retry got full=%v with %d spaces, want full 280", msg.Full, len(msg.Spaces))
	}
	if n := src.spacesCalls(); n != 2 {
		t.Fatalf("Spaces calls = %d, want the failure and one retry", n)
	}
}

// TestHub_PushRequestedInFlight: a push requested while a slow query runs
// reruns afterwards with the latest version, never goes backwards, and
// still keeps pushes ≥ 250 ms apart.
func TestHub_PushRequestedInFlight(t *testing.T) {
	h, src, clk := newTestHub(t)
	sub, _ := h.Subscribe(1, false)
	src.set(func(s *fakeSource) { s.blockSnap = true })

	src.setVersion(1)
	h.LotChanged(1, 1) // leading push at 0, blocked in the query
	<-src.started
	clk.Advance(100 * time.Millisecond)
	src.setVersion(2)
	h.LotChanged(1, 2) // trailing edge at 250 ms, while the query is in flight
	clk.Advance(150 * time.Millisecond)
	clk.Advance(150 * time.Millisecond) // the slow query finishes at 400 ms

	src.set(func(s *fakeSource) { s.blockSnap = false })
	src.release <- struct{}{}
	h.WaitIdle()
	var versions []int64
	versions = append(versions, readVersion(t, sub))

	advance(h, clk, 100*time.Millisecond) // 500 ms: the deferred rerun
	versions = append(versions, readVersion(t, sub))
	advance(h, clk, 10*time.Millisecond)
	bump(h, src, 1, 3) // 510 ms: too close to the rerun, trailing at 750 ms
	advance(h, clk, time.Second)
	versions = append(versions, readVersion(t, sub))

	if want := []int64{1, 2, 3}; fmt.Sprint(versions) != fmt.Sprint(want) {
		t.Fatalf("versions = %v, want %v (latest last, never backwards)", versions, want)
	}
	calls := src.calls()
	if len(calls) != 3 {
		t.Fatalf("Snapshot calls = %d (%v), want 3", len(calls), calls)
	}
	assertSpacing(t, calls)
}

// TestHub_DeltaDuringFullFetchIsReplayed: a delta that arrives while the full
// snapshot is being read comes after it, never before it or lost.
func TestHub_DeltaDuringFullFetchIsReplayed(t *testing.T) {
	h, src, _ := newTestHub(t)
	sub, _ := h.Subscribe(1, true)
	src.set(func(s *fakeSource) { s.blockSpaces = true })

	for i := range availability.SpacesQueueCap + 1 {
		h.SpaceChanged(1, fmt.Sprintf("E%03d", i%280+1), "free")
	}
	<-src.started
	h.SpaceChanged(1, "E117", "occupied")
	if n := len(sub.Spaces()); n != 0 {
		t.Fatalf("%d messages before the full snapshot", n)
	}
	src.release <- struct{}{}
	h.WaitIdle()

	if msg := nextSpaces(t, sub); !msg.Full || len(msg.Spaces) != 280 {
		t.Fatalf("first message full=%v with %d spaces, want the full snapshot", msg.Full, len(msg.Spaces))
	}
	msg := nextSpaces(t, sub)
	if msg.Full || len(msg.Spaces) != 1 || msg.Spaces[0] != (availability.SpaceState{Code: "E117", State: "occupied"}) {
		t.Fatalf("second message %+v, want the replayed E117 delta", msg)
	}
}

// TestHub_RefetchLoopIsBounded: resyncs that keep invalidating the full fetch
// stop the immediate refetches after MaxRefetches; a scheduled one follows.
func TestHub_RefetchLoopIsBounded(t *testing.T) {
	h, src, clk := newTestHub(t)
	sub, _ := h.Subscribe(1, true)
	src.set(func(s *fakeSource) { s.blockSpaces = true })

	h.Resync()
	for range availability.MaxRefetches + 1 {
		<-src.started
		h.Resync() // invalidates the fetch in flight
		src.release <- struct{}{}
	}
	h.WaitIdle()
	if n := src.spacesCalls(); n != availability.MaxRefetches+1 {
		t.Fatalf("Spaces calls = %d, want %d before backing off", n, availability.MaxRefetches+1)
	}
	if n := len(sub.Spaces()); n != 0 {
		t.Fatalf("%d spaces messages from invalidated fetches", n)
	}

	src.set(func(s *fakeSource) { s.blockSpaces = false })
	advance(h, clk, availability.CoalesceWindow)
	if msg := <-sub.Spaces(); !msg.Full || len(msg.Spaces) != 280 {
		t.Fatalf("scheduled refetch sent full=%v with %d spaces", msg.Full, len(msg.Spaces))
	}
}

// TestHub_ConcurrentUse drives every entry point at once on real timers, for
// the race detector: no data race, no send on a closed channel, and Close
// ends every subscriber and leaves no goroutine behind.
func TestHub_ConcurrentUse(t *testing.T) {
	baseline := runtime.NumGoroutine()
	clk := fakeclock.New(time.Date(2026, 11, 24, 14, 0, 0, 0, time.UTC))
	h := availability.NewHub(&fakeSource{clk: clk}, clock.System{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	var readers, writers sync.WaitGroup
	for i := range 20 {
		s, unsubscribe := h.Subscribe(int64(i%3), i%2 == 0)
		readers.Go(func() {
			for range s.Availability() {
			}
		})
		readers.Go(func() {
			for range s.Spaces() {
			}
		})
		if i%5 == 0 {
			writers.Go(unsubscribe)
		}
	}
	for g := range 4 {
		writers.Go(func() {
			for i := range 200 {
				lot := int64((g + i) % 3)
				h.LotChanged(lot, int64(i))
				h.SpaceChanged(lot, fmt.Sprintf("E%03d", i%280+1), "free")
				if i%50 == 0 {
					h.Resync()
				}
			}
		})
	}
	writers.Wait()
	h.Close()
	readers.Wait() // every channel closed
	<-h.Done()
	// Close waits for the hub's pushes: nothing it started is left.
	guard := time.After(5 * time.Second)
	for runtime.NumGoroutine() > baseline {
		select {
		case <-guard:
			t.Fatalf("%d goroutines after Close, baseline %d", runtime.NumGoroutine(), baseline)
		case <-time.After(5 * time.Millisecond):
		}
	}
}

// TestHub_SlowQueryDoesNotBlockCaller: the listener goroutine calls the hub;
// a slow query of one lot must not hold it, so other lots keep flowing.
func TestHub_SlowQueryDoesNotBlockCaller(t *testing.T) {
	h, src, _ := newTestHub(t)
	slow, _ := h.Subscribe(1, false)
	h.Subscribe(2, false)
	src.set(func(s *fakeSource) { s.blockSnap = true })

	returned := make(chan struct{})
	go func() {
		h.LotChanged(1, 1)
		close(returned)
	}()
	<-src.started // lot 1's query is in flight
	select {
	case <-returned:
	case <-time.After(5 * time.Second):
		t.Fatal("LotChanged blocked on a slow query")
	}
	h.Resync() // returns too, while lot 1 is still blocked
	<-src.started
	src.release <- struct{}{}
	src.release <- struct{}{}
	h.WaitIdle()
	if v := readVersion(t, slow); v != 0 {
		t.Fatalf("version %d", v)
	}
}
