// Command worker is the MyPark background job runner. In M0 it runs no jobs:
// it only serves the health probes on WORKER_HEALTH_ADDR (default :8091),
// /healthz for liveness and /readyz for the database and migrations.
//
// With -healthcheck it does not serve: it probes GET /readyz on the local
// WORKER_HEALTH_ADDR port and exits 0 only on a 2xx answer.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/jackc/pgx/v5/stdlib"

	"github.com/XYZLZ/MyPark/apps/server/internal/config"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/health"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/database"
	"github.com/XYZLZ/MyPark/apps/server/internal/transport/http/healthprobe"
	"github.com/XYZLZ/MyPark/apps/server/internal/transport/http/httpserver"
)

// version is the build version reported by /healthz. Release builds set it
// with -ldflags "-X main.version=...".
var version = "dev"

const (
	serviceName = "worker"
	// EnvHealthAddr names the listen address of the health endpoints.
	EnvHealthAddr = "WORKER_HEALTH_ADDR"
	// DefaultHealthAddr is used when WORKER_HEALTH_ADDR is unset.
	DefaultHealthAddr = ":8091"
	// maxConns is enough for the readiness checks; raise it with the jobs.
	maxConns = 4
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := execute(ctx, os.Args[1:], os.Getenv, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "worker:", err)
		stop()
		os.Exit(1)
	}
}

// execute parses the command-line flags, then either runs the -healthcheck
// probe or runs the worker.
func execute(ctx context.Context, args []string, getenv func(string) string, stdout io.Writer) error {
	fs := flag.NewFlagSet(serviceName, flag.ContinueOnError)
	healthcheck := fs.Bool("healthcheck", false, "probe GET /readyz on the local "+EnvHealthAddr+" port and exit")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected arguments: %v", fs.Args())
	}
	if *healthcheck {
		return healthprobe.Local(getenv, EnvHealthAddr, DefaultHealthAddr, "/readyz")
	}
	return run(ctx, getenv, stdout)
}

// run validates the environment, then serves the health endpoints until ctx
// is canceled and shuts down gracefully.
func run(ctx context.Context, getenv func(string) string, stdout io.Writer) error {
	addr, addrErr := config.ListenAddrFrom(getenv, EnvHealthAddr, DefaultHealthAddr)
	dbURL, dbErr := config.DatabaseURLFrom(getenv, config.EnvDatabaseURL)
	if err := errors.Join(addrErr, dbErr); err != nil {
		return err
	}
	log := slog.New(slog.NewJSONHandler(stdout, nil))
	slog.SetDefault(log)

	// The pool connects lazily, as in cmd/api: an unreachable database is
	// reported by /readyz, not by a crash loop.
	pool, err := database.NewPool(ctx, dbURL, maxConns)
	if err != nil {
		return err
	}
	defer pool.Close()
	sqlDB := stdlib.OpenDBFromPool(pool)
	defer sqlDB.Close()

	mux := http.NewServeMux()
	health.NewHandler(serviceName, version, pool, database.NewMigrationStatus(sqlDB)).Register(mux)
	srv := httpserver.New(addr, httpserver.NewHandler(log, mux), log)
	return httpserver.Serve(ctx, log, srv, serviceName, "version", version)
}
