package reservations

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/XYZLZ/MyPark/apps/server/internal/features/vehicles"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/database"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/database/dbgen"
)

// VehicleChecker is the vehicles.ReservationChecker adapter (FR-VEH-4, design
// 7.11): a vehicle is in use while a pending_payment, confirmed or checked_in
// reservation points at it. It reads through the context transaction, so the
// delete's vehicle lock and this read share one snapshot.
type VehicleChecker struct{ pool *pgxpool.Pool }

var _ vehicles.ReservationChecker = VehicleChecker{}

// NewVehicleChecker returns the checker over pool.
func NewVehicleChecker(pool *pgxpool.Pool) VehicleChecker { return VehicleChecker{pool: pool} }

// HasActive implements vehicles.ReservationChecker.
func (c VehicleChecker) HasActive(ctx context.Context, vehicleID int64) (bool, error) {
	ok, err := dbgen.New(database.Conn(ctx, c.pool)).VehicleHasActiveReservation(ctx, vehicleID)
	if err != nil {
		return false, fmt.Errorf("reservations: check vehicle in use: %w", err)
	}
	return ok, nil
}
