// Package lots holds the parking lot feature. In M0 it only seeds the single
// pilot lot and its spaces.
package lots

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/database/dbgen"
)

// Pilot lot seed values. Latitude and longitude stay NULL until the real
// coordinates are confirmed (see the spec gaps).
const (
	seedSlug     = "zona-colonial"
	seedName     = "Parqueo Banreservas Zona Colonial"
	seedAddress  = "Calle Arzobispo Meriño No. 386, Zona Colonial, Santo Domingo"
	seedCapacity = 280
)

// Seed creates or updates the pilot lot, its spaces E001..E280 and its
// occupancy row, and returns the lot id. It is idempotent: running it again
// returns the same lot and creates nothing new.
//
// Seed runs its statements on q as given; bind q to a transaction (or use
// SeedTx) to make the whole seed atomic.
func Seed(ctx context.Context, q *dbgen.Queries) (lotID int64, err error) {
	lotID, err = q.UpsertLotBySlug(ctx, dbgen.UpsertLotBySlugParams{
		Slug:     seedSlug,
		Name:     seedName,
		Address:  seedAddress,
		Capacity: seedCapacity,
	})
	if err != nil {
		return 0, fmt.Errorf("seed: upsert lot %s: %w", seedSlug, err)
	}
	for i := range seedCapacity {
		code := spaceCode(i + 1)
		if err := q.InsertSpace(ctx, dbgen.InsertSpaceParams{LotID: lotID, Code: code}); err != nil {
			return 0, fmt.Errorf("seed: insert space %s: %w", code, err)
		}
	}
	if err := q.InitLotOccupancy(ctx, lotID); err != nil {
		return 0, fmt.Errorf("seed: init occupancy: %w", err)
	}
	return lotID, nil
}

// TxBeginner starts a transaction; *pgxpool.Pool and *pgx.Conn implement it.
type TxBeginner interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

// SeedTx runs Seed in a single transaction on db, so a failed seed leaves
// nothing half-written.
func SeedTx(ctx context.Context, db TxBeginner) (lotID int64, err error) {
	err = pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		lotID, err = Seed(ctx, dbgen.New(tx))
		return err
	})
	return lotID, err
}

// spaceCode formats space number n as E001, E002, ... E280.
func spaceCode(n int) string { return fmt.Sprintf("E%03d", n) }
