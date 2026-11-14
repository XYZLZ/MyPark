// Package postgres is the PostgreSQL adapter of the lots feature.
package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
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
