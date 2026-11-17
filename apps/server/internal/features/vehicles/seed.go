package vehicles

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/database"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/database/dbgen"
)

// SeedVehicle gives the owner a first vehicle unless it already has a live
// one, so a rerun never duplicates a vehicle. It must run inside a
// transaction. The plate is validated with the registry's own rules.
func SeedVehicle(ctx context.Context, ownerID int64, plate, brand, model, color string, now time.Time) error {
	normalized, display, err := NormalizePlate(plate)
	if err != nil {
		return fmt.Errorf("vehicles: seed plate: %w", err)
	}
	tx, err := database.MustTx(ctx)
	if err != nil {
		return err
	}
	q := dbgen.New(tx)
	if _, err := q.LockVehicleOwner(ctx, ownerID); err != nil {
		return fmt.Errorf("vehicles: lock seed owner: %w", err)
	}
	n, err := q.CountActiveVehiclesOwned(ctx, ownerID)
	if err != nil {
		return fmt.Errorf("vehicles: count seed owner vehicles: %w", err)
	}
	if n > 0 {
		return nil
	}
	_, err = q.InsertVehicle(ctx, dbgen.InsertVehicleParams{
		OwnerID: ownerID, PlateNormalized: normalized, PlateDisplay: display,
		Make: brand, Model: model, Color: color, Now: pgtype.Timestamptz{Time: now, Valid: true},
	})
	if err != nil {
		return fmt.Errorf("vehicles: insert seed vehicle: %w", err)
	}
	return nil
}
