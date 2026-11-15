package availability

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/XYZLZ/MyPark/apps/server/internal/domain/apperr"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/lots"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/clock"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/ratelimit"
	"github.com/XYZLZ/MyPark/apps/server/internal/transport/http/httpx"
	"github.com/XYZLZ/MyPark/apps/server/internal/transport/http/reqctx"
	"github.com/XYZLZ/MyPark/apps/server/internal/transport/http/router"
	"github.com/XYZLZ/MyPark/apps/server/internal/transport/http/sse"
)

// Stream timing (design §11.3, §11.4).
const (
	// HeartbeatEvery is the period of the heartbeat event.
	HeartbeatEvery = 15 * time.Second
	// WriteTimeout bounds each write of a stream. A client that stops
	// reading is dropped only once the kernel send buffer is full and a
	// write then blocks for this long; until then writes succeed.
	WriteTimeout = 10 * time.Second
)

// Wire values of the stream.
const (
	retryMS         = 3000
	capsRetryAfter  = 10 * time.Second
	includeSpacesQS = "include=spaces"
)

// streamPattern is the route of the availability stream.
const streamPattern = "GET /v1/lots/{id}/availability/stream"

// StreamDeps are what the stream handler needs. Service reads the initial
// snapshot; Timers drive the heartbeat and Clock stamps its as_of.
type StreamDeps struct {
	Hub     *Hub
	Service *Service
	Lots    lots.Resolver
	Caps    *StreamCaps
	Timers  clock.Timers
	Clock   clock.Clock
	Log     *slog.Logger
}

// RegisterStream adds the stream route to r: public, rate limited per IP on
// connect (sse_connect_ip), then capped by StreamCaps in the handler.
func RegisterStream(r *router.Router, h http.HandlerFunc) {
	r.Handle(streamPattern, router.Policy{Public: true, Limit: &ratelimit.SSEConnectIP}, h)
}

// NewStreamHandler serves the availability stream (design §11.3): retry,
// the current snapshot (the id is its version), the full spaces when the
// query is exactly include=spaces, then hub pushes and heartbeats until the
// client leaves, a write fails or the hub closes. Every error before the
// first frame is a JSON envelope: 404 for an unknown lot, 429 over the caps.
func NewStreamHandler(d StreamDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		lotID, ok := resolveLot(w, r, d.Lots, d.Log)
		if !ok {
			return
		}
		release, ok := d.Caps.Acquire(reqctx.ClientIP(r.Context()))
		if !ok {
			httpx.WriteError(w, r, d.Log, apperr.RateLimited(capsRetryAfter))
			return
		}
		defer release()
		logLastEventID(d.Log, r)

		// Subscribe first: a change committed while the snapshot is read is
		// then pushed, and pushes not newer than the snapshot are dropped.
		withSpaces := r.URL.RawQuery == includeSpacesQS
		sub, unsubscribe := d.Hub.Subscribe(lotID, withSpaces)
		defer unsubscribe()

		ctx := r.Context()
		snap, err := d.Service.Snapshot(ctx, lotID)
		if err != nil {
			httpx.WriteError(w, r, d.Log, err)
			return
		}
		snapJSON, err := json.Marshal(snap)
		if err != nil {
			httpx.WriteError(w, r, d.Log, err)
			return
		}
		var spacesJSON []byte
		if withSpaces {
			spaces, err := d.Service.Spaces(ctx, lotID)
			if err == nil {
				if spaces == nil {
					spaces = []SpaceState{}
				}
				spacesJSON, err = json.Marshal(SpacesMsg{Full: true, Spaces: spaces})
			}
			if err != nil {
				httpx.WriteError(w, r, d.Log, err)
				return
			}
		}
		sw, err := sse.New(w, WriteTimeout)
		if err != nil {
			httpx.WriteError(w, r, d.Log, err)
			return
		}
		// Before the first write, so a heartbeat is due one period after the
		// client sees the snapshot.
		ticker := d.Timers.NewTicker(HeartbeatEvery)
		defer ticker.Stop()

		sent := snap.Version
		err = sw.Retry(retryMS)
		if err == nil {
			err = sw.Event("availability", strconv.FormatInt(sent, 10), snapJSON)
		}
		if err == nil && spacesJSON != nil {
			err = sw.Event("spaces", "", spacesJSON)
		}
		for err == nil {
			select {
			case payload, ok := <-sub.Availability():
				if !ok {
					return
				}
				v, fresh := newerVersion(payload, sent)
				if !fresh {
					continue
				}
				sent = v
				err = sw.Event("availability", strconv.FormatInt(v, 10), payload)
			case msg, ok := <-sub.Spaces():
				if !ok {
					return
				}
				var b []byte
				if b, err = json.Marshal(msg); err == nil {
					err = sw.Event("spaces", "", b)
				}
			case <-ticker.C():
				err = heartbeat(sw, d.Clock.Now())
			case <-ctx.Done():
				return
			case <-d.Hub.Done():
				return
			}
		}
		d.Log.DebugContext(ctx, "availability stream: write failed, ending stream", "lot_id", lotID, "err", err)
	}
}

// newerVersion reads the version of a hub payload. ok is false when it is
// not newer than sent (an older push still in flight when the stream read
// its snapshot) or unreadable.
func newerVersion(payload []byte, sent int64) (int64, bool) {
	var p struct {
		Version int64 `json:"version"`
	}
	if json.Unmarshal(payload, &p) != nil || p.Version <= sent {
		return 0, false
	}
	return p.Version, true
}

func heartbeat(sw *sse.Writer, now time.Time) error {
	b, err := json.Marshal(struct {
		AsOf time.Time `json:"as_of"`
	}{now.UTC().Truncate(time.Second)})
	if err != nil {
		return err
	}
	return sw.Event("heartbeat", "", b)
}

// logLastEventID records a resume attempt. The id is never trusted: the
// stream always starts with the current snapshot (design §11.3), so a
// malformed id is only logged as such, and its raw value never is.
func logLastEventID(log *slog.Logger, r *http.Request) {
	raw := r.Header.Get("Last-Event-ID")
	if raw == "" {
		return
	}
	if v, err := strconv.ParseInt(raw, 10, 64); err == nil {
		log.DebugContext(r.Context(), "availability stream: resume", "last_event_id", v)
		return
	}
	log.DebugContext(r.Context(), "availability stream: resume with a malformed Last-Event-ID")
}
