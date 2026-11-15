package availability

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/clock"
)

// SpacesQueueCap is the per-subscriber queue of space deltas. A subscriber
// that falls this far behind gets one full spaces snapshot instead (M2C-39).
const SpacesQueueCap = 300

// queryTimeout bounds one snapshot or spaces query of the hub. It stays
// well inside the 2 s push budget (M2 exit criterion).
const queryTimeout = 1500 * time.Millisecond

// maxRefetches bounds the immediate refetches of a full spaces snapshot that
// a resync or lost deltas invalidated; after that the refetch is scheduled
// one CoalesceWindow later, so the loop cannot livelock.
const maxRefetches = 3

// SnapshotSource reads the lot state the hub fans out. *Service implements it.
type SnapshotSource interface {
	Snapshot(ctx context.Context, lotID int64) (Snapshot, error)
	Spaces(ctx context.Context, lotID int64) ([]SpaceState, error)
}

// SpacesMsg is the payload of an SSE spaces event: the full list, or the
// spaces that changed.
type SpacesMsg struct {
	Full   bool         `json:"full"`
	Spaces []SpaceState `json:"spaces"`
}

// Subscriber receives the pushes of one lot. Both channels close when the
// subscriber leaves or the hub closes.
type Subscriber struct {
	withSpaces bool
	avail      chan []byte
	spaces     chan SpacesMsg

	// Guarded by Hub.mu.
	needFull bool // the next spaces message must be a full snapshot
	closed   bool
}

// Availability yields encoded availability snapshots. It holds one payload:
// a newer push replaces an unread one (latest wins).
func (s *Subscriber) Availability() <-chan []byte { return s.avail }

// Spaces yields space deltas and full snapshots, only for subscribers that
// asked for spaces.
func (s *Subscriber) Spaces() <-chan SpacesMsg { return s.spaces }

// offer replaces the unread payload, if any. Hub.mu is held, so the hub is
// the only sender and the send cannot block.
func (s *Subscriber) offer(payload []byte) {
	select {
	case <-s.avail:
	default:
	}
	s.avail <- payload
}

// drainSpaces drops every queued spaces message. Hub.mu is held.
func (s *Subscriber) drainSpaces() {
	for {
		select {
		case <-s.spaces:
		default:
			return
		}
	}
}

func (s *Subscriber) close() {
	if !s.closed {
		s.closed = true
		close(s.avail)
		close(s.spaces)
	}
}

// lotState is the hub state of one lot with subscribers. Guarded by Hub.mu.
// lotState is the hub state of one lot with subscribers. Guarded by Hub.mu.
type lotState struct {
	id   int64
	subs map[*Subscriber]struct{}
	co   Coalescer

	timer       clock.Stopper // trailing edge
	retry       clock.Stopper // availability retry after a failed query
	spacesRetry clock.Stopper // full spaces refetch after a failure or backoff

	pushing, pushAgain   bool // an availability query is in flight; push again after it
	fetching, fetchAgain bool // a full spaces query is in flight; its result is stale
	refetches            int  // immediate refetches since the last delivered full snapshot
	pending              []SpaceState
}

func (ls *lotState) stopTimers() {
	for _, t := range []clock.Stopper{ls.timer, ls.retry, ls.spacesRetry} {
		if t != nil {
			t.Stop()
		}
	}
	ls.timer, ls.retry, ls.spacesRetry = nil, nil, nil
}

func (ls *lotState) anyNeedFull() bool {
	for s := range ls.subs {
		if s.needFull {
			return true
		}
	}
	return false
}

// Hub fans lot changes out to SSE subscribers (design §11.4, M2C-39): per lot,
// leading and trailing edge coalescing, one snapshot query per push encoded
// once for every subscriber, and a bounded delta queue per subscriber for
// spaces. Queries run on their own goroutine, one at a time per lot, so a
// slow lot never holds the caller (the listener) or other lots. A failed
// query is retried one CoalesceWindow later. The hub never blocks on a
// subscriber.
type Hub struct {
	src    SnapshotSource
	timers clock.Timers
	log    *slog.Logger
	ctx    context.Context // canceled by Close, ends in-flight queries
	cancel context.CancelFunc
	done   chan struct{}
	wg     sync.WaitGroup // query goroutines; Add only under mu while open

	mu     sync.Mutex
	lots   map[int64]*lotState
	closed bool
}

// NewHub returns a hub that reads lot state from src and coalesces on t.
func NewHub(src SnapshotSource, t clock.Timers, log *slog.Logger) *Hub {
	ctx, cancel := context.WithCancel(context.Background())
	return &Hub{src: src, timers: t, log: log, ctx: ctx, cancel: cancel,
		done: make(chan struct{}), lots: make(map[int64]*lotState)}
}

// Subscribe registers a subscriber for the lot. The stream sends the initial
// snapshot itself; the hub delivers only later changes. The returned func
// unsubscribes and is idempotent. After Close the subscriber is closed.
func (h *Hub) Subscribe(lotID int64, withSpaces bool) (*Subscriber, func()) {
	s := &Subscriber{
		withSpaces: withSpaces,
		avail:      make(chan []byte, 1),
		spaces:     make(chan SpacesMsg, SpacesQueueCap),
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		s.close()
		return s, func() {}
	}
	ls := h.lots[lotID]
	if ls == nil {
		ls = &lotState{id: lotID, subs: make(map[*Subscriber]struct{})}
		h.lots[lotID] = ls
	}
	ls.subs[s] = struct{}{}
	return s, func() { h.unsubscribe(ls, s) }
}

func (h *Hub) unsubscribe(ls *lotState, s *Subscriber) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if s.closed {
		return
	}
	delete(ls.subs, s)
	s.close()
	if len(ls.subs) == 0 && h.lots[ls.id] == ls {
		ls.stopTimers()
		delete(h.lots, ls.id)
	}
}

// live reports whether ls is still the open hub's state for its lot. mu held.
func (h *Hub) live(ls *lotState) bool { return !h.closed && h.lots[ls.id] == ls }

// LotChanged handles a lot_changed notification. The pushed payload carries
// the version read by the snapshot query, not this one.
func (h *Hub) LotChanged(lotID, _ int64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if ls := h.lots[lotID]; ls != nil && !h.closed {
		h.bump(ls)
	}
}

// bump runs a change through the coalescer: push now, or arm the trailing
// edge. Every push goes through here, so pushes stay CoalesceWindow apart.
// mu held.
func (h *Hub) bump(ls *lotState) {
	now := h.timers.Now()
	pushNow, trailingAt := ls.co.Bump(now)
	if !trailingAt.IsZero() {
		ls.timer = h.timers.AfterFunc(trailingAt.Sub(now), func() { h.trailing(ls) })
	}
	if pushNow {
		h.startPush(ls)
	}
}

func (h *Hub) trailing(ls *lotState) {
	h.mu.Lock()
	defer h.mu.Unlock()
	ls.timer = nil
	if h.live(ls) && ls.co.Fire(h.timers.Now()) {
		h.startPush(ls)
	}
}

func (h *Hub) retryPush(ls *lotState) {
	h.mu.Lock()
	defer h.mu.Unlock()
	ls.retry = nil
	if h.live(ls) {
		h.bump(ls)
	}
}

// startPush starts the availability query of the lot, or, when one is in
// flight, asks for another push after it. mu held.
func (h *Hub) startPush(ls *lotState) {
	if ls.pushing {
		ls.pushAgain = true
		return
	}
	ls.pushing = true
	h.wg.Add(1)
	go h.push(ls)
}

// push queries the lot once and offers the encoded snapshot to every
// subscriber. A push asked for meanwhile goes back through the coalescer; a
// failure is retried one CoalesceWindow later.
func (h *Hub) push(ls *lotState) {
	defer h.wg.Done()
	ctx, cancel := context.WithTimeout(h.ctx, queryTimeout)
	snap, err := h.src.Snapshot(ctx, ls.id)
	cancel()
	var payload []byte
	if err == nil {
		payload, err = json.Marshal(snap)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	ls.pushing = false
	again := ls.pushAgain
	ls.pushAgain = false
	if !h.live(ls) {
		return
	}
	if err != nil {
		h.log.Warn("availability hub: snapshot failed, retrying", "lot_id", ls.id, "err", err)
		if ls.retry == nil {
			ls.retry = h.timers.AfterFunc(CoalesceWindow, func() { h.retryPush(ls) })
		}
		return
	}
	for s := range ls.subs {
		s.offer(payload)
	}
	if again {
		h.bump(ls)
	}
}

// SpaceChanged handles a space_changed notification: a delta for every
// subscriber that wants spaces. A full queue is dropped and replaced by a
// full snapshot, so a slow subscriber never blocks the hub.
func (h *Hub) SpaceChanged(lotID int64, code, state string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	ls := h.lots[lotID]
	if h.closed || ls == nil {
		return
	}
	delta := SpaceState{Code: code, State: state}
	for s := range ls.subs {
		if !s.withSpaces || s.needFull {
			continue
		}
		select {
		case s.spaces <- SpacesMsg{Spaces: []SpaceState{delta}}:
		default:
			s.drainSpaces()
			s.needFull = true
		}
	}
	switch {
	case ls.fetching:
		// Replayed after the full snapshot in flight, which may predate it.
		// Room is kept for that snapshot in the drained queues.
		if len(ls.pending) < SpacesQueueCap-1 {
			ls.pending = append(ls.pending, delta)
		} else {
			ls.fetchAgain = true
		}
	case ls.spacesRetry == nil && ls.anyNeedFull():
		h.startFetch(ls)
	}
}

// startFetch starts the full spaces query of the lot, or marks the one in
// flight stale. mu held.
func (h *Hub) startFetch(ls *lotState) {
	if ls.fetching {
		ls.fetchAgain = true
		return
	}
	ls.fetching = true
	ls.pending = nil
	h.wg.Add(1)
	go h.fetch(ls)
}

func (h *Hub) retryFetch(ls *lotState) {
	h.mu.Lock()
	defer h.mu.Unlock()
	ls.spacesRetry = nil
	if h.live(ls) && ls.anyNeedFull() {
		h.startFetch(ls)
	}
}

// fetch queries the spaces of the lot once and sends the full snapshot, then
// the deltas received meanwhile, to every subscriber that needs it. A stale
// result is refetched at once up to maxRefetches times, then one
// CoalesceWindow later, as is a failed one.
func (h *Hub) fetch(ls *lotState) {
	defer h.wg.Done()
	ctx, cancel := context.WithTimeout(h.ctx, queryTimeout)
	spaces, err := h.src.Spaces(ctx, ls.id)
	cancel()
	h.mu.Lock()
	defer h.mu.Unlock()
	stale := ls.fetchAgain
	ls.fetchAgain = false
	if !h.live(ls) {
		ls.fetching, ls.pending = false, nil
		return
	}
	switch {
	case err != nil:
		h.log.Warn("availability hub: spaces failed, retrying", "lot_id", ls.id, "err", err)
	case stale && ls.refetches < maxRefetches:
		ls.refetches++
		ls.pending = nil
		h.wg.Add(1)
		go h.fetch(ls) // still fetching
		return
	case stale:
		// Keeps changing under the fetch: back off instead of looping.
	default:
		for s := range ls.subs {
			if !s.needFull {
				continue
			}
			s.drainSpaces()
			s.spaces <- SpacesMsg{Full: true, Spaces: spaces}
			for _, d := range ls.pending {
				s.spaces <- SpacesMsg{Spaces: []SpaceState{d}}
			}
			s.needFull = false
		}
		ls.fetching, ls.pending, ls.refetches = false, nil, 0
		return
	}
	ls.fetching, ls.pending, ls.refetches = false, nil, 0
	if ls.spacesRetry == nil {
		ls.spacesRetry = h.timers.AfterFunc(CoalesceWindow, func() { h.retryFetch(ls) })
	}
}

// Resync sends a fresh availability snapshot to every subscriber and a full
// spaces snapshot to those that want spaces. The listener calls it after
// every connect, since notifications may have been missed.
func (h *Hub) Resync() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return
	}
	for _, ls := range h.lots {
		h.bump(ls)
		spaces := false
		for s := range ls.subs {
			if s.withSpaces {
				s.drainSpaces()
				s.needFull, spaces = true, true
			}
		}
		if spaces {
			h.startFetch(ls)
		}
	}
}

// Close ends every subscriber, stops the timers, cancels in-flight queries
// and waits for them. Later calls do nothing. Shutdown calls it before
// srv.Shutdown so open streams end at once.
func (h *Hub) Close() {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return
	}
	h.closed = true
	h.cancel()
	for _, ls := range h.lots {
		ls.stopTimers()
		for s := range ls.subs {
			s.close()
		}
	}
	clear(h.lots)
	h.mu.Unlock()
	h.wg.Wait()
	close(h.done)
}

// Done is closed once Close has ended every subscriber and query.
func (h *Hub) Done() <-chan struct{} { return h.done }
