package notifications

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/clock"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/database/dbgen"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/email"
)

// Dispatch tuning (spec 11.3).
const (
	// batchSize is how many messages one Tick delivers at most. Each is
	// claimed on its own, right before its send, so its lease starts when the
	// send starts.
	batchSize = 10
	// leaseTTL must stay above the SMTP exchange timeout (30 s) with margin.
	leaseTTL    = time.Minute
	maxAttempts = 8

	backoffBase = 5 * time.Second
	backoffMax  = 15 * time.Minute
	// maxErrorLen caps outbox_messages.last_error.
	maxErrorLen = 500
)

// Backoff returns the delay before retrying a message that has failed
// attempts times: min(5s * 2^attempts, 15m).
func Backoff(attempts int) time.Duration {
	if attempts >= 20 { // 5s * 2^20 is far past the cap; avoids shift overflow
		return backoffMax
	}
	return min(backoffBase<<max(attempts, 0), backoffMax)
}

// Dispatcher delivers outbox messages: claim a batch in a short transaction,
// send outside any transaction, then record the outcome. Delivery is
// at-least-once (OC-24): a worker that crashes after the SMTP acknowledgement
// but before the update leaves a lease that expires and the message is sent
// again, with the same Message-ID.
type Dispatcher struct {
	pool  *pgxpool.Pool
	send  email.Sender
	clock clock.Clock
	from  string
	log   *slog.Logger
}

// NewDispatcher returns a Dispatcher that sends from the address from.
func NewDispatcher(pool *pgxpool.Pool, s email.Sender, c clock.Clock, from string, log *slog.Logger) *Dispatcher {
	return &Dispatcher{pool: pool, send: s, clock: c, from: from, log: log}
}

// Tick claims up to one batch of due messages and delivers them. It returns
// how many were sent. A message that fails is rescheduled with backoff, or
// marked dead after the 8th attempt; that is not an error of Tick. When ctx
// is canceled the in-flight message keeps its lease and is retried later.
func (d *Dispatcher) Tick(ctx context.Context) (sent int, err error) {
	for range batchSize {
		now := d.clock.Now()
		rows, err := dbgen.New(d.pool).ClaimOutboxBatch(ctx, dbgen.ClaimOutboxBatchParams{
			Now:        pgtype.Timestamptz{Time: now, Valid: true},
			LeaseUntil: pgtype.Timestamptz{Time: now.Add(leaseTTL), Valid: true},
			Batch:      1,
		})
		if err != nil {
			return sent, fmt.Errorf("notifications: claim outbox: %w", err)
		}
		if len(rows) == 0 {
			return sent, nil
		}
		ok, err := d.deliver(ctx, rows[0])
		if err != nil {
			return sent, err
		}
		if ok {
			sent++
		}
	}
	return sent, nil
}

// deliver sends one claimed message and records the outcome. It returns
// (false, nil) when the send failed and was rescheduled or buried.
func (d *Dispatcher) deliver(ctx context.Context, row dbgen.ClaimOutboxBatchRow) (bool, error) {
	q := dbgen.New(d.pool)
	sendErr := d.sendRow(ctx, row)
	if ctx.Err() != nil {
		// Shutting down: leave the lease in place instead of burning an attempt.
		return false, nil
	}
	if sendErr == nil {
		n, err := q.MarkOutboxSent(ctx, dbgen.MarkOutboxSentParams{
			ID: row.ID, Attempts: row.Attempts, Now: pgtype.Timestamptz{Time: d.clock.Now(), Valid: true},
		})
		if err != nil {
			return false, fmt.Errorf("notifications: mark sent: %w", err)
		}
		if n == 0 {
			d.lostLease(row)
			return false, nil
		}
		d.log.Info("outbox message sent", "id", row.ID, "template", row.Template, "attempts", row.Attempts)
		return true, nil
	}

	reason := truncate(redact(sendErr.Error()), maxErrorLen)
	attrs := []any{"id", row.ID, "template", row.Template, "attempts", row.Attempts, "error", reason}
	last := pgtype.Text{String: reason, Valid: true}
	if row.Attempts >= maxAttempts {
		d.log.Error("outbox message dead", attrs...)
		n, err := q.MarkOutboxDead(ctx, dbgen.MarkOutboxDeadParams{ID: row.ID, Attempts: row.Attempts, LastError: last})
		if err != nil {
			return false, fmt.Errorf("notifications: mark dead: %w", err)
		}
		if n == 0 {
			d.lostLease(row)
		}
		return false, nil
	}
	d.log.Warn("outbox message failed, will retry", attrs...)
	n, err := q.MarkOutboxRetry(ctx, dbgen.MarkOutboxRetryParams{
		ID:          row.ID,
		Attempts:    row.Attempts,
		LastError:   last,
		AvailableAt: pgtype.Timestamptz{Time: d.clock.Now().Add(Backoff(int(row.Attempts))), Valid: true},
	})
	if err != nil {
		return false, fmt.Errorf("notifications: mark retry: %w", err)
	}
	if n == 0 {
		d.lostLease(row)
	}
	return false, nil
}

// lostLease logs a finish that matched no row: the lease expired and another
// claim owns the message now, so this outcome is dropped untouched.
func (d *Dispatcher) lostLease(row dbgen.ClaimOutboxBatchRow) {
	d.log.Warn("outbox lease lost, outcome discarded", "id", row.ID, "template", row.Template, "attempts", row.Attempts)
}

func (d *Dispatcher) sendRow(ctx context.Context, row dbgen.ClaimOutboxBatchRow) error {
	var data map[string]string
	if err := json.Unmarshal(row.Payload, &data); err != nil {
		return errors.New("outbox payload is not a string map")
	}
	subject, text, html, err := Render(Template(row.Template), row.Locale, data)
	if err != nil {
		return err
	}
	return d.send.Send(ctx, email.Message{
		From: d.from, To: row.ToAddress, Subject: subject, Text: text, HTML: html,
		MessageID: email.MessageID(row.DedupeKey),
	})
}

// addressLike matches email addresses so SMTP replies that echo the
// recipient never reach the database or the logs.
var addressLike = regexp.MustCompile(`[^\s<>"']+@[^\s<>"']+`)

func redact(s string) string { return addressLike.ReplaceAllString(s, "[address]") }

// truncate cuts s to at most n bytes on a rune boundary.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !isRuneStart(s[n]) {
		n--
	}
	return s[:n]
}

func isRuneStart(b byte) bool { return b&0xC0 != 0x80 }
