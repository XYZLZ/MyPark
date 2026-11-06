// Command migrate applies the embedded goose migrations to the database at
// MIGRATOR_DATABASE_URL, which must connect as the DDL role mypark_migrator.
// It is forward-only: there is no down command.
package main

import (
	"cmp"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"

	"github.com/XYZLZ/MyPark/apps/server/internal/config"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/database"
)

// EnvMigratorDatabaseURL names the connection URL of the migrator role.
const EnvMigratorDatabaseURL = "MIGRATOR_DATABASE_URL"

const (
	applicationName = "mypark-migrate"
	// lockTimeout makes a migration that waits on a lock held by the
	// running app (e.g. an ALTER TABLE behind a long transaction) fail fast
	// instead of hanging the deploy and blocking every query queued behind
	// it.
	lockTimeout = "10s"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Getenv, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "migrate:", err)
		stop()
		os.Exit(1)
	}
}

// run applies every pending migration and logs the resulting version.
func run(ctx context.Context, getenv func(string) string, stdout io.Writer) error {
	url, err := config.DatabaseURLFrom(getenv, EnvMigratorDatabaseURL)
	if err != nil {
		return err
	}
	log := slog.New(slog.NewJSONHandler(stdout, nil))

	connCfg, err := connConfig(url)
	if err != nil {
		return err
	}
	db := stdlib.OpenDB(*connCfg)
	defer db.Close()

	if err := database.MigrateUp(ctx, db); err != nil {
		return err
	}
	current, expected, err := database.NewMigrationStatus(db).Status(ctx)
	if err != nil {
		return err
	}
	log.Info("migrations applied", "current_version", current, "expected_version", expected)
	return nil
}

// connConfig parses url and sets the migrator's runtime parameters
// application_name and lock_timeout (values given in the URL win). It sets
// no statement_timeout, unlike the app pool: a migration may legitimately
// run longer than an API query.
func connConfig(url string) (*pgx.ConnConfig, error) {
	cfg, err := pgx.ParseConfig(url)
	if err != nil {
		// pgx redacts the password in parse errors.
		return nil, fmt.Errorf("parse %s: %w", EnvMigratorDatabaseURL, err)
	}
	params := cfg.RuntimeParams
	params["application_name"] = cmp.Or(params["application_name"], applicationName)
	params["lock_timeout"] = cmp.Or(params["lock_timeout"], lockTimeout)
	return cfg, nil
}
