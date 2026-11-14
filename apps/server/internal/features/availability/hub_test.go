package availability_test

import (
	"context"
	"encoding/json"
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

// fakeSource answers with the latest version the test published and records
// when each query ran on the fake clock.
type fakeSource struct {
	clk *fakeclock.Clock

	mu         sync.Mutex
	version    int64
	snapCalls  []time.Time
	spaceCalls int
}

func (s *fakeSource) Snapshot(context.Context, int64) (availability.Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.snapCalls = append(s.snapCalls, s.clk.Now())
	return availability.Snapshot{Version: s.version, Status: availability.StatusOpen}, nil
}

func (s *fakeSource) Spaces(context.Context, int64) ([]availability.SpaceState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.spaceCalls++
	out := make([]availability.SpaceState, 280)
	for i := range out {
		out[i] = availability.SpaceState{Code: fmt.Sprintf("E%03d", i+1), State: "free"}
	}
	return out, nil
}

func (s *fakeSource) setVersion(v int64) {
	s.mu.Lock()
	s.version = v
	s.mu.Unlock()
}

func (s *fakeSource) calls() []time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]time.Time(nil), s.snapCalls...)
}

func newTestHub(t *testing.T) (*availability.Hub, *fakeSource, *fakeclock.Clock) {
	t.Helper()
	clk := fakeclock.New(time.Date(2026, 11, 24, 14, 0, 0, 0, time.UTC))
	src := &fakeSource{clk: clk}
	h := availability.NewHub(src, clk, slog.New(slog.NewTextHandler(io.Discard, nil)))
	t.Cleanup(h.Close)
	return h, src, clk
}

// bump publishes version v as the new lot state and notifies the hub.
func bump(h *availability.Hub, src *fakeSource, lot, v int64) {
	src.setVersion(v)
	h.LotChanged(lot, v)
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

func TestHub_BurstCollapsesToTwoPushes(t *testing.T) {
	h, src, clk := newTestHub(t)
	sub, _ := h.Subscribe(1, false)

	bump(h, src, 1, 1)
	for v := int64(2); v <= 50; v++ {
		clk.Advance(4 * time.Millisecond) // 49 bumps over 196 ms
		bump(h, src, 1, v)
	}
	clk.Advance(time.Second)

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
		clk.Advance(10 * time.Millisecond)
	}
	clk.Advance(time.Second)

	calls := src.calls()
	if len(calls) < 4 {
		t.Fatalf("Snapshot calls = %d, want the bursts to keep pushing", len(calls))
	}
	for i := 1; i < len(calls); i++ {
		if gap := calls[i].Sub(calls[i-1]); gap < availability.CoalesceWindow {
			t.Errorf("pushes %d and %d are %v apart, want ≥ %v", i-1, i, gap, availability.CoalesceWindow)
		}
	}
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
	msg := <-sub.Spaces()
	if !msg.Full || len(msg.Spaces) != 280 {
		t.Fatalf("after overflow got full=%v with %d spaces, want a full resync of 280", msg.Full, len(msg.Spaces))
	}
	if n := len(sub.Spaces()); n != 0 {
		t.Fatalf("%d messages queued after the resync", n)
	}
	if src.spaceCalls != 1 {
		t.Fatalf("Spaces calls = %d, want 1", src.spaceCalls)
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
	if src.spaceCalls != 1 {
		t.Errorf("Spaces calls = %d, want 1 (only lot 1 wants spaces)", src.spaceCalls)
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

	clk.Advance(time.Hour)
	h.LotChanged(1, 3)
	h.SpaceChanged(1, "E001", "free")
	h.Resync()
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
	// The hub owns no goroutine: once Close returns, nothing it started is left.
	guard := time.After(5 * time.Second)
	for runtime.NumGoroutine() > baseline {
		select {
		case <-guard:
			t.Fatalf("%d goroutines after Close, baseline %d", runtime.NumGoroutine(), baseline)
		case <-time.After(5 * time.Millisecond):
		}
	}
}
