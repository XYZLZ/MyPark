// Package pubsub receives the Postgres notifications that drive the
// availability hub (design §11.6): lot_changed (payload lot_id:version) and
// space_changed (payload lot_id:code:state), both sent on commit.
package pubsub

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Reconnect backoff bounds; the delay doubles after each failed attempt.
const (
	backoffMin = 250 * time.Millisecond
	backoffMax = 5 * time.Second
)

// closeTimeout bounds closing the listening connection.
const closeTimeout = 5 * time.Second

// Handlers receive the notifications. They run on the listener goroutine, one
// at a time, in arrival order.
type Handlers struct {
	LotChanged   func(lotID, version int64)
	SpaceChanged func(lotID int64, code, state string)
	// Reconnected runs each time LISTEN is in place, the first connect
	// included: notifications sent while disconnected (or before the first
	// connect, if subscribers came first) are lost, so it resyncs. With no
	// subscribers it is a no-op.
	Reconnected func()
}

// Listener holds one dedicated connection, taken out of the pool, that
// LISTENs and waits for notifications.
type Listener struct {
	pool      *pgxpool.Pool
	log       *slog.Logger
	h         Handlers
	connected atomic.Bool
	// sleep waits out a backoff; tests replace it.
	sleep func(ctx context.Context, d time.Duration) error
}

// New returns a listener on a connection of pool. It does nothing until Run.
func New(pool *pgxpool.Pool, log *slog.Logger, h Handlers) *Listener {
	return &Listener{pool: pool, log: log, h: h, sleep: sleepCtx}
}

// Connected reports whether LISTEN is in place (the /readyz listener check).
func (l *Listener) Connected() bool { return l.connected.Load() }

// Run listens until ctx is done, reconnecting with backoff (250 ms doubling
// up to 5 s) after any error. It returns ctx's error.
func (l *Listener) Run(ctx context.Context) error {
	delay := backoffMin
	for {
		err := l.session(ctx, func() {
			delay = backoffMin
			l.h.Reconnected()
		})
		l.connected.Store(false)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		l.log.Warn("pubsub: listener disconnected", "err", err, "retry_in", delay)
		if err := l.sleep(ctx, delay); err != nil {
			return ctx.Err()
		}
		delay = min(delay*2, backoffMax)
	}
}

// session acquires and hijacks a connection, LISTENs, calls up, then
// dispatches notifications until an error.
func (l *Listener) session(ctx context.Context, up func()) error {
	pc, err := l.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire: %w", err)
	}
	conn := pc.Hijack()
	defer func() {
		cctx, cancel := context.WithTimeout(context.Background(), closeTimeout)
		defer cancel()
		_ = conn.Close(cctx)
	}()
	if _, err := conn.Exec(ctx, "listen lot_changed; listen space_changed"); err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	l.connected.Store(true)
	up()
	for {
		n, err := conn.WaitForNotification(ctx)
		if err != nil {
			return fmt.Errorf("wait: %w", err)
		}
		l.dispatch(n)
	}
}

func (l *Listener) dispatch(n *pgconn.Notification) {
	switch n.Channel {
	case "lot_changed":
		if lot, v, ok := parseLotChanged(n.Payload); ok {
			l.h.LotChanged(lot, v)
			return
		}
	case "space_changed":
		if lot, code, state, ok := parseSpaceChanged(n.Payload); ok {
			l.h.SpaceChanged(lot, code, state)
			return
		}
	}
	l.log.Warn("pubsub: malformed notification dropped", "channel", n.Channel, "payload_len", len(n.Payload))
}

// parseLotChanged parses "lot_id:version".
func parseLotChanged(p string) (lot, version int64, ok bool) {
	a, b, found := strings.Cut(p, ":")
	if !found {
		return 0, 0, false
	}
	lot, err1 := strconv.ParseInt(a, 10, 64)
	version, err2 := strconv.ParseInt(b, 10, 64)
	return lot, version, err1 == nil && err2 == nil
}

// parseSpaceChanged parses "lot_id:code:state".
func parseSpaceChanged(p string) (lot int64, code, state string, ok bool) {
	a, rest, found := strings.Cut(p, ":")
	if !found {
		return 0, "", "", false
	}
	code, state, found = strings.Cut(rest, ":")
	lot, err := strconv.ParseInt(a, 10, 64)
	if !found || err != nil || code == "" || state == "" || strings.Contains(state, ":") {
		return 0, "", "", false
	}
	return lot, code, state, true
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
