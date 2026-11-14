// Package postgres is the PostgreSQL adapter of the availability feature.
package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/XYZLZ/MyPark/apps/server/internal/features/availability"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/database"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/database/dbgen"
)

// Repo implements availability.Repo over dbgen.
type Repo struct{ pool *pgxpool.Pool }

var _ availability.Repo = (*Repo)(nil)

// NewRepo returns a Repo that reads through pool, or through the transaction
// carried by the context when there is one.
func NewRepo(pool *pgxpool.Pool) *Repo { return &Repo{pool: pool} }

// Counts implements availability.Repo.
func (r *Repo) Counts(ctx context.Context, lotID int64) (availability.SnapshotInput, error) {
	row, err := dbgen.New(database.Conn(ctx, r.pool)).GetLotSnapshot(ctx, lotID)
	if err != nil {
		return availability.SnapshotInput{}, fmt.Errorf("availability: snapshot of lot %d: %w", lotID, err)
	}
	return availability.SnapshotInput{
		LotPublicID:  row.PublicID,
		Capacity:     int(row.Capacity),
		OutOfService: int(row.OutOfService),
		Occupied:     int(row.Occupied),
		LotClosed:    row.Status == "closed",
		Version:      row.Version,
		UpdatedAt:    row.UpdatedAt.Time,
	}, nil
}

// Spaces implements availability.Repo.
func (r *Repo) Spaces(ctx context.Context, lotID int64) ([]availability.SpaceState, error) {
	rows, err := dbgen.New(database.Conn(ctx, r.pool)).ListSpaceStates(ctx, lotID)
	if err != nil {
		return nil, fmt.Errorf("availability: spaces of lot %d: %w", lotID, err)
	}
	out := make([]availability.SpaceState, len(rows))
	for i, row := range rows {
		out[i] = availability.SpaceState{Code: row.Code, State: row.Status}
	}
	return out, nil
}
