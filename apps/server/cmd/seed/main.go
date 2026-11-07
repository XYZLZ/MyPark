// Command seed creates the pilot parking lot, its 280 spaces and the initial
// legal documents in the database at DATABASE_URL. Seeding is DML only, so it
// runs as the app role mypark_app. It is idempotent and safe to run on every
// compose start.
package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/jackc/pgx/v5"

	"github.com/XYZLZ/MyPark/apps/server/internal/config"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/legal"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/lots"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/database"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/database/dbgen"
)

// seedMaxConns is enough: the seed runs in a single transaction.
const seedMaxConns = 2

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Getenv, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "seed:", err)
		stop()
		os.Exit(1)
	}
}

// run seeds the pilot lot and the legal documents in one transaction and logs its id.
func run(ctx context.Context, getenv func(string) string, stdout io.Writer) error {
	url, err := config.DatabaseURLFrom(getenv, config.EnvDatabaseURL)
	if err != nil {
		return err
	}
	log := slog.New(slog.NewJSONHandler(stdout, nil))

	pool, err := database.NewPool(ctx, url, seedMaxConns)
	if err != nil {
		return err
	}
	defer pool.Close()

	var lotID int64
	err = pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		var err error
		if lotID, err = lots.Seed(ctx, dbgen.New(tx)); err != nil {
			return err
		}
		return legal.SeedDocuments(ctx, tx)
	})
	if err != nil {
		return err
	}
	log.Info("seed applied", "lot_id", lotID)
	return nil
}
