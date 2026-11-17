// Package postgres is the PostgreSQL adapter of the reservations feature.
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

	"github.com/XYZLZ/MyPark/apps/server/internal/features/reservations"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/database"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/database/dbgen"
)

// activeKey is the partial unique index of one active reservation per driver.
const activeKey = "reservations_user_active_key"

// txLimits bounds one driver-API transaction (M3B-55): a timeout rolls back
// and answers 500, so nothing is stored.
const txLimits = `select set_config('statement_timeout', '2s', true), set_config('lock_timeout', '1s', true)`

// Repo implements reservations.Repo over dbgen. It works inside the
// transaction carried by the context, or on the pool when there is none.
type Repo struct{ pool *pgxpool.Pool }

var _ reservations.Repo = (*Repo)(nil)

// NewRepo returns a Repo over pool.
func NewRepo(pool *pgxpool.Pool) *Repo { return &Repo{pool: pool} }

func (r *Repo) q(ctx context.Context) *dbgen.Queries {
	return dbgen.New(database.Conn(ctx, r.pool))
}

func ts(t time.Time) pgtype.Timestamptz { return pgtype.Timestamptz{Time: t, Valid: true} }

func ints(in []int32) []int {
	out := make([]int, len(in))
	for i, v := range in {
		out[i] = int(v)
	}
	return out
}

// SetTxLimits implements reservations.Repo.
func (r *Repo) SetTxLimits(ctx context.Context) error {
	tx, err := database.MustTx(ctx)
	if err != nil {
		return fmt.Errorf("reservations: transaction limits: %w", err)
	}
	if _, err := tx.Exec(ctx, txLimits); err != nil {
		return fmt.Errorf("reservations: set transaction limits: %w", err)
	}
	return nil
}

// LotForReservation implements reservations.Repo.
func (r *Repo) LotForReservation(ctx context.Context, lotPublicID uuid.UUID) (reservations.Lot, error) {
	row, err := r.q(ctx).GetLotForReservation(ctx, lotPublicID)
	if errors.Is(err, pgx.ErrNoRows) {
		return reservations.Lot{}, reservations.ErrNotFound
	}
	if err != nil {
		return reservations.Lot{}, fmt.Errorf("reservations: read lot: %w", err)
	}
	return reservations.Lot{
		ID: row.ID, PublicID: row.PublicID, Closed: row.Status == "closed",
		Capacity: int(row.Capacity), OutOfService: int(row.OutOfService),
	}, nil
}

// PolicyAt implements reservations.Repo.
func (r *Repo) PolicyAt(ctx context.Context, lotID int64, now time.Time) (reservations.Policy, error) {
	p, err := r.q(ctx).GetPolicyVersionAt(ctx, dbgen.GetPolicyVersionAtParams{LotID: lotID, At: ts(now)})
	if errors.Is(err, pgx.ErrNoRows) {
		return reservations.Policy{}, reservations.ErrNoPolicy
	}
	if err != nil {
		return reservations.Policy{}, fmt.Errorf("reservations: read policy: %w", err)
	}
	return reservations.Policy{
		VersionID:          p.ID,
		MaxReservablePct:   int(p.MaxReservablePct),
		BookingHorizonDays: int(p.BookingHorizonDays),
		ArrivalGraceMin:    int(p.ArrivalGraceMin),
		EarlyArrivalMin:    int(p.EarlyArrivalMin),
		PaymentTimeoutMin:  int(p.PaymentTimeoutMin),
		MaxActivePerUser:   int(p.MaxActivePerUser),
		SlotMin:            int(p.SlotMin),
		NoShowThreshold:    int(p.NoshowBlockThreshold),
		NoShowWindowDays:   int(p.NoshowWindowDays),
		NoShowBlockDays:    int(p.NoshowBlockDays),
	}, nil
}

// VehicleForShare implements reservations.Repo.
func (r *Repo) VehicleForShare(ctx context.Context, userID int64, vehiclePublicID uuid.UUID) (reservations.VehicleRef, error) {
	v, err := r.q(ctx).GetVehicleForShare(ctx, dbgen.GetVehicleForShareParams{PublicID: vehiclePublicID, UserID: userID})
	if errors.Is(err, pgx.ErrNoRows) {
		return reservations.VehicleRef{}, reservations.ErrNotFound
	}
	if err != nil {
		return reservations.VehicleRef{}, fmt.Errorf("reservations: read vehicle: %w", err)
	}
	return reservations.VehicleRef{
		ID: v.ID, PublicID: v.PublicID, PlateDisplay: v.PlateDisplay, Make: v.Make, Model: v.Model, Color: v.Color,
	}, nil
}

// BlockedUntil implements reservations.Repo.
func (r *Repo) BlockedUntil(ctx context.Context, userID int64) (time.Time, error) {
	t, err := r.q(ctx).GetReservationBlockedUntil(ctx, userID)
	if err != nil {
		return time.Time{}, fmt.Errorf("reservations: read block: %w", err)
	}
	if !t.Valid {
		return time.Time{}, nil
	}
	return t.Time, nil
}

// HasActive implements reservations.Repo.
func (r *Repo) HasActive(ctx context.Context, userID int64) (bool, error) {
	ok, err := r.q(ctx).HasActiveReservation(ctx, userID)
	if err != nil {
		return false, fmt.Errorf("reservations: check active: %w", err)
	}
	return ok, nil
}

// Insert implements reservations.Repo.
func (r *Repo) Insert(ctx context.Context, n reservations.NewReservation) (reservations.Row, error) {
	row, err := r.q(ctx).InsertReservation(ctx, dbgen.InsertReservationParams{
		UserID: n.UserID, VehicleID: n.VehicleID, LotID: n.LotID,
		ArrivalAt: ts(n.Arrival), WindowStart: ts(n.Window.Start), WindowEnd: ts(n.Window.End),
		FeeCents: n.FeeCents, TariffVersionID: n.TariffVersionID, PolicyVersionID: n.PolicyVersionID,
		PaymentDueAt: ts(n.PaymentDueAt), Now: ts(n.Now),
	})
	if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok && pgErr.Code == "23505" && pgErr.ConstraintName == activeKey {
		return reservations.Row{}, reservations.ErrActiveExists
	}
	if err != nil {
		return reservations.Row{}, fmt.Errorf("reservations: insert: %w", err)
	}
	return reservations.Row{
		ID: row.ID, PublicID: row.PublicID, Arrival: row.ArrivalAt.Time.UTC(),
		Status: reservations.Status(row.Status), FeeCents: row.FeeCents,
		PaymentDueAt: row.PaymentDueAt.Time.UTC(), CreatedAt: row.CreatedAt.Time.UTC(),
	}, nil
}

// EnsureAndLockSlots implements reservations.Repo.
func (r *Repo) EnsureAndLockSlots(ctx context.Context, lotID int64, w reservations.Window) ([]int, error) {
	q := r.q(ctx)
	err := q.EnsureSlotLoads(ctx, dbgen.EnsureSlotLoadsParams{
		LotID: lotID, WindowStart: ts(w.Start), WindowLast: ts(w.End.Add(-5 * time.Minute)),
	})
	if err != nil {
		return nil, fmt.Errorf("reservations: ensure slot loads: %w", err)
	}
	held, err := q.LockSlotLoads(ctx, dbgen.LockSlotLoadsParams{LotID: lotID, WindowStart: ts(w.Start), WindowEnd: ts(w.End)})
	if err != nil {
		return nil, fmt.Errorf("reservations: lock slot loads: %w", err)
	}
	return ints(held), nil
}

// IncSlots implements reservations.Repo.
func (r *Repo) IncSlots(ctx context.Context, lotID int64, w reservations.Window) error {
	err := r.q(ctx).IncSlotLoads(ctx, dbgen.IncSlotLoadsParams{LotID: lotID, WindowStart: ts(w.Start), WindowEnd: ts(w.End)})
	if err != nil {
		return fmt.Errorf("reservations: increment slot loads: %w", err)
	}
	return nil
}

// DecSlots implements reservations.Repo.
func (r *Repo) DecSlots(ctx context.Context, lotID int64, w reservations.Window) error {
	err := r.q(ctx).DecSlotLoads(ctx, dbgen.DecSlotLoadsParams{LotID: lotID, WindowStart: ts(w.Start), WindowEnd: ts(w.End)})
	if err != nil {
		return fmt.Errorf("reservations: decrement slot loads: %w", err)
	}
	return nil
}

// SlotLoads implements reservations.Repo.
func (r *Repo) SlotLoads(ctx context.Context, lotID int64, w reservations.Window) ([]int, error) {
	held, err := r.q(ctx).ReadSlotLoads(ctx, dbgen.ReadSlotLoadsParams{LotID: lotID, WindowStart: ts(w.Start), WindowEnd: ts(w.End)})
	if err != nil {
		return nil, fmt.Errorf("reservations: read slot loads: %w", err)
	}
	return ints(held), nil
}

// record maps the joined row every read query returns. The three generated
// row types have identical fields, so callers convert to one of them.
func record(r dbgen.GetReservationForUserRow) reservations.Record {
	opt := func(t pgtype.Timestamptz) *time.Time {
		if !t.Valid {
			return nil
		}
		u := t.Time.UTC()
		return &u
	}
	optText := func(t pgtype.Text) *string {
		if !t.Valid {
			return nil
		}
		return &t.String
	}
	return reservations.Record{
		RowID: r.ID, VehicleID: r.VehicleID,
		View: reservations.View{
			ID: r.PublicID, LotID: r.LotPublicID,
			Vehicle: reservations.VehicleRef{
				ID: r.VehicleID, PublicID: r.VehiclePublicID, PlateDisplay: r.PlateDisplay,
				Make: r.Make, Model: r.Model, Color: r.Color,
			},
			ArrivalAt: r.ArrivalAt.Time.UTC(),
			Window:    reservations.Window{Start: r.WindowStart.Time.UTC(), End: r.WindowEnd.Time.UTC()},
			Status:    reservations.Status(r.Status), FeeCents: r.FeeCents,
			PaymentDueAt: r.PaymentDueAt.Time.UTC(), CreatedAt: r.CreatedAt.Time.UTC(),
			ConfirmedAt: opt(r.ConfirmedAt), CancelledAt: opt(r.CancelledAt), CancelledBy: optText(r.CancelledBy),
			RefundReason: optText(r.RefundReason), CheckedInAt: opt(r.CheckedInAt),
			CompletedAt: opt(r.CompletedAt), ExpiredAt: opt(r.ExpiredAt),
		},
	}
}

// Get implements reservations.Repo.
func (r *Repo) Get(ctx context.Context, userID int64, id uuid.UUID) (reservations.Record, error) {
	row, err := r.q(ctx).GetReservationForUser(ctx, dbgen.GetReservationForUserParams{PublicID: id, UserID: userID})
	if errors.Is(err, pgx.ErrNoRows) {
		return reservations.Record{}, reservations.ErrNotFound
	}
	if err != nil {
		return reservations.Record{}, fmt.Errorf("reservations: read reservation: %w", err)
	}
	return record(row), nil
}

// List implements reservations.Repo.
func (r *Repo) List(ctx context.Context, userID int64, f reservations.ListFilter) ([]reservations.Record, error) {
	p := dbgen.ListReservationsForUserParams{UserID: userID, Scope: f.Scope, RowLimit: int32(f.Limit)}
	if f.After != nil {
		p.HasCursor, p.CursorArrival, p.CursorPublicID = true, ts(f.After.ArrivalAt), f.After.PublicID
	}
	rows, err := r.q(ctx).ListReservationsForUser(ctx, p)
	if err != nil {
		return nil, fmt.Errorf("reservations: list reservations: %w", err)
	}
	out := make([]reservations.Record, len(rows))
	for i, row := range rows {
		out[i] = record(dbgen.GetReservationForUserRow(row))
	}
	return out, nil
}
