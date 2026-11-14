// Package postgres is the PostgreSQL adapter of the tariffs feature.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/XYZLZ/MyPark/apps/server/internal/features/tariffs"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/database"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/database/dbgen"
)

// Repo implements tariffs.Repo over dbgen.
type Repo struct{ pool *pgxpool.Pool }

var _ tariffs.Repo = (*Repo)(nil)

// NewRepo returns a Repo that reads through pool, or through the transaction
// carried by the context when there is one.
func NewRepo(pool *pgxpool.Pool) *Repo { return &Repo{pool: pool} }

// At implements tariffs.Repo.
func (r *Repo) At(ctx context.Context, lotID int64, t time.Time) (tariffs.Version, error) {
	row, err := dbgen.New(database.Conn(ctx, r.pool)).GetTariffVersionAt(ctx, dbgen.GetTariffVersionAtParams{
		LotID: lotID, At: pgtype.Timestamptz{Time: t, Valid: true},
	})
	return toVersion(row, err)
}

// ByID implements tariffs.Repo.
func (r *Repo) ByID(ctx context.Context, id int64) (tariffs.Version, error) {
	row, err := dbgen.New(database.Conn(ctx, r.pool)).GetTariffVersionByID(ctx, id)
	return toVersion(row, err)
}

func toVersion(row dbgen.TariffVersion, err error) (tariffs.Version, error) {
	if errors.Is(err, pgx.ErrNoRows) {
		return tariffs.Version{}, tariffs.ErrNoVersion
	}
	if err != nil {
		return tariffs.Version{}, fmt.Errorf("tariffs: read version: %w", err)
	}
	return tariffs.Version{ID: row.ID, LotID: row.LotID, Tariff: tariffs.Tariff{
		EffectiveFrom:   row.EffectiveFrom.Time,
		FirstHour:       row.FirstHourCents,
		AdditionalHour:  row.AdditionalHourCents,
		DailyCap:        row.DailyCapCents,
		LostTicket:      row.LostTicketCents,
		ReservationFee:  row.ReservationFeeCents,
		ExitGrace:       time.Duration(row.ExitGraceMin) * time.Minute,
		PostPaymentExit: time.Duration(row.PostPaymentExitMin) * time.Minute,
	}}, nil
}
