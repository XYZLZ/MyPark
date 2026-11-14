// Package postgres implements devices.Registry over sqlc.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/XYZLZ/MyPark/apps/server/internal/domain/authz"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/devices"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/database"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/database/dbgen"
)

// Repo is the Postgres-backed devices.Registry. It uses the transaction in
// the context when there is one, otherwise the pool. It never opens a device
// secret: the authenticator does, only for an active device.
type Repo struct {
	pool *pgxpool.Pool
}

var _ devices.Registry = (*Repo)(nil)

// NewRepo returns a Repo over pool.
func NewRepo(pool *pgxpool.Pool) *Repo {
	return &Repo{pool: pool}
}

func (r *Repo) q(ctx context.Context) *dbgen.Queries {
	return dbgen.New(database.Conn(ctx, r.pool))
}

// DeviceByPublicID implements devices.Registry: the row with its secret still
// sealed. Any status other than active counts as revoked (fail closed).
func (r *Repo) DeviceByPublicID(ctx context.Context, id uuid.UUID) (devices.Record, error) {
	row, err := r.q(ctx).GetDeviceByPublicID(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return devices.Record{}, devices.ErrDeviceNotFound
	}
	if err != nil {
		return devices.Record{}, fmt.Errorf("devices: device by public id: %w", err)
	}
	rec := devices.Record{
		ID: row.ID, LotID: row.LotID, PublicID: row.PublicID, Kind: authz.DeviceKind(row.Kind),
		Revoked: row.Status != devices.StatusActive, SecretCiphertext: row.SecretCiphertext, SecretKeyID: row.SecretKeyID,
	}
	if row.LastSeenAt.Valid {
		rec.LastSeenAt = row.LastSeenAt.Time
	}
	return rec, nil
}

// TouchLastSeen implements devices.Registry.
func (r *Repo) TouchLastSeen(ctx context.Context, id int64, now time.Time) error {
	err := r.q(ctx).TouchDeviceLastSeen(ctx, dbgen.TouchDeviceLastSeenParams{
		ID: id, Now: pgtype.Timestamptz{Time: now, Valid: true},
	})
	if err != nil {
		return fmt.Errorf("devices: touch last seen: %w", err)
	}
	return nil
}
