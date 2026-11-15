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
	return toStay(qs.GetStayForUpdate(ctx, dbgen.GetStayForUpdateParams{LotID: lotID, PublicID: publicID}))
}

// toStay maps a locked stays row, or its lookup error.
func toStay(row dbgen.Stay, err error) (gate.Stay, error) {
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return gate.Stay{}, gate.ErrNotFound
	case err != nil:
		return gate.Stay{}, fmt.Errorf("gate: stay for update: %w", err)
	}
	return gate.Stay{
		ID: row.ID, PublicID: row.PublicID, LotID: row.LotID, CredentialKind: row.CredentialKind,
		Status: gate.Status(row.Status), EmployeeCardID: row.EmployeeCardID.Int64, TariffVersionID: row.TariffVersionID,
		EnteredAt: row.EnteredAt.Time, PaidAt: row.PaidAt.Time, ExitDeadlineAt: row.ExitDeadlineAt.Time, ExitAuthorizedAt: row.ExitAuthorizedAt.Time,
		LostTicket: row.LostTicket,
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

// StayByTicketHashForUpdate implements gate.Repo.
func (Repo) StayByTicketHashForUpdate(ctx context.Context, lotID int64, hash []byte) (gate.Stay, error) {
	qs, err := q(ctx)
	if err != nil {
		return gate.Stay{}, err
	}
	return toStay(qs.GetStayByTicketHashForUpdate(ctx, dbgen.GetStayByTicketHashForUpdateParams{LotID: lotID, TicketCodeHash: hash}))
}

// OpenCardStayForUpdate implements gate.Repo.
func (Repo) OpenCardStayForUpdate(ctx context.Context, lotID, cardID int64) (gate.Stay, error) {
	qs, err := q(ctx)
	if err != nil {
		return gate.Stay{}, err
	}
	return toStay(qs.GetOpenStayByCardForUpdate(ctx, dbgen.GetOpenStayByCardForUpdateParams{
		LotID: lotID, EmployeeCardID: pgtype.Int8{Int64: cardID, Valid: true},
	}))
}

// PaymentTotals implements gate.Repo.
func (Repo) PaymentTotals(ctx context.Context, lotID, stayID int64) (int64, error) {
	qs, err := q(ctx)
	if err != nil {
		return 0, err
	}
	row, err := qs.PaymentTotals(ctx, dbgen.PaymentTotalsParams{LotID: lotID, StayID: stayID})
	if err != nil {
		return 0, fmt.Errorf("gate: payment totals: %w", err)
	}
	return row.PaidCents, nil
}

// InsertPayment implements gate.Repo: a succeeded parking fee.
func (Repo) InsertPayment(ctx context.Context, p gate.NewPayment) (int64, error) {
	qs, err := q(ctx)
	if err != nil {
		return 0, err
	}
	id, err := qs.InsertPayment(ctx, dbgen.InsertPaymentParams{
		Kind: "parking_fee", StayID: p.StayID, Method: p.Method, AmountCents: p.AmountCents, Status: "succeeded",
		IdempotencyKey: p.IdempotencyKey, DeviceID: pgtype.Int8{Int64: p.DeviceID, Valid: p.DeviceID != 0},
		Now: ts(p.SucceededAt), SucceededAt: ts(p.SucceededAt),
	})
	if err != nil {
		return 0, fmt.Errorf("gate: insert payment: %w", err)
	}
	return id, nil
}

// SetStayPaid implements gate.Repo.
func (Repo) SetStayPaid(ctx context.Context, lotID, stayID int64, now, exitDeadline time.Time) error {
	qs, err := q(ctx)
	if err != nil {
		return err
	}
	n, err := qs.SetStayPaid(ctx, dbgen.SetStayPaidParams{Now: ts(now), ExitDeadlineAt: ts(exitDeadline), LotID: lotID, ID: stayID})
	return oneRow("set stay paid", stayID, n, err)
}

// ReactivateStay implements gate.Repo.
func (Repo) ReactivateStay(ctx context.Context, lotID, stayID int64, now time.Time) error {
	qs, err := q(ctx)
	if err != nil {
		return err
	}
	n, err := qs.ReactivateStay(ctx, dbgen.ReactivateStayParams{Now: ts(now), LotID: lotID, ID: stayID})
	return oneRow("reactivate stay", stayID, n, err)
}

// MarkStayLost implements gate.Repo. The error never carries the driver's
// text, which could quote the ticket code hash.
func (Repo) MarkStayLost(ctx context.Context, lotID, stayID int64, hash []byte, now time.Time) error {
	qs, err := q(ctx)
	if err != nil {
		return err
	}
	n, err := qs.MarkStayLost(ctx, dbgen.MarkStayLostParams{TicketCodeHash: hash, Now: ts(now), LotID: lotID, ID: stayID})
	if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok {
		return fmt.Errorf("gate: mark stay lost: SQLSTATE %s (%s)", pgErr.Code, pgErr.ConstraintName)
	}
	return oneRow("mark stay lost", stayID, n, err)
}

// oneRow checks a guarded update of a locked stay: exactly one row changes,
// because the caller holds the lock and has checked the status.
func oneRow(op string, stayID, n int64, err error) error {
	if err != nil {
		return fmt.Errorf("gate: %s: %w", op, err)
	}
	if n != 1 {
		return fmt.Errorf("gate: %s %d: %d rows changed", op, stayID, n)
	}
	return nil
}

// CardByHash implements gate.Repo.
func (Repo) CardByHash(ctx context.Context, lotID int64, hash []byte) (gate.Card, error) {
	qs, err := q(ctx)
	if err != nil {
		return gate.Card{}, err
	}
	row, err := qs.GetCardByHash(ctx, dbgen.GetCardByHashParams{LotID: lotID, Hash: hash})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return gate.Card{}, gate.ErrNotFound
	case err != nil:
		return gate.Card{}, fmt.Errorf("gate: card: %w", err)
	}
	return gate.Card{ID: row.ID, PublicID: row.PublicID, Revoked: row.Status == "revoked"}, nil
}

// AuthorizeExit implements gate.Repo.
func (Repo) AuthorizeExit(ctx context.Context, lotID, stayID int64, now time.Time) error {
	qs, err := q(ctx)
	if err != nil {
		return err
	}
	n, err := qs.AuthorizeExit(ctx, dbgen.AuthorizeExitParams{Now: ts(now), LotID: lotID, ID: stayID})
	return oneRow("authorize exit", stayID, n, err)
}

// SetStayExited implements gate.Repo.
func (Repo) SetStayExited(ctx context.Context, lotID, stayID int64, now time.Time) error {
	qs, err := q(ctx)
	if err != nil {
		return err
	}
	n, err := qs.SetStayExited(ctx, dbgen.SetStayExitedParams{Now: ts(now), LotID: lotID, ID: stayID})
	return oneRow("set stay exited", stayID, n, err)
}

// ClearExitAuthorization implements gate.Repo.
func (Repo) ClearExitAuthorization(ctx context.Context, lotID, stayID int64, now time.Time) error {
	qs, err := q(ctx)
	if err != nil {
		return err
	}
	n, err := qs.ClearExitAuthorization(ctx, dbgen.ClearExitAuthorizationParams{Now: ts(now), LotID: lotID, ID: stayID})
	return oneRow("clear exit authorization", stayID, n, err)
}
