// Package postgres is the PostgreSQL adapter of the auth feature.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/XYZLZ/MyPark/apps/server/internal/features/auth"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/database"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/database/dbgen"
)

// emailActiveKey is the unique index on live emails.
const emailActiveKey = "users_email_active_key"

// Repo implements auth.Repo over dbgen. It reads and writes through the
// transaction carried by the context, or through the pool when there is none.
type Repo struct{ pool *pgxpool.Pool }

var _ auth.Repo = (*Repo)(nil)

// NewRepo returns a Repo over pool.
func NewRepo(pool *pgxpool.Pool) *Repo { return &Repo{pool: pool} }

func (r *Repo) q(ctx context.Context) *dbgen.Queries {
	return dbgen.New(database.Conn(ctx, r.pool))
}

func ts(t time.Time) pgtype.Timestamptz { return pgtype.Timestamptz{Time: t, Valid: true} }

func text(s string) pgtype.Text { return pgtype.Text{String: s, Valid: true} }

func optText(s *string) pgtype.Text {
	if s == nil {
		return pgtype.Text{}
	}
	return text(*s)
}

// LockUserByEmail implements auth.Repo.
func (r *Repo) LockUserByEmail(ctx context.Context, email string) (auth.UserRow, bool, error) {
	row, err := r.q(ctx).LockUserByEmail(ctx, email)
	if errors.Is(err, pgx.ErrNoRows) {
		return auth.UserRow{}, false, nil
	}
	if err != nil {
		return auth.UserRow{}, false, safeError("auth: lock user", err)
	}
	return auth.UserRow{ID: row.ID, PublicID: row.PublicID, Role: row.Role, Status: row.Status, Locale: row.Locale}, true, nil
}

// LockPendingDriverByEmail implements auth.Repo.
func (r *Repo) LockPendingDriverByEmail(ctx context.Context, email string) (auth.UserRow, bool, error) {
	row, err := r.q(ctx).LockPendingDriverByEmail(ctx, email)
	if errors.Is(err, pgx.ErrNoRows) {
		return auth.UserRow{}, false, nil
	}
	if err != nil {
		return auth.UserRow{}, false, safeError("auth: lock pending driver", err)
	}
	return auth.UserRow{ID: row.ID, PublicID: row.PublicID, Role: auth.RoleDriver, Status: auth.StatusPending, Locale: row.Locale}, true, nil
}

// InsertPendingDriver implements auth.Repo.
func (r *Repo) InsertPendingDriver(ctx context.Context, d auth.NewDriver) (auth.UserRow, error) {
	row, err := r.q(ctx).InsertPendingDriver(ctx, dbgen.InsertPendingDriverParams{
		Email: d.Email, PasswordHash: text(d.PasswordHash), Now: ts(d.Now),
		FirstName: d.FirstName, LastName: d.LastName, Phone: optText(d.Phone), Locale: d.Locale,
		TermsVersion: text(d.TermsVersion), PrivacyVersion: text(d.PrivacyVersion),
	})
	if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok && pgErr.Code == "23505" && pgErr.ConstraintName == emailActiveKey {
		return auth.UserRow{}, auth.ErrEmailTaken
	}
	if err != nil {
		return auth.UserRow{}, safeError("auth: insert driver", err)
	}
	return auth.UserRow{ID: row.ID, PublicID: row.PublicID, Role: auth.RoleDriver, Status: auth.StatusPending, Locale: d.Locale}, nil
}

// OverwritePendingDriver implements auth.Repo.
func (r *Repo) OverwritePendingDriver(ctx context.Context, id int64, d auth.NewDriver) error {
	err := r.q(ctx).OverwritePendingDriver(ctx, dbgen.OverwritePendingDriverParams{
		ID: id, PasswordHash: text(d.PasswordHash), Now: ts(d.Now),
		FirstName: d.FirstName, LastName: d.LastName, Phone: optText(d.Phone), Locale: d.Locale,
		TermsVersion: text(d.TermsVersion), PrivacyVersion: text(d.PrivacyVersion),
	})
	return safeError("auth: overwrite pending driver", err)
}

// ActivateUser implements auth.Repo.
func (r *Repo) ActivateUser(ctx context.Context, id int64, now time.Time) error {
	return safeError("auth: activate user", r.q(ctx).ActivateUser(ctx, dbgen.ActivateUserParams{ID: id, Now: ts(now)}))
}

// ConsumeLiveCodes implements auth.Repo.
func (r *Repo) ConsumeLiveCodes(ctx context.Context, userID int64, p auth.Purpose, now time.Time) error {
	err := r.q(ctx).ConsumeLiveAuthCodes(ctx, dbgen.ConsumeLiveAuthCodesParams{UserID: userID, Purpose: string(p), Now: ts(now)})
	return safeError("auth: consume live codes", err)
}

// InsertCode implements auth.Repo.
func (r *Repo) InsertCode(ctx context.Context, userID int64, p auth.Purpose, hash []byte, expiresAt, now time.Time) (int64, error) {
	id, err := r.q(ctx).InsertAuthCode(ctx, dbgen.InsertAuthCodeParams{
		UserID: userID, Purpose: string(p), CodeHash: hash, ExpiresAt: ts(expiresAt), Now: ts(now),
	})
	if err != nil {
		return 0, safeError("auth: insert code", err)
	}
	return id, nil
}

// AttemptCode implements auth.Repo.
func (r *Repo) AttemptCode(ctx context.Context, userID int64, p auth.Purpose, now time.Time, maxAttempts int) (auth.CodeAttempt, bool, error) {
	row, err := r.q(ctx).AttemptAuthCode(ctx, dbgen.AttemptAuthCodeParams{
		UserID: userID, Purpose: string(p), Now: ts(now), MaxAttempts: int16(maxAttempts),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return auth.CodeAttempt{}, false, nil
	}
	if err != nil {
		return auth.CodeAttempt{}, false, safeError("auth: attempt code", err)
	}
	return auth.CodeAttempt{ID: row.ID, CodeHash: row.CodeHash}, true, nil
}

// ConsumeCode implements auth.Repo.
func (r *Repo) ConsumeCode(ctx context.Context, id int64, now time.Time) error {
	return safeError("auth: consume code", r.q(ctx).ConsumeAuthCode(ctx, dbgen.ConsumeAuthCodeParams{ID: id, Now: ts(now)}))
}

// safeError drops the server message and detail of a Postgres error: a
// unique or check violation detail echoes the failing row, which holds the
// email and names. A nil err stays nil.
func safeError(op string, err error) error {
	if err == nil {
		return nil
	}
	if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok {
		return fmt.Errorf("%s: sqlstate %s constraint %q", op, pgErr.Code, pgErr.ConstraintName)
	}
	return fmt.Errorf("%s: %w", op, err)
}
