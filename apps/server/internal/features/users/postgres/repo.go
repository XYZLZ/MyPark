// Package postgres is the PostgreSQL adapter of the users feature.
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

	"github.com/XYZLZ/MyPark/apps/server/internal/domain/authz"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/auth"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/users"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/database"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/database/dbgen"
)

// Repo implements users.Repo over dbgen. It reads and writes through the
// transaction carried by the context, or through the pool when there is none.
type Repo struct{ pool *pgxpool.Pool }

var _ users.Repo = (*Repo)(nil)

// NewRepo returns a Repo over pool.
func NewRepo(pool *pgxpool.Pool) *Repo { return &Repo{pool: pool} }

func (r *Repo) q(ctx context.Context) *dbgen.Queries {
	return dbgen.New(database.Conn(ctx, r.pool))
}

func ts(t time.Time) pgtype.Timestamptz { return pgtype.Timestamptz{Time: t, Valid: true} }

func optText(s *string) pgtype.Text {
	if s == nil {
		return pgtype.Text{}
	}
	return pgtype.Text{String: *s, Valid: true}
}

func strPtr(t pgtype.Text) *string {
	if !t.Valid {
		return nil
	}
	return &t.String
}

func timePtr(t pgtype.Timestamptz) *time.Time {
	if !t.Valid {
		return nil
	}
	return &t.Time
}

// row holds the columns every profile query returns.
type row struct {
	publicID                                         uuid.UUID
	email, firstName, lastName, role, status, locale string
	phone, termsVersion, privacyVersion              pgtype.Text
	emailVerifiedAt, termsAcceptedAt                 pgtype.Timestamptz
	privacyAcceptedAt, createdAt                     pgtype.Timestamptz
}

func (w row) view() auth.UserView {
	role, _ := authz.ParseRole(w.role)
	return auth.UserView{
		PublicID: w.publicID, Email: w.email, FirstName: w.firstName, LastName: w.lastName,
		Phone: strPtr(w.phone), Role: role, Status: w.status, Locale: w.locale,
		EmailVerifiedAt: timePtr(w.emailVerifiedAt),
		TermsVersion:    strPtr(w.termsVersion), TermsAcceptedAt: timePtr(w.termsAcceptedAt),
		PrivacyVersion: strPtr(w.privacyVersion), PrivacyAcceptedAt: timePtr(w.privacyAcceptedAt),
		CreatedAt: w.createdAt.Time,
	}
}

// GetProfile implements users.Repo.
func (r *Repo) GetProfile(ctx context.Context, id int64) (auth.UserView, error) {
	g, err := r.q(ctx).GetUserProfileByID(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return auth.UserView{}, users.ErrNotFound
	}
	if err != nil {
		return auth.UserView{}, safeError("users: get profile", err)
	}
	return row{
		publicID: g.PublicID, email: g.Email, firstName: g.FirstName, lastName: g.LastName, role: g.Role,
		status: g.Status, locale: g.Locale, phone: g.Phone, termsVersion: g.TermsVersion,
		privacyVersion: g.PrivacyVersion, emailVerifiedAt: g.EmailVerifiedAt,
		termsAcceptedAt: g.TermsAcceptedAt, privacyAcceptedAt: g.PrivacyAcceptedAt, createdAt: g.CreatedAt,
	}.view(), nil
}

// UpdateProfile implements users.Repo.
func (r *Repo) UpdateProfile(ctx context.Context, id int64, u users.ProfileUpdate, now time.Time) (auth.UserView, error) {
	g, err := r.q(ctx).UpdateUserProfile(ctx, dbgen.UpdateUserProfileParams{
		ID: id, FirstName: optText(u.FirstName), LastName: optText(u.LastName), Locale: optText(u.Locale),
		SetPhone: u.SetPhone, Phone: optText(u.Phone), Now: ts(now),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return auth.UserView{}, users.ErrNotFound
	}
	if err != nil {
		return auth.UserView{}, safeError("users: update profile", err)
	}
	return row{
		publicID: g.PublicID, email: g.Email, firstName: g.FirstName, lastName: g.LastName, role: g.Role,
		status: g.Status, locale: g.Locale, phone: g.Phone, termsVersion: g.TermsVersion,
		privacyVersion: g.PrivacyVersion, emailVerifiedAt: g.EmailVerifiedAt,
		termsAcceptedAt: g.TermsAcceptedAt, privacyAcceptedAt: g.PrivacyAcceptedAt, createdAt: g.CreatedAt,
	}.view(), nil
}

// AnonymizeProfile implements users.Repo.
func (r *Repo) AnonymizeProfile(ctx context.Context, id int64, now time.Time) error {
	n, err := r.q(ctx).AnonymizeUserProfile(ctx, dbgen.AnonymizeUserProfileParams{ID: id, Now: ts(now)})
	if err != nil {
		return safeError("users: anonymize profile", err)
	}
	if n == 0 {
		return users.ErrNotFound
	}
	return nil
}

// SetLegalAcceptance implements users.Repo.
func (r *Repo) SetLegalAcceptance(ctx context.Context, id int64, terms, privacy string, now time.Time) error {
	err := r.q(ctx).UpdateUserLegalAcceptance(ctx, dbgen.UpdateUserLegalAcceptanceParams{
		ID: id, TermsVersion: pgtype.Text{String: terms, Valid: true},
		PrivacyVersion: pgtype.Text{String: privacy, Valid: true}, Now: ts(now),
	})
	return safeError("users: set legal acceptance", err)
}

// safeError drops the server message and detail of a Postgres error: a check
// violation detail echoes the failing row, which holds names and the phone.
func safeError(op string, err error) error {
	if err == nil {
		return nil
	}
	if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok {
		return fmt.Errorf("%s: sqlstate %s constraint %q", op, pgErr.Code, pgErr.ConstraintName)
	}
	return fmt.Errorf("%s: %w", op, err)
}

// ListAdmins implements users.Repo.
func (r *Repo) ListAdmins(ctx context.Context, limit int, after *users.AdminCursor) ([]users.AdminUser, error) {
	arg := dbgen.ListAdminsParams{PageSize: int32(limit)}
	if after != nil {
		arg.CursorAt = ts(after.At)
		arg.CursorID = pgtype.Int8{Int64: after.ID, Valid: true}
	}
	rows, err := r.q(ctx).ListAdmins(ctx, arg)
	if err != nil {
		return nil, safeError("users: list admins", err)
	}
	out := make([]users.AdminUser, len(rows))
	for i, a := range rows {
		out[i] = users.AdminUser{
			ID: a.ID, PublicID: a.PublicID, Email: a.Email, FirstName: a.FirstName, LastName: a.LastName,
			Status: a.Status, Locale: a.Locale, CreatedAt: a.CreatedAt.Time,
		}
	}
	return out, nil
}
