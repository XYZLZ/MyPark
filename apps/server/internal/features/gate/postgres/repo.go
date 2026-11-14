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

// StayForUpdate implements gate.Repo.
func (Repo) StayForUpdate(ctx context.Context, lotID int64, publicID uuid.UUID) (gate.Stay, error) {
	qs, err := q(ctx)
	if err != nil {
		return gate.Stay{}, err
	}
	row, err := qs.GetStayForUpdate(ctx, dbgen.GetStayForUpdateParams{LotID: lotID, PublicID: publicID})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return gate.Stay{}, gate.ErrNotFound
	case err != nil:
		return gate.Stay{}, fmt.Errorf("gate: stay for update: %w", err)
	}
	return gate.Stay{
		ID: row.ID, PublicID: row.PublicID, Status: gate.Status(row.Status), EmployeeCardID: row.EmployeeCardID.Int64,
	}, nil
}

// ActivateStay implements gate.Repo. Exactly one row must change: the
// caller holds the stay's lock and has checked its status.
func (Repo) ActivateStay(ctx context.Context, lotID, stayID int64, now time.Time) error {
	qs, err := q(ctx)
	if err != nil {
		return err
	}
	n, err := qs.ActivateStay(ctx, dbgen.ActivateStayParams{Now: ts(now), LotID: lotID, ID: stayID})
	if err != nil {
		return fmt.Errorf("gate: activate stay: %w", err)
	}
	if n != 1 {
		return fmt.Errorf("gate: activate stay %d: %d rows changed", stayID, n)
	}
	return nil
}

// BumpOccupancy implements gate.Repo.
func (Repo) BumpOccupancy(ctx context.Context, lotID int64, delta int, now time.Time) (int64, error) {
	qs, err := q(ctx)
	if err != nil {
		return 0, err
	}
	version, err := qs.BumpOccupancy(ctx, dbgen.BumpOccupancyParams{Delta: int32(delta), Now: ts(now), LotID: lotID})
	if err != nil {
		return 0, fmt.Errorf("gate: bump occupancy: %w", err)
	}
	return version, nil
}

// NotifyLotChanged implements gate.Repo.
func (Repo) NotifyLotChanged(ctx context.Context, lotID, version int64) error {
	qs, err := q(ctx)
	if err != nil {
		return err
	}
	if err := qs.NotifyLotChanged(ctx, dbgen.NotifyLotChangedParams{LotID: lotID, Version: version}); err != nil {
		return fmt.Errorf("gate: notify lot changed: %w", err)
	}
	return nil
}
