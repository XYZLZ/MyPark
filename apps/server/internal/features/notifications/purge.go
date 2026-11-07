package notifications

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/clock"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/database/dbgen"
)

// OutboxRetention is how long sent and dead outbox rows are kept.
const OutboxRetention = 90 * 24 * time.Hour

// PurgeOutbox deletes sent and dead messages older than OutboxRetention,
// measured from the injected clock, and returns how many it removed.
func PurgeOutbox(ctx context.Context, pool *pgxpool.Pool, c clock.Clock) (int64, error) {
	n, err := dbgen.New(pool).PurgeOutbox(ctx, pgtype.Timestamptz{Time: c.Now().Add(-OutboxRetention), Valid: true})
	if err != nil {
		return 0, fmt.Errorf("notifications: purge outbox: %w", err)
	}
	return n, nil
}
