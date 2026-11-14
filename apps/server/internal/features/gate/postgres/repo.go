// Package postgres implements gate.Repo over sqlc.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
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

// LockLotOccupancy implements gate.Repo.
func (Repo) LockLotOccupancy(ctx context.Context, lotID int64) (gate.LotOccupancy, error) {
	qs, err := q(ctx)
	if err != nil {
		return gate.LotOccupancy{}, err
	}
	row, err := qs.LockLotOccupancy(ctx, lotID)
	if err != nil {
		return gate.LotOccupancy{}, fmt.Errorf("gate: lock lot occupancy: %w", err)
	}
	return gate.LotOccupancy{Occupied: int(row.Occupied), Capacity: int(row.Capacity), Closed: row.Status == "closed"}, nil
}

// CountOutOfService implements gate.Repo.
func (Repo) CountOutOfService(ctx context.Context, lotID int64) (int, error) {
	qs, err := q(ctx)
	if err != nil {
		return 0, err
	}
	n, err := qs.CountOutOfService(ctx, lotID)
	if err != nil {
		return 0, fmt.Errorf("gate: count out of service: %w", err)
	}
	return int(n), nil
}

// CountFreshPending implements gate.Repo.
func (Repo) CountFreshPending(ctx context.Context, lotID int64, now time.Time) (int, error) {
	qs, err := q(ctx)
	if err != nil {
		return 0, err
	}
	n, err := qs.CountFreshPending(ctx, dbgen.CountFreshPendingParams{LotID: lotID, Now: ts(now)})
	if err != nil {
		return 0, fmt.Errorf("gate: count fresh pending: %w", err)
	}
	return int(n), nil
}

// ActiveCardByHash implements gate.Repo.
func (Repo) ActiveCardByHash(ctx context.Context, lotID int64, hash []byte) (gate.Card, error) {
	qs, err := q(ctx)
	if err != nil {
		return gate.Card{}, err
	}
	row, err := qs.GetActiveCardByHash(ctx, dbgen.GetActiveCardByHashParams{LotID: lotID, Hash: hash})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return gate.Card{}, gate.ErrNotFound
	case err != nil:
		return gate.Card{}, fmt.Errorf("gate: active card: %w", err)
	}
	return gate.Card{ID: row.ID, PublicID: row.PublicID}, nil
}

// CardHasOpenStay implements gate.Repo. Called under the lot lock, it locks
// the open stay it finds (M2C-29: lot, then stays).
func (Repo) CardHasOpenStay(ctx context.Context, lotID, cardID int64) (bool, error) {
	qs, err := q(ctx)
	if err != nil {
		return false, err
	}
	_, err = qs.GetOpenStayByCardForUpdate(ctx, dbgen.GetOpenStayByCardForUpdateParams{
		LotID: lotID, EmployeeCardID: pgtype.Int8{Int64: cardID, Valid: true},
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return false, nil
	case err != nil:
		return false, fmt.Errorf("gate: open stay of card: %w", err)
	}
	return true, nil
}

// InsertStay implements gate.Repo. The error never carries the driver's
// text, which could quote the ticket code hash.
func (Repo) InsertStay(ctx context.Context, s gate.NewStay) (uuid.UUID, error) {
	qs, err := q(ctx)
	if err != nil {
		return uuid.Nil, err
	}
	p := dbgen.InsertStayParams{
		LotID: s.LotID, CredentialKind: gate.KindTicket, TicketCodeHash: s.TicketCodeHash,
		Status: "entry_pending", TariffVersionID: s.TariffVersionID, Now: ts(s.AcceptedAt),
	}
	if s.EmployeeCardID != 0 {
		p.CredentialKind, p.TicketCodeHash = gate.KindEmployeeCard, nil
		p.EmployeeCardID = pgtype.Int8{Int64: s.EmployeeCardID, Valid: true}
	}
	row, err := qs.InsertStay(ctx, p)
	if err != nil {
		if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok {
			return uuid.Nil, fmt.Errorf("gate: insert stay: SQLSTATE %s (%s)", pgErr.Code, pgErr.ConstraintName)
		}
		return uuid.Nil, fmt.Errorf("gate: insert stay: %w", err)
	}
	return row.PublicID, nil
}

func ts(t time.Time) pgtype.Timestamptz { return pgtype.Timestamptz{Time: t, Valid: true} }
