// Command seed creates the pilot parking lot and its 280 spaces in the
// database at DATABASE_URL. Seeding is DML only, so it runs as the app role
// mypark_app. It is idempotent and safe to run on every compose start.
package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/XYZLZ/MyPark/apps/server/internal/config"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/lots"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/database"
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

// run seeds the pilot lot in one transaction and logs its id.
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

	lotID, err := lots.SeedTx(ctx, pool)
	if err != nil {
		return err
	}
	log.Info("seed applied", "lot_id", lotID)
	return nil
}
