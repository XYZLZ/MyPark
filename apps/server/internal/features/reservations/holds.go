package reservations

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/XYZLZ/MyPark/apps/server/internal/features/availability"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/database"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/database/dbgen"
)

// Holds is the availability.ReservationHolds adapter (design 8.4). It reads
// through the context transaction when there is one, so the gate sees the
// holds under the lot lock.
type Holds struct{ pool *pgxpool.Pool }

var _ availability.ReservationHolds = Holds{}

// NewHolds returns the holds adapter over pool.
func NewHolds(pool *pgxpool.Pool) Holds { return Holds{pool: pool} }

func (h Holds) q(ctx context.Context) *dbgen.Queries { return dbgen.New(database.Conn(ctx, h.pool)) }

func tstz(t time.Time) pgtype.Timestamptz { return pgtype.Timestamptz{Time: t, Valid: true} }

// EffectiveHolds counts the confirmed reservations whose hold window contains
// now. A pending_payment reservation never counts (it does not change what the
// public sees).
func (h Holds) EffectiveHolds(ctx context.Context, lotID int64, now time.Time) (int, error) {
	n, err := h.q(ctx).CountEffectiveHolds(ctx, dbgen.CountEffectiveHoldsParams{LotID: lotID, Now: tstz(now)})
	if err != nil {
		return 0, fmt.Errorf("reservations: count effective holds: %w", err)
	}
	return int(n), nil
}

// UpcomingArrivals counts the confirmed arrivals in (now, now+within].
func (h Holds) UpcomingArrivals(ctx context.Context, lotID int64, now time.Time, within time.Duration) (int, error) {
	n, err := h.q(ctx).CountUpcomingArrivals(ctx, dbgen.CountUpcomingArrivalsParams{
		LotID: lotID, Now: tstz(now), Until: tstz(now.Add(within)),
	})
	if err != nil {
		return 0, fmt.Errorf("reservations: count upcoming arrivals: %w", err)
	}
	return int(n), nil
}
