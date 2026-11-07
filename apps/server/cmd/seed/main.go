// Command seed creates the pilot parking lot, its 280 spaces, the initial
// legal documents and, when none exists, the first admin in the database at
// DATABASE_URL. Seeding is DML only, so it runs as the app role mypark_app.
// It is idempotent and safe to run on every compose start.
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
	auditpg "github.com/XYZLZ/MyPark/apps/server/internal/features/audit/postgres"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/auth"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/legal"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/lots"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/clock"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/database"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/database/dbgen"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/hashing"
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

// run seeds the pilot lot, the legal documents and the first admin in one
// transaction and logs the lot id. The admin variables are validated before
// anything is written, and the password is never logged.
func run(ctx context.Context, getenv func(string) string, stdout io.Writer) error {
	url, err := config.DatabaseURLFrom(getenv, config.EnvDatabaseURL)
	if err != nil {
		return err
	}
	admin, withAdmin, err := config.LoadSeedAdmin(getenv)
	if err != nil {
		return err
	}
	log := slog.New(slog.NewJSONHandler(stdout, nil))
	if !withAdmin {
		log.Warn("admin seed skipped: " + config.EnvSeedAdminEmail + " is not set")
	}

	pool, err := database.NewPool(ctx, url, seedMaxConns)
	if err != nil {
		return err
	}
	defer pool.Close()

	deps := auth.SeedDeps{
		Hasher: hashing.NewArgon2id(hashing.DefaultParams, 1),
		Audit:  auditpg.NewRecorder(),
		Clock:  clock.System{},
	}
	var lotID int64
	adminCreated := false
	err = database.NewTxRunner(pool).InTx(ctx, func(ctx context.Context) error {
		tx, err := database.MustTx(ctx)
		if err != nil {
			return err
		}
		if lotID, err = lots.Seed(ctx, dbgen.New(tx)); err != nil {
			return err
		}
		if err := legal.SeedDocuments(ctx, tx); err != nil {
			return err
		}
		if !withAdmin {
			return nil
		}
		adminCreated, err = auth.SeedFirstAdmin(ctx, deps, auth.SeedAdmin{
			Email: admin.Email(), FirstName: admin.FirstName(), LastName: admin.LastName(),
			Password: admin.Password(),
		})
		return err
	})
	if err != nil {
		return err
	}
	log.Info("seed applied", "lot_id", lotID)
	switch {
	case !withAdmin:
	case adminCreated:
		log.Info("admin seeded")
	default:
		log.Info("admin seed skipped: an admin already exists")
	}
	return nil
}
