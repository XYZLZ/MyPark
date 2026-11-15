package availability_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/XYZLZ/MyPark/apps/server/internal/features/availability"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/lots"
	"github.com/XYZLZ/MyPark/apps/server/internal/testsupport"
	"github.com/XYZLZ/MyPark/apps/server/internal/testsupport/fakeclock"
)

const streamLot = int64(7)

// blockingRepo answers Counts with version, after the test releases it when
// block is set. started tells the test the handler is reading its initial
// snapshot, so it has already subscribed.
type blockingRepo struct {
	version int64
	block   bool
	started chan struct{}
	release chan struct{}
}

func (r *blockingRepo) Counts(ctx context.Context, _ int64) (availability.SnapshotInput, error) {
	if r.block {
		r.started <- struct{}{}
		select {
		case <-r.release:
		case <-ctx.Done():
			return availability.SnapshotInput{}, ctx.Err()
		}
	}
	return availability.SnapshotInput{Capacity: 280, Version: r.version}, nil
}

func (r *blockingRepo) Spaces(context.Context, int64) ([]availability.SpaceState, error) {
	return []availability.SpaceState{{Code: "E001", State: "free"}}, nil
}

type oneLot struct{ id uuid.UUID }

func (l oneLot) LotIDByPublicID(_ context.Context, id uuid.UUID) (int64, error) {
	if id != l.id {
		return 0, lots.ErrNotFound
	}
	return streamLot, nil
}

type streamFix struct {
	url  string
	hub  *availability.Hub
	src  *fakeSource
	repo *blockingRepo
	clk  *fakeclock.Clock
}

func newStreamFix(t *testing.T, initialVersion int64) *streamFix {
	t.Helper()
	clk := fakeclock.New(time.Date(2026, 11, 24, 14, 0, 0, 0, time.UTC))
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	src := &fakeSource{clk: clk, started: make(chan struct{}), release: make(chan struct{})}
	hub := availability.NewHub(src, clk, log)
	repo := &blockingRepo{version: initialVersion, block: true, started: make(chan struct{}), release: make(chan struct{})}
	lot := uuid.New()
	mux := http.NewServeMux()
	mux.Handle("GET /lots/{id}/stream", availability.NewStreamHandler(availability.StreamDeps{
		Hub: hub, Service: availability.NewService(repo, availability.NoReservations{}, clk),
		Lots: oneLot{lot}, Caps: availability.NewStreamCaps(10, 10), Timers: clk, Clock: clk, Log: log,
	}))
	srv := httptest.NewServer(mux)
	// Cleanups run last first: the hub closes the streams, then the server
	// can close without waiting on them.
	t.Cleanup(srv.Close)
	t.Cleanup(hub.Close)
	return &streamFix{url: srv.URL + "/lots/" + lot.String() + "/stream", hub: hub, src: src, repo: repo, clk: clk}
}

// open starts the request on its own goroutine: the response headers come
// only with the first frame, after the blocked initial snapshot.
func (f *streamFix) open(t *testing.T) <-chan *http.Response {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	out := make(chan *http.Response, 1)
	go func() {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, f.url, nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			close(out)
			return
		}
		out <- resp
	}()
	return out
}

func frames(t *testing.T, respCh <-chan *http.Response) <-chan testsupport.SSEFrame {
	t.Helper()
	select {
	case resp, ok := <-respCh:
		if !ok {
			t.Fatal("stream request failed")
		}
		t.Cleanup(func() { resp.Body.Close() })
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200", resp.StatusCode)
		}
		return testsupport.ReadSSE(resp.Body)
	case <-time.After(5 * time.Second):
		t.Fatal("no response headers")
	}
	return nil
}

func waitStarted(t *testing.T, ch chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatal("the handler never read its initial snapshot")
	}
}

func wantAvailability(t *testing.T, f testsupport.SSEFrame, version int64) {
	t.Helper()
	var snap availability.Snapshot
	if err := json.Unmarshal([]byte(f.Data), &snap); err != nil {
		t.Fatalf("frame %+v: %v", f, err)
	}
	if f.Event != "availability" || f.ID != strconv.FormatInt(version, 10) || snap.Version != version {
		t.Fatalf("frame = %+v, want availability with id and version %d", f, version)
	}
}

// S11.1 carry: the stream subscribes before it reads its initial snapshot,
// so a change committed meanwhile is not lost.
func TestStream_SubscribesBeforeSnapshot(t *testing.T) {
	f := newStreamFix(t, 6)
	resp := f.open(t)
	waitStarted(t, f.repo.started)
	// The handler is subscribed: a push now reaches it.
	f.src.setVersion(7)
	f.hub.LotChanged(streamLot, 7)
	f.hub.WaitIdle()
	close(f.repo.release)

	fr := frames(t, resp)
	if got := testsupport.NextSSE(t, fr, 5*time.Second); got.Retry != "3000" {
		t.Fatalf("first frame = %+v, want retry 3000", got)
	}
	wantAvailability(t, testsupport.NextSSE(t, fr, 5*time.Second), 6)
	wantAvailability(t, testsupport.NextSSE(t, fr, 5*time.Second), 7)
}

// S11.1 carry: a push that is not newer than the initial snapshot (an older
// push still in flight when the stream read it) is dropped.
func TestStream_DropsPushesNotNewerThanSent(t *testing.T) {
	for _, hubVersion := range []int64{5, 6} {
		t.Run(strconv.FormatInt(hubVersion, 10), func(t *testing.T) {
			f := newStreamFix(t, 6)
			resp := f.open(t)
			waitStarted(t, f.repo.started)
			f.src.setVersion(hubVersion)
			f.hub.LotChanged(streamLot, hubVersion)
			f.hub.WaitIdle()
			close(f.repo.release)

			fr := frames(t, resp)
			testsupport.NextSSE(t, fr, 5*time.Second) // retry
			wantAvailability(t, testsupport.NextSSE(t, fr, 5*time.Second), 6)
			// Two heartbeats in a row: the stale payload was never written,
			// whichever the handler picked first.
			for i := range 2 {
				f.clk.Advance(availability.HeartbeatEvery)
				if got := testsupport.NextSSE(t, fr, 5*time.Second); got.Event != "heartbeat" {
					t.Fatalf("frame %d after the snapshot = %+v, want heartbeat", i+1, got)
				}
			}
		})
	}
}
