package auth

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/clock"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/database/dbgen"
)

// PurgeRetention is how long expired auth data is kept (policies 1.8).
const PurgeRetention = 30 * 24 * time.Hour

// Purger deletes expired authentication data. The worker runs it hourly.
type Purger interface {
	// Purge deletes auth codes and refresh tokens expired more than
	// PurgeRetention ago, and sessions revoked or expired more than
	// PurgeRetention ago together with their tokens.
	Purge(ctx context.Context) error
}

type pgPurger struct {
	pool  *pgxpool.Pool
	clock clock.Clock
}

// NewPurger returns the Postgres-backed Purger.
func NewPurger(pool *pgxpool.Pool, c clock.Clock) Purger { return &pgPurger{pool: pool, clock: c} }

// Purge implements Purger in one transaction, tokens before sessions (FK).
func (p *pgPurger) Purge(ctx context.Context) error {
	cutoff := pgtype.Timestamptz{Time: p.clock.Now().Add(-PurgeRetention), Valid: true}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("auth: purge: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := dbgen.New(tx)
	for _, step := range []struct {
		name string
		run  func(context.Context, pgtype.Timestamptz) (int64, error)
	}{
		{"codes", q.PurgeAuthCodes},
		{"refresh tokens", q.PurgeRefreshTokens},
		{"sessions", q.PurgeAuthSessions},
	} {
		if _, err := step.run(ctx, cutoff); err != nil {
			return fmt.Errorf("auth: purge %s: %w", step.name, err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("auth: purge: commit: %w", err)
	}
	return nil
}
