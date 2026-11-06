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

const applicationName = "mypark-migrate"

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

	connCfg, err := pgx.ParseConfig(url)
	if err != nil {
		return fmt.Errorf("parse %s: %w", EnvMigratorDatabaseURL, err)
	}
	// No statement_timeout here, unlike the app pool: a migration may
	// legitimately run longer than an API query.
	connCfg.RuntimeParams["application_name"] = cmp.Or(connCfg.RuntimeParams["application_name"], applicationName)
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
