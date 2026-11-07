package auth

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/XYZLZ/MyPark/apps/server/internal/domain/secret"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/audit"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/clock"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/database"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/database/dbgen"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/hashing"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/validation"
)

// seedLockKey is the advisory lock that serializes concurrent seed runs
// ("MYPARKAD" in ASCII).
const seedLockKey int64 = 0x4D595041524B4144

// ErrSeedEmailTaken means a live account that is not an admin holds the seed
// admin's email.
var ErrSeedEmailTaken = errors.New("auth: seed admin email is held by another account")

// SeedAdmin is the first admin to create. The caller has validated the email
// and the names; the password is checked here against the register policy.
type SeedAdmin struct {
	Email, FirstName, LastName string
	Password                   secret.String
}

// SeedDeps are the collaborators of SeedFirstAdmin.
type SeedDeps struct {
	Hasher hashing.PasswordHasher
	Audit  audit.Recorder
	Clock  clock.Clock
}

// seedPassword mirrors the register policy for passwords (spec 7.5).
type seedPassword struct {
	Password string `json:"password" validate:"required,min=10,max=128,password_common"`
}

// SeedFirstAdmin creates the first admin unless any non-deleted admin exists
// (OC-20). It must run inside a transaction: the existence check, the insert
// and the audit row commit together. It never overwrites or rehashes, so a
// repeated run is a no-op. The password is never logged or audited.
func SeedFirstAdmin(ctx context.Context, d SeedDeps, in SeedAdmin) (created bool, err error) {
	tx, err := database.MustTx(ctx)
	if err != nil {
		return false, err
	}
	q := dbgen.New(tx)
	if err := q.LockAdminSeed(ctx, seedLockKey); err != nil {
		return false, fmt.Errorf("auth: lock admin seed: %w", err)
	}
	exists, err := q.AdminExists(ctx)
	if err != nil {
		return false, fmt.Errorf("auth: check admin exists: %w", err)
	}
	if exists {
		return false, nil
	}
	if err := validation.New().Struct(seedPassword{Password: in.Password.Reveal()}); err != nil {
		return false, fmt.Errorf("auth: seed admin password rejected by the register policy: %w", err)
	}
	hash, err := hashPassword(ctx, d.Hasher, in.Password)
	if err != nil {
		return false, err
	}
	now := d.Clock.Now()
	row, err := q.InsertSeedAdmin(ctx, dbgen.InsertSeedAdminParams{
		Email: NormalizeEmail(in.Email), PasswordHash: pgtype.Text{String: hash, Valid: true},
		FirstName: in.FirstName, LastName: in.LastName, Now: pgtype.Timestamptz{Time: now, Valid: true},
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return false, ErrSeedEmailTaken
	}
	if err != nil {
		return false, fmt.Errorf("auth: insert seed admin: %w", safeSeedError(err))
	}
	err = d.Audit.Record(ctx, audit.Entry{
		Action: audit.AdminSeeded, Actor: audit.ActorSystem,
		EntityType: "user", EntityID: row.PublicID.String(),
	})
	if err != nil {
		return false, fmt.Errorf("auth: audit %s: %w", audit.AdminSeeded, err)
	}
	return true, nil
}

// safeSeedError drops the server message and detail of a Postgres error: a
// check violation detail echoes the failing row.
func safeSeedError(err error) error {
	if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok {
		return fmt.Errorf("sqlstate %s constraint %q", pgErr.Code, pgErr.ConstraintName)
	}
	return err
}
