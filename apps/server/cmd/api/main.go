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
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"

	"github.com/XYZLZ/MyPark/apps/server/internal/config"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/health"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/legal"
	legalpg "github.com/XYZLZ/MyPark/apps/server/internal/features/legal/postgres"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/clock"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/database"
	"github.com/XYZLZ/MyPark/apps/server/internal/transport/http/healthprobe"
	"github.com/XYZLZ/MyPark/apps/server/internal/transport/http/httpserver"
	"github.com/XYZLZ/MyPark/apps/server/internal/transport/http/middleware"
	"github.com/XYZLZ/MyPark/apps/server/internal/transport/http/router"
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

	d := deps{
		log:    log,
		cfg:    cfg,
		pool:   pool,
		clock:  clock.System{},
		health: health.NewHandler(serviceName, version, pool, database.NewMigrationStatus(sqlDB)),
	}
	srv := httpserver.New(cfg.HTTPAddr(), newHandler(d), log)
	// The config is logged only through Config.LogValue, which redacts the
	// database password.
	return httpserver.Serve(ctx, log, srv, serviceName, "version", version, "config", cfg)
}

// legalCacheTTL is how long the current legal versions are cached.
const legalCacheTTL = 60 * time.Second

// deps are the dependencies newHandler wires. Tests build it directly.
type deps struct {
	log    *slog.Logger
	cfg    config.Config
	pool   *pgxpool.Pool
	clock  clock.Clock
	health *health.Handler
}

// newHandler builds the API router with every feature handler and wraps it
// in the shared middleware chain (see httpserver.NewHandler).
func newHandler(d deps) http.Handler {
	mux := http.NewServeMux()
	d.health.Register(mux)

	// Routes registered so far are all public: the authentication, role and
	// legal middlewares are wired with the features that need them.
	r := router.New(mux, router.Middlewares{
		ClientIP:  middleware.ClientIP(d.cfg.TrustedProxyCIDRs()),
		RateLimit: router.PassthroughRateLimit,
	})
	legalSvc := legal.NewService(legalpg.NewRepo(d.pool), d.clock, legalCacheTTL)
	legal.NewHandler(legalSvc, d.log).Register(r)

	return httpserver.NewHandler(d.log, mux, d.cfg.CORSAllowedOrigins())
}
