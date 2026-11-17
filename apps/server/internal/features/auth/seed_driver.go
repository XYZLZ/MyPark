package auth

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/XYZLZ/MyPark/apps/server/internal/domain/secret"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/database"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/database/dbgen"
)

// SeedDriverInput is a simulated driver to seed. The caller derives the
// password and passes the current legal versions.
type SeedDriverInput struct {
	Email, FirstName, LastName string
	Password                   secret.String
	TermsVersion               string
	PrivacyVersion             string
}

// SeedDriver creates an active, verified driver that has accepted the given
// legal versions (M3B-44). It must run inside a transaction and is idempotent
// on the email: an existing account is never rehashed, so a block or any other
// change made since is kept. Only the argon2id hash is stored; the password is
// never logged or audited.
func SeedDriver(ctx context.Context, d SeedDeps, in SeedDriverInput) (created bool, err error) {
	tx, err := database.MustTx(ctx)
	if err != nil {
		return false, err
	}
	q := dbgen.New(tx)
	email := NormalizeEmail(in.Email)
	// Look first: hashing is the expensive part and a rerun must not pay it.
	if _, err := q.GetLiveUserIDByEmail(ctx, email); err == nil {
		return false, nil
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return false, fmt.Errorf("auth: look up seed driver: %w", safeSeedError(err))
	}
	hash, err := hashPassword(ctx, d.Hasher, in.Password)
	if err != nil {
		return false, err
	}
	rows, err := q.InsertSeedDriver(ctx, dbgen.InsertSeedDriverParams{
		Email: email, PasswordHash: pgtype.Text{String: hash, Valid: true},
		FirstName: in.FirstName, LastName: in.LastName,
		TermsVersion:   pgtype.Text{String: in.TermsVersion, Valid: true},
		PrivacyVersion: pgtype.Text{String: in.PrivacyVersion, Valid: true},
		Now:            pgtype.Timestamptz{Time: d.Clock.Now(), Valid: true},
	})
	if err != nil {
		return false, fmt.Errorf("auth: insert seed driver: %w", safeSeedError(err))
	}
	return rows == 1, nil
}

// SeedDriverID returns the id of the live driver account with this email, for
// the seed to attach the driver's vehicle. ok is false when the email is held
// by an account that is not a driver: the seed must not touch it.
func SeedDriverID(ctx context.Context, email string) (id int64, ok bool, err error) {
	tx, err := database.MustTx(ctx)
	if err != nil {
		return 0, false, err
	}
	id, err = dbgen.New(tx).GetLiveDriverIDByEmail(ctx, NormalizeEmail(email))
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("auth: seed driver id: %w", safeSeedError(err))
	}
	return id, true, nil
}
