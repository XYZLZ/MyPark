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

// queryTimeout bounds one snapshot or spaces query of the hub.
const queryTimeout = 5 * time.Second

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
type lotState struct {
	id    int64
	subs  map[*Subscriber]struct{}
	co    Coalescer
	timer clock.Stopper

	pushing, pushAgain   bool // an availability query is in flight; rerun it after
	fetching, fetchAgain bool // a full spaces query is in flight; rerun it after
	pending              []SpaceState
}

// Hub fans lot changes out to SSE subscribers (design §11.4, M2C-39): per lot,
// leading and trailing edge coalescing, one snapshot query per push encoded
// once for every subscriber, and a bounded delta queue per subscriber for
// spaces. It owns no goroutine: work runs on the caller (the listener) or on
// a coalescing timer, queries run outside the lock, and the hub never blocks
// on a subscriber.
type Hub struct {
	src    SnapshotSource
	timers clock.Timers
	log    *slog.Logger
	ctx    context.Context // canceled by Close, ends in-flight queries
	cancel context.CancelFunc
	done   chan struct{}

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
		if ls.timer != nil {
			ls.timer.Stop()
		}
		delete(h.lots, ls.id)
	}
}

// LotChanged handles a lot_changed notification. The pushed payload carries
// the version read by the snapshot query, not this one.
func (h *Hub) LotChanged(lotID, _ int64) {
	h.mu.Lock()
	ls := h.lots[lotID]
	if h.closed || ls == nil {
		h.mu.Unlock()
		return
	}
	now := h.timers.Now()
	pushNow, trailingAt := ls.co.Bump(now)
	if !trailingAt.IsZero() {
		ls.timer = h.timers.AfterFunc(trailingAt.Sub(now), func() { h.trailing(ls) })
	}
	h.mu.Unlock()
	if pushNow {
		h.pushAvailability(ls)
	}
}

func (h *Hub) trailing(ls *lotState) {
	h.mu.Lock()
	ls.timer = nil
	fire := !h.closed && h.lots[ls.id] == ls && ls.co.Fire(h.timers.Now())
	h.mu.Unlock()
	if fire {
		h.pushAvailability(ls)
	}
}

// pushAvailability queries the lot once and offers the encoded snapshot to
// every subscriber. Pushes of one lot never overlap: a push requested while
// one is in flight reruns it once afterwards, so payloads stay in order.
func (h *Hub) pushAvailability(ls *lotState) {
	h.mu.Lock()
	if ls.pushing {
		ls.pushAgain = true
		h.mu.Unlock()
		return
	}
	ls.pushing = true
	h.mu.Unlock()
	for {
		ctx, cancel := context.WithTimeout(h.ctx, queryTimeout)
		snap, err := h.src.Snapshot(ctx, ls.id)
		cancel()
		var payload []byte
		if err == nil {
			payload, err = json.Marshal(snap)
		}
		h.mu.Lock()
		switch {
		case h.closed:
		case err != nil:
			h.log.Warn("availability hub: snapshot failed", "lot_id", ls.id, "err", err)
		default:
			for s := range ls.subs {
				s.offer(payload)
			}
		}
		if h.closed || !ls.pushAgain {
			ls.pushing = false
			h.mu.Unlock()
			return
		}
		ls.pushAgain = false
		h.mu.Unlock()
	}
}

// SpaceChanged handles a space_changed notification: a delta for every
// subscriber that wants spaces. A full queue is dropped and replaced by a
// full snapshot, so a slow subscriber never blocks the hub.
func (h *Hub) SpaceChanged(lotID int64, code, state string) {
	h.mu.Lock()
	ls := h.lots[lotID]
	if h.closed || ls == nil {
		h.mu.Unlock()
		return
	}
	delta := SpaceState{Code: code, State: state}
	anyNeedFull := false
	for s := range ls.subs {
		if !s.withSpaces {
			continue
		}
		if !s.needFull {
			select {
			case s.spaces <- SpacesMsg{Spaces: []SpaceState{delta}}:
				continue
			default:
				s.drainSpaces()
				s.needFull = true
			}
		}
		anyNeedFull = true
	}
	fetch := false
	switch {
	case ls.fetching:
		// Replayed after the full snapshot in flight, which may predate it.
		// Room is kept for that snapshot in the drained queues.
		if len(ls.pending) < SpacesQueueCap-1 {
			ls.pending = append(ls.pending, delta)
		} else {
			ls.fetchAgain = true
		}
	case anyNeedFull:
		fetch = true
	}
	h.mu.Unlock()
	if fetch {
		h.fetchSpaces(ls)
	}
}

// fetchSpaces queries the spaces of the lot once and sends the full snapshot,
// then the deltas received meanwhile, to every subscriber that needs it.
func (h *Hub) fetchSpaces(ls *lotState) {
	h.mu.Lock()
	if ls.fetching {
		ls.fetchAgain = true
		h.mu.Unlock()
		return
	}
	ls.fetching = true
	h.mu.Unlock()
	for {
		ctx, cancel := context.WithTimeout(h.ctx, queryTimeout)
		spaces, err := h.src.Spaces(ctx, ls.id)
		cancel()
		h.mu.Lock()
		switch {
		case h.closed || ls.fetchAgain: // stale: a resync or lost deltas ask for a fresh one
		case err != nil:
			h.log.Warn("availability hub: spaces failed", "lot_id", ls.id, "err", err)
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
		}
		ls.pending = nil
		if h.closed || !ls.fetchAgain {
			ls.fetching = false
			h.mu.Unlock()
			return
		}
		ls.fetchAgain = false
		h.mu.Unlock()
	}
}

// Resync sends a fresh availability snapshot to every subscriber and a full
// spaces snapshot to those that want spaces. The listener calls it after a
// reconnect, when notifications may have been missed.
func (h *Hub) Resync() {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return
	}
	type job struct {
		ls     *lotState
		spaces bool
	}
	jobs := make([]job, 0, len(h.lots))
	for _, ls := range h.lots {
		j := job{ls: ls}
		for s := range ls.subs {
			if s.withSpaces {
				s.drainSpaces()
				s.needFull, j.spaces = true, true
			}
		}
		jobs = append(jobs, j)
	}
	h.mu.Unlock()
	for _, j := range jobs {
		h.pushAvailability(j.ls)
		if j.spaces {
			h.fetchSpaces(j.ls)
		}
	}
}

// Close ends every subscriber, stops the coalescing timers and cancels
// in-flight queries. Later calls do nothing. Shutdown calls it before
// srv.Shutdown so open streams end at once.
func (h *Hub) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return
	}
	h.closed = true
	h.cancel()
	for _, ls := range h.lots {
		if ls.timer != nil {
			ls.timer.Stop()
		}
		for s := range ls.subs {
			s.close()
		}
	}
	clear(h.lots)
	close(h.done)
}

// Done is closed by Close.
func (h *Hub) Done() <-chan struct{} { return h.done }
