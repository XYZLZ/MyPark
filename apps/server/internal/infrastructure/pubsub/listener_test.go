package pubsub

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/XYZLZ/MyPark/apps/server/internal/testsupport"
)

// syncBuffer is a log sink safe for the listener goroutine and the test.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// eventually polls cond until it holds or a 5 s guard expires.
func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	tick := time.NewTicker(5 * time.Millisecond)
	defer tick.Stop()
	guard := time.After(5 * time.Second)
	for !cond() {
		select {
		case <-tick.C:
		case <-guard:
			t.Fatalf("timed out waiting for %s", what)
		}
	}
}

func receive[T any](t *testing.T, ch <-chan T, what string) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
		var zero T
		return zero
	}
}

type spaceMsg struct {
	lot         int64
	code, state string
}

func TestListener_ReceivesAndReconnects(t *testing.T) {
	td := testsupport.NewTestDB(t)
	logs := &syncBuffer{}
	lotCh := make(chan [2]int64, 16)
	spaceCh := make(chan spaceMsg, 16)
	reconnected := make(chan struct{}, 16)
	l := New(td.App, slog.New(slog.NewTextHandler(logs, nil)), Handlers{
		LotChanged:   func(lot, v int64) { lotCh <- [2]int64{lot, v} },
		SpaceChanged: func(lot int64, code, state string) { spaceCh <- spaceMsg{lot, code, state} },
		Reconnected:  func() { reconnected <- struct{}{} },
	})
	// The backoff wait blocks until released, so the disconnected state is
	// observable without racing the reconnect.
	slept := make(chan time.Duration, 16)
	release := make(chan struct{})
	l.sleep = func(ctx context.Context, d time.Duration) error {
		slept <- d
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	baseline := runtime.NumGoroutine()
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- l.Run(ctx) }()
	stopped := false
	t.Cleanup(func() { // before the test database is dropped
		if !stopped {
			cancel()
			if err := <-done; !errors.Is(err, context.Canceled) {
				t.Errorf("Run = %v, want context.Canceled", err)
			}
		}
	})

	eventually(t, "the first connection", l.Connected)
	// Called on the first connect too: a hub that got subscribers while the
	// database was unreachable must resync.
	receive(t, reconnected, "Reconnected after the first connect")
	notify := func(channel, payload string) {
		t.Helper()
		if _, err := td.Super.Exec(t.Context(), "select pg_notify($1, $2)", channel, payload); err != nil {
			t.Fatalf("pg_notify: %v", err)
		}
	}

	notify("lot_changed", "7:42")
	if got := receive(t, lotCh, "lot_changed"); got != [2]int64{7, 42} {
		t.Fatalf("LotChanged%v, want (7, 42)", got)
	}
	notify("space_changed", "7:E117:occupied")
	if got := receive(t, spaceCh, "space_changed"); got != (spaceMsg{7, "E117", "occupied"}) {
		t.Fatalf("SpaceChanged = %+v", got)
	}

	// Notifications arrive in order, so once 7:43 is in, "x" was dropped.
	notify("lot_changed", "x")
	notify("lot_changed", "7:43")
	if got := receive(t, lotCh, "lot_changed after the malformed one"); got != [2]int64{7, 43} {
		t.Fatalf("LotChanged%v, want (7, 43): the malformed payload was not dropped", got)
	}
	if !strings.Contains(logs.String(), "level=WARN") {
		t.Errorf("malformed payload not logged at warn; logs:\n%s", logs.String())
	}

	var pid int32
	if err := td.Super.QueryRow(t.Context(),
		`select pid from pg_stat_activity where datname = current_database() and query ilike 'listen%'`).Scan(&pid); err != nil {
		t.Fatalf("find listener backend: %v", err)
	}
	if _, err := td.Super.Exec(t.Context(), "select pg_terminate_backend($1)", pid); err != nil {
		t.Fatalf("terminate listener: %v", err)
	}
	if d := receive(t, slept, "the backoff"); d != backoffMin {
		t.Errorf("first backoff = %v, want %v", d, backoffMin)
	}
	if l.Connected() {
		t.Fatal("Connected = true while the listener backs off")
	}
	if len(reconnected) != 0 {
		t.Fatal("Reconnected called before reconnecting")
	}
	close(release)
	receive(t, reconnected, "Reconnected")
	if !l.Connected() {
		t.Fatal("Connected = false after Reconnected")
	}
	notify("lot_changed", "7:44")
	if got := receive(t, lotCh, "lot_changed after reconnect"); got != [2]int64{7, 44} {
		t.Fatalf("LotChanged%v after reconnect, want (7, 44)", got)
	}
	if n := len(reconnected); n != 0 {
		t.Fatalf("Reconnected called %d extra times", n)
	}

	cancel()
	stopped = true
	if err := receive(t, done, "Run to return"); !errors.Is(err, context.Canceled) {
		t.Errorf("Run = %v, want context.Canceled", err)
	}
	if l.Connected() {
		t.Error("Connected = true after Run returned")
	}
	eventually(t, "listener goroutines to exit", func() bool { return runtime.NumGoroutine() <= baseline })
}

func TestParsePayloads(t *testing.T) {
	if lot, v, ok := parseLotChanged("7:42"); !ok || lot != 7 || v != 42 {
		t.Errorf("parseLotChanged(7:42) = %d, %d, %v", lot, v, ok)
	}
	for _, p := range []string{"", "x", "7", "7:", ":42", "7:x", "a:1", "7:42:1"} {
		if _, _, ok := parseLotChanged(p); ok {
			t.Errorf("parseLotChanged(%q) accepted", p)
		}
	}
	if lot, code, state, ok := parseSpaceChanged("7:E117:occupied"); !ok || lot != 7 || code != "E117" || state != "occupied" {
		t.Errorf("parseSpaceChanged = %d, %q, %q, %v", lot, code, state, ok)
	}
	for _, p := range []string{"", "x", "7:E117", "7::free", "7:E117:", "x:E117:free"} {
		if _, _, _, ok := parseSpaceChanged(p); ok {
			t.Errorf("parseSpaceChanged(%q) accepted", p)
		}
	}
}
