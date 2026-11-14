// Package postgres implements devices.Repository over sqlc.
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
	"github.com/XYZLZ/MyPark/apps/server/internal/domain/secret"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/devices"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/database"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/database/dbgen"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/secretbox"
)

// Repo is the Postgres-backed devices.Repository. It uses the transaction in
// the context when there is one, otherwise the pool.
type Repo struct {
	pool   *pgxpool.Pool
	sealer *secretbox.Sealer
}

var _ devices.Repository = (*Repo)(nil)

// NewRepo returns a Repo that opens device secrets with sealer.
func NewRepo(pool *pgxpool.Pool, sealer *secretbox.Sealer) *Repo {
	return &Repo{pool: pool, sealer: sealer}
}

func (r *Repo) q(ctx context.Context) *dbgen.Queries {
	return dbgen.New(database.Conn(ctx, r.pool))
}

// GetByPublicID implements devices.Repository. A secret that cannot be opened
// is an error, never a device with an empty secret.
func (r *Repo) GetByPublicID(ctx context.Context, publicID uuid.UUID) (devices.Device, error) {
	row, err := r.q(ctx).GetDeviceByPublicID(ctx, publicID)
	if errors.Is(err, pgx.ErrNoRows) {
		return devices.Device{}, devices.ErrNotFound
	}
	if err != nil {
		return devices.Device{}, fmt.Errorf("devices: get by public id: %w", err)
	}
	plain, err := r.sealer.Open(row.SecretCiphertext, row.SecretKeyID)
	if err != nil {
		return devices.Device{}, fmt.Errorf("devices: open secret of %s: %w", publicID, err)
	}
	return devices.Device{
		ID: row.ID, PublicID: row.PublicID, LotID: row.LotID, Kind: authz.DeviceKind(row.Kind),
		Name: row.Name, Status: row.Status, Secret: secret.Bytes(plain),
	}, nil
}

// TouchLastSeen implements devices.Repository.
func (r *Repo) TouchLastSeen(ctx context.Context, id int64, now time.Time) error {
	err := r.q(ctx).TouchDeviceLastSeen(ctx, dbgen.TouchDeviceLastSeenParams{
		ID: id, Now: pgtype.Timestamptz{Time: now, Valid: true},
	})
	if err != nil {
		return fmt.Errorf("devices: touch last seen: %w", err)
	}
	return nil
}
