// Package postgres is the PostgreSQL adapter of the vehicles feature.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/XYZLZ/MyPark/apps/server/internal/features/vehicles"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/database"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/database/dbgen"
)

// plateActiveKey is the partial unique index on active plates.
const plateActiveKey = "vehicles_plate_active_key"

// Repo implements vehicles.Repo over dbgen. It works inside the transaction
// carried by the context, or on the pool when there is none.
type Repo struct{ pool *pgxpool.Pool }

var _ vehicles.Repo = (*Repo)(nil)

// NewRepo returns a Repo over pool.
func NewRepo(pool *pgxpool.Pool) *Repo { return &Repo{pool: pool} }

func (r *Repo) q(ctx context.Context) *dbgen.Queries {
	return dbgen.New(database.Conn(ctx, r.pool))
}

func ts(t time.Time) pgtype.Timestamptz { return pgtype.Timestamptz{Time: t, Valid: true} }

// safeError wraps err without the SQL text or bound arguments, which hold
// plates.
func safeError(op string, err error) error {
	if err == nil {
		return nil
	}
	if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok {
		return fmt.Errorf("%s: sqlstate %s constraint %q", op, pgErr.Code, pgErr.ConstraintName)
	}
	return fmt.Errorf("%s: %w", op, err)
}

func isPlateTaken(err error) bool {
	pgErr, ok := errors.AsType[*pgconn.PgError](err)
	return ok && pgErr.Code == "23505" && pgErr.ConstraintName == plateActiveKey
}

// row is the column set every returning query shares.
type row struct {
	ID              int64
	PublicID        uuid.UUID
	PlateNormalized string
	PlateDisplay    string
	Make            string
	Model           string
	Color           string
	CreatedAt       pgtype.Timestamptz
	UpdatedAt       pgtype.Timestamptz
}

func record(r row) vehicles.Record {
	return vehicles.Record{ID: r.ID, Vehicle: vehicles.Vehicle{
		PublicID: r.PublicID, Plate: r.PlateDisplay, PlateNormalized: r.PlateNormalized,
		Make: r.Make, Model: r.Model, Color: r.Color,
		CreatedAt: r.CreatedAt.Time.UTC(), UpdatedAt: r.UpdatedAt.Time.UTC(),
	}}
}

// LockOwner implements vehicles.Repo.
func (r *Repo) LockOwner(ctx context.Context, ownerID int64) error {
	_, err := r.q(ctx).LockVehicleOwner(ctx, ownerID)
	if errors.Is(err, pgx.ErrNoRows) {
		return vehicles.ErrNotFound
	}
	return safeError("vehicles: lock owner", err)
}

// CountActiveOwned implements vehicles.Repo.
func (r *Repo) CountActiveOwned(ctx context.Context, ownerID int64) (int, error) {
	n, err := r.q(ctx).CountActiveVehiclesOwned(ctx, ownerID)
	if err != nil {
		return 0, safeError("vehicles: count", err)
	}
	return int(n), nil
}

// Insert implements vehicles.Repo.
func (r *Repo) Insert(ctx context.Context, ownerID int64, f vehicles.Fields, now time.Time) (vehicles.Record, error) {
	v, err := r.q(ctx).InsertVehicle(ctx, dbgen.InsertVehicleParams{
		OwnerID: ownerID, PlateNormalized: f.PlateNormalized, PlateDisplay: f.PlateDisplay,
		Make: f.Make, Model: f.Model, Color: f.Color, Now: ts(now),
	})
	if isPlateTaken(err) {
		return vehicles.Record{}, vehicles.ErrPlateTaken
	}
	if err != nil {
		return vehicles.Record{}, safeError("vehicles: insert", err)
	}
	return record(row(v)), nil
}

// ListOwned implements vehicles.Repo.
func (r *Repo) ListOwned(ctx context.Context, ownerID int64) ([]vehicles.Record, error) {
	rows, err := r.q(ctx).ListVehiclesOwned(ctx, ownerID)
	if err != nil {
		return nil, safeError("vehicles: list", err)
	}
	out := make([]vehicles.Record, len(rows))
	for i, v := range rows {
		out[i] = record(row(v))
	}
	return out, nil
}

// GetOwned implements vehicles.Repo.
func (r *Repo) GetOwned(ctx context.Context, ownerID int64, id uuid.UUID) (vehicles.Record, error) {
	v, err := r.q(ctx).GetVehicleOwned(ctx, dbgen.GetVehicleOwnedParams{PublicID: id, OwnerID: ownerID})
	if errors.Is(err, pgx.ErrNoRows) {
		return vehicles.Record{}, vehicles.ErrNotFound
	}
	if err != nil {
		return vehicles.Record{}, safeError("vehicles: get", err)
	}
	return record(row(v)), nil
}

// UpdateOwned implements vehicles.Repo.
func (r *Repo) UpdateOwned(ctx context.Context, ownerID int64, id uuid.UUID, f vehicles.Fields, now time.Time) (vehicles.Record, error) {
	v, err := r.q(ctx).UpdateVehicleOwned(ctx, dbgen.UpdateVehicleOwnedParams{
		PublicID: id, OwnerID: ownerID, PlateNormalized: f.PlateNormalized, PlateDisplay: f.PlateDisplay,
		Make: f.Make, Model: f.Model, Color: f.Color, Now: ts(now),
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return vehicles.Record{}, vehicles.ErrNotFound
	case isPlateTaken(err):
		return vehicles.Record{}, vehicles.ErrPlateTaken
	case err != nil:
		return vehicles.Record{}, safeError("vehicles: update", err)
	}
	return record(row(v)), nil
}

// SoftDeleteOwned implements vehicles.Repo.
func (r *Repo) SoftDeleteOwned(ctx context.Context, ownerID int64, id uuid.UUID, now time.Time) error {
	n, err := r.q(ctx).SoftDeleteVehicleOwned(ctx, dbgen.SoftDeleteVehicleOwnedParams{
		PublicID: id, OwnerID: ownerID, Now: ts(now),
	})
	if err != nil {
		return safeError("vehicles: delete", err)
	}
	if n == 0 {
		return vehicles.ErrNotFound
	}
	return nil
}

// EraseOwner implements vehicles.Repo.
func (r *Repo) EraseOwner(ctx context.Context, ownerID int64, now time.Time) error {
	return safeError("vehicles: erase owner", r.q(ctx).EraseVehiclesOfOwner(ctx,
		dbgen.EraseVehiclesOfOwnerParams{OwnerID: ownerID, Now: ts(now)}))
}
