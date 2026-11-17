package gate

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
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

// pendingWindow is how long an accepted entry waits for its barrier pass
// (M2C-30).
const pendingWindow = 60 * time.Second

// errSkipVoid rolls back a reservation void whose stay changed under it.
var errSkipVoid = errors.New("gate: reservation stay no longer stale")

// ReservationVoider voids reservation stays not passed within 60 s, one
// transaction per stay, restoring a checked-in reservation's hold in the
// same transaction (design 12.7, M3B-60).
type ReservationVoider struct {
	Tx           database.TxRunnerPort
	Repo         Repo
	Reservations Reservations
	Log          *slog.Logger
}

// Run voids at most one batch of stale reservation stays and returns how
// many it voided. The candidates are read without locks; each is re-checked
// under the design 8.1 order: the lot, the reservation, its slot loads, the
// stay. A stay passed or re-opened meanwhile is skipped; any other failure
// is logged and left for the next tick.
func (v *ReservationVoider) Run(ctx context.Context, db database.DBTX, now time.Time) (int64, error) {
	rows, err := dbgen.New(db).ReservationStaysToVoid(ctx, pgtype.Timestamptz{Time: now, Valid: true})
	if err != nil {
		return 0, fmt.Errorf("gate: reservation stays to void: %w", err)
	}
	var voided int64
	for _, row := range rows {
		if ctx.Err() != nil {
			return voided, ctx.Err()
		}
		err := v.Tx.InTx(ctx, func(ctx context.Context) error { return v.voidOne(ctx, row, now) })
		switch {
		case errors.Is(err, errSkipVoid):
		case err != nil:
			v.Log.ErrorContext(ctx, "gate: reservation stay not voided, retrying next tick", "stay_id", row.ID, "err", err)
		default:
			voided++
		}
	}
	return voided, nil
}

func (v *ReservationVoider) voidOne(ctx context.Context, row dbgen.ReservationStaysToVoidRow, now time.Time) error {
	if _, err := v.Repo.LockLotOccupancy(ctx, row.LotID); err != nil {
		return err
	}
	restored, err := v.Reservations.RestoreAfterVoid(ctx, row.ReservationID, now)
	if err != nil {
		return err
	}
	stay, err := v.Repo.StayForUpdateByID(ctx, row.LotID, row.ID)
	if err != nil {
		return err
	}
	if stay.Status != StatusEntryPending || !stay.AcceptedAt.Before(now.Add(-pendingWindow)) {
		return errSkipVoid
	}
	if err := v.Repo.VoidStay(ctx, row.LotID, row.ID, now); err != nil {
		return err
	}
	if !restored { // cancelled or expired meanwhile: only the stay, as in M2
		return nil
	}
	version, err := v.Repo.BumpOccupancy(ctx, row.LotID, 0, now)
	if err != nil {
		return err
	}
	return v.Repo.NotifyLotChanged(ctx, row.LotID, version)
}
