package ratelimit

import (
	"context"
	"crypto/sha256"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/clock"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/database"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/database/dbgen"
)

// Postgres is a fixed-window Limiter backed by the rate_limit_counters table,
// so every API instance shares the same counts (spec 10.2, OC-21).
type Postgres struct {
	q     *dbgen.Queries
	clock clock.Clock
}

// NewPostgres returns a Limiter over pool. Window boundaries and expiry come
// from c, never from the database clock.
func NewPostgres(pool *pgxpool.Pool, c clock.Clock) *Postgres {
	return &Postgres{q: dbgen.New(pool), clock: c}
}

// HashKey is the stored bucket key: sha256(rule.Prefix + key). Emails and IPs
// never reach the table or the logs in clear.
func HashKey(rule Rule, key string) []byte {
	sum := sha256.Sum256([]byte(rule.Prefix + key))
	return sum[:]
}

// Allow counts one attempt against the bucket of (rule, key). Every attempt
// counts, so the outcome never reveals which earlier attempts failed.
func (p *Postgres) Allow(ctx context.Context, rule Rule, key string) (Decision, error) {
	now := p.clock.Now()
	row, err := p.q.HitRateLimit(ctx, dbgen.HitRateLimitParams{
		Key:        HashKey(rule, key),
		WindowSize: pgtype.Interval{Microseconds: rule.Window.Microseconds(), Valid: true},
		Now:        pgtype.Timestamptz{Time: now, Valid: true},
	})
	if err != nil {
		return Decision{}, fmt.Errorf("ratelimit: hit %s: %w", rule.Name, err)
	}
	if int(row.Count) <= rule.Limit {
		return Decision{Allowed: true}, nil
	}
	// Whole seconds, rounded up, so a client never retries too early.
	wait := row.ExpiresAt.Time.Sub(now)
	return Decision{RetryAfter: (wait + time.Second - 1).Truncate(time.Second)}, nil
}

// Purge deletes the counters whose window ended before now and returns how
// many it removed. The worker runs it every minute.
func Purge(ctx context.Context, db database.DBTX, now time.Time) (int64, error) {
	n, err := dbgen.New(db).PurgeRateLimits(ctx, pgtype.Timestamptz{Time: now, Valid: true})
	if err != nil {
		return 0, fmt.Errorf("ratelimit: purge: %w", err)
	}
	return n, nil
}
