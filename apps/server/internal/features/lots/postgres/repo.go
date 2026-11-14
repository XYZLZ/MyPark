// Package postgres is the PostgreSQL adapter of the lots feature.
package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/XYZLZ/MyPark/apps/server/internal/features/lots"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/database"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/database/dbgen"
)

// Repo implements lots.Resolver over dbgen.
type Repo struct{ pool *pgxpool.Pool }

var _ lots.Resolver = (*Repo)(nil)

// NewRepo returns a Repo that reads through pool, or through the transaction
// carried by the context when there is one.
func NewRepo(pool *pgxpool.Pool) *Repo { return &Repo{pool: pool} }

// LotIDByPublicID implements lots.Resolver.
func (r *Repo) LotIDByPublicID(ctx context.Context, id uuid.UUID) (int64, error) {
	lotID, err := dbgen.New(database.Conn(ctx, r.pool)).GetLotIDByPublicID(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, lots.ErrNotFound
	}
	if err != nil {
		return 0, fmt.Errorf("lots: resolve public id: %w", err)
	}
	return lotID, nil
}

var _ lots.Repo = (*Repo)(nil)

// List implements lots.Repo.
func (r *Repo) List(ctx context.Context) ([]lots.Summary, error) {
	rows, err := dbgen.New(database.Conn(ctx, r.pool)).ListLots(ctx)
	if err != nil {
		return nil, fmt.Errorf("lots: list: %w", err)
	}
	out := make([]lots.Summary, len(rows))
	for i, row := range rows {
		out[i] = lots.Summary{ID: row.PublicID, Name: row.Name, Slug: row.Slug}
	}
	return out, nil
}

// ByPublicID implements lots.Repo.
func (r *Repo) ByPublicID(ctx context.Context, id uuid.UUID) (lots.Lot, error) {
	row, err := dbgen.New(database.Conn(ctx, r.pool)).GetLotByPublicID(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return lots.Lot{}, lots.ErrNotFound
	}
	if err != nil {
		return lots.Lot{}, fmt.Errorf("lots: get: %w", err)
	}
	var hours lots.OpeningHours
	if err := json.Unmarshal(row.OpeningHours, &hours); err != nil {
		return lots.Lot{}, fmt.Errorf("lots: decode opening hours of lot %d: %w", row.ID, err)
	}
	return lots.Lot{
		ID: row.PublicID, Slug: row.Slug, Name: row.Name, Address: row.Address,
		Latitude: float(row.Latitude), Longitude: float(row.Longitude),
		Timezone: row.Timezone, Capacity: int(row.Capacity), OpeningHours: hours, InternalID: row.ID,
	}, nil
}

// float converts a nullable numeric to *float64.
func float(n pgtype.Numeric) *float64 {
	f, err := n.Float64Value()
	if err != nil || !f.Valid {
		return nil
	}
	return &f.Float64
}
