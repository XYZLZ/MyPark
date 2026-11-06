// Command api is the MyPark HTTP API server.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/stdlib"

	"github.com/XYZLZ/MyPark/apps/server/internal/config"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/health"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/database"
	"github.com/XYZLZ/MyPark/apps/server/internal/transport/http/middleware"
	"github.com/XYZLZ/MyPark/apps/server/internal/transport/http/response"
)

// version is the build version reported by /healthz. Release builds set it
// with -ldflags "-X main.version=...".
var version = "dev"

const (
	serviceName     = "api"
	shutdownTimeout = 10 * time.Second
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Getenv, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "api:", err)
		stop()
		os.Exit(1)
	}
}

// run loads the configuration, serves HTTP until ctx is canceled, then shuts
// down gracefully within shutdownTimeout.
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

	srv := &http.Server{
		Addr:              cfg.HTTPAddr(),
		Handler:           newHandler(log, health.NewHandler(serviceName, version, pool, database.NewMigrationStatus(sqlDB))),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
		ErrorLog:          slog.NewLogLogger(log.Handler(), slog.LevelError),
	}

	ln, err := net.Listen("tcp", srv.Addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", srv.Addr, err)
	}
	// The config is logged only through Config.LogValue, which redacts the
	// database password.
	log.Info("api listening", "addr", ln.Addr().String(), "version", version, "config", cfg)

	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.Serve(ln) }()

	select {
	case err := <-serveErr:
		return fmt.Errorf("serve: %w", err)
	case <-ctx.Done():
	}

	log.Info("api shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	if err := <-serveErr; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("serve: %w", err)
	}
	log.Info("api stopped")
	return nil
}

// newHandler builds the full HTTP chain: recover -> request ID -> access log
// -> router (mux with JSON 404/405). It also points response.RequestIDFunc at
// the request ID middleware so every error envelope carries request_id.
func newHandler(log *slog.Logger, h *health.Handler) http.Handler {
	response.RequestIDFunc = middleware.RequestIDFrom

	mux := http.NewServeMux()
	h.Register(mux)

	return middleware.Recover(log)(
		middleware.RequestID(
			middleware.AccessLog(log)(
				middleware.JSONErrors(mux),
			),
		),
	)
}
