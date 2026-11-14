package gate

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/database"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/database/dbgen"
)

// voidBatch is the LIMIT of the VoidUnpassed query.
const voidBatch = 200

// VoidUnpassedEntries voids every entry_pending stay accepted more than 60 s
// before now, in batches of 200, and returns how many it voided (design
// §12.5, M2C-30). Each batch commits on its own and locks only the stays it
// claims (SKIP LOCKED), so it never waits on a pass holding one: that pass
// commits first and the stay is no longer pending. Occupancy is untouched,
// so nothing is notified. A car that still passes later is a late entry pass.
func VoidUnpassedEntries(ctx context.Context, db database.DBTX, now time.Time) (int64, error) {
	q := dbgen.New(db)
	var total int64
	for {
		n, err := q.VoidUnpassed(ctx, pgtype.Timestamptz{Time: now, Valid: true})
		if err != nil {
			return total, fmt.Errorf("gate: void unpassed entries: %w", err)
		}
		total += n
		if n < voidBatch {
			return total, nil
		}
	}
}
