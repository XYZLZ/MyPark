// Command api is the MyPark HTTP API server.
//
// With -healthcheck it does not serve: it probes GET /readyz on the local
// HTTP_ADDR port and exits 0 only on a 2xx answer. The distroless image has
// no curl, so the Docker healthcheck runs "/app/api -healthcheck".
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

const serviceName = "api"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := execute(ctx, os.Args[1:], os.Getenv, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "api:", err)
		stop()
		os.Exit(1)
	}
}

// execute parses the command-line flags, then either runs the -healthcheck
// probe or serves the API.
func execute(ctx context.Context, args []string, getenv func(string) string, stdout io.Writer) error {
	fs := flag.NewFlagSet(serviceName, flag.ContinueOnError)
	healthcheck := fs.Bool("healthcheck", false, "probe GET /readyz on the local HTTP_ADDR port and exit")
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
		return healthprobe.Local(getenv, config.EnvHTTPAddr, config.DefaultHTTPAddr, "/readyz")
	}
	return run(ctx, getenv, stdout)
}

// run loads the configuration, serves HTTP until ctx is canceled, then shuts
// down gracefully within httpserver.ShutdownTimeout.
func run(ctx context.Context, getenv func(string) string, stdout io.Writer) error {
	cfg, err := config.Load(getenv)
	if err != nil {
		return err
	}
	log := slog.New(slog.NewJSONHandler(stdout, &slog.HandlerOptions{Level: cfg.LogLevel()}))
	slog.SetDefault(log)

	// The pool connects lazily: an unreachable database does not stop the
	// API from booting, /readyz reports it as 503 instead.
	pool, err := database.NewPool(ctx, cfg.DatabaseURL(), cfg.DBMaxConns())
	if err != nil {
		return err
	}
	defer pool.Close()
	sqlDB := stdlib.OpenDBFromPool(pool)
	defer sqlDB.Close()

	h := health.NewHandler(serviceName, version, pool, database.NewMigrationStatus(sqlDB))
	srv := httpserver.New(cfg.HTTPAddr(), newHandler(log, h), log)
	// The config is logged only through Config.LogValue, which redacts the
	// database password.
	return httpserver.Serve(ctx, log, srv, serviceName, "version", version, "config", cfg)
}

// newHandler builds the API router and wraps it in the shared middleware
// chain (see httpserver.NewHandler).
func newHandler(log *slog.Logger, h *health.Handler) http.Handler {
	mux := http.NewServeMux()
	h.Register(mux)
	return httpserver.NewHandler(log, mux)
}
