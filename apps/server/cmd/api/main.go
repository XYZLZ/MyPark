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

	"github.com/XYZLZ/MyPark/apps/server/internal/config"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/health"
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

	// TODO(A4): replace notWiredDB with database.NewPool(ctx, cfg) as the
	// Pinger and the migration runner as the MigrationStatus. Until then
	// /readyz always returns 503.
	db := notWiredDB{}
	srv := &http.Server{
		Addr:              cfg.HTTPAddr(),
		Handler:           newHandler(log, health.NewHandler(serviceName, version, db, db)),
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

// errDBNotWired is returned by the readiness stub until A4 lands.
var errDBNotWired = errors.New("database not wired yet")

// notWiredDB is a temporary health.Pinger and health.MigrationStatus that
// always fails, so /readyz reports not ready.
//
// TODO(A4): delete once the Postgres pool and migration status are wired.
type notWiredDB struct{}

func (notWiredDB) Ping(context.Context) error { return errDBNotWired }

func (notWiredDB) Status(context.Context) (int64, int64, error) { return 0, 0, errDBNotWired }
