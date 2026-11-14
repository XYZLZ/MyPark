// Package postgres implements gate.Repo over sqlc.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/XYZLZ/MyPark/apps/server/internal/features/gate"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/database"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/database/dbgen"
)

// Repo is the Postgres gate.Repo. Every method runs on the transaction in
// the context and fails without one: a gate write outside the event
// transaction would escape its rollback.
type Repo struct{}

var _ gate.Repo = Repo{}

// NewRepo returns a Repo.
func NewRepo() Repo { return Repo{} }

func q(ctx context.Context) (*dbgen.Queries, error) {
	tx, err := database.MustTx(ctx)
	if err != nil {
		return nil, fmt.Errorf("gate: %w", err)
	}
	return dbgen.New(tx), nil
}

// UpdateSpaceFromSensor implements gate.Repo.
func (Repo) UpdateSpaceFromSensor(ctx context.Context, lotID int64, code, state string, now time.Time) (bool, error) {
	qs, err := q(ctx)
	if err != nil {
		return false, err
	}
	_, err = qs.UpdateSpaceFromSensor(ctx, dbgen.UpdateSpaceFromSensorParams{
		Status: state, Now: pgtype.Timestamptz{Time: now, Valid: true}, LotID: lotID, Code: code,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return false, nil
	case err != nil:
		return false, fmt.Errorf("gate: update space from sensor: %w", err)
	}
	return true, nil
}

// SpaceExists implements gate.Repo.
func (Repo) SpaceExists(ctx context.Context, lotID int64, code string) (bool, error) {
	qs, err := q(ctx)
	if err != nil {
		return false, err
	}
	ok, err := qs.SpaceExists(ctx, dbgen.SpaceExistsParams{LotID: lotID, Code: code})
	if err != nil {
		return false, fmt.Errorf("gate: space exists: %w", err)
	}
	return ok, nil
}

// NotifySpaceChanged implements gate.Repo.
func (Repo) NotifySpaceChanged(ctx context.Context, lotID int64, code, state string) error {
	qs, err := q(ctx)
	if err != nil {
		return err
	}
	if err := qs.NotifySpaceChanged(ctx, strconv.FormatInt(lotID, 10)+":"+code+":"+state); err != nil {
		return fmt.Errorf("gate: notify space changed: %w", err)
	}
	return nil
}
