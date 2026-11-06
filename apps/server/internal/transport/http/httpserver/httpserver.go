// Package httpserver holds the HTTP plumbing shared by the server binaries
// (cmd/api, cmd/worker, cmd/simulator): the middleware chain, the server
// timeouts and the listen/serve/graceful-shutdown lifecycle.
package httpserver

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/XYZLZ/MyPark/apps/server/internal/transport/http/middleware"
	"github.com/XYZLZ/MyPark/apps/server/internal/transport/http/response"
)

// ShutdownTimeout bounds the graceful shutdown after the context is canceled.
const ShutdownTimeout = 10 * time.Second

// NewHandler wraps mux in the full HTTP chain: recover -> request ID ->
// access log -> router (mux with JSON 404/405). It also points
// response.RequestIDFunc at the request ID middleware so every error
// envelope carries request_id.
func NewHandler(log *slog.Logger, mux *http.ServeMux) http.Handler {
	response.RequestIDFunc = middleware.RequestIDFrom
	return middleware.Recover(log)(
		middleware.RequestID(
			middleware.AccessLog(log)(
				middleware.JSONErrors(mux),
			),
		),
	)
}

// New returns an http.Server for addr with the timeouts every MyPark
// server uses, logging its internal errors to log.
func New(addr string, h http.Handler, log *slog.Logger) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           h,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
		ErrorLog:          slog.NewLogLogger(log.Handler(), slog.LevelError),
	}
}

// Serve listens on srv.Addr and serves until ctx is canceled, then shuts
// down gracefully within ShutdownTimeout. Once listening it logs
// "<name> listening" with the bound address (so ":0" resolves to a real
// port) followed by attrs. It returns nil after a clean shutdown.
func Serve(ctx context.Context, log *slog.Logger, srv *http.Server, name string, attrs ...any) error {
	ln, err := net.Listen("tcp", srv.Addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", srv.Addr, err)
	}
	log.Info(name+" listening", append([]any{"addr", ln.Addr().String()}, attrs...)...)

	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.Serve(ln) }()

	select {
	case err := <-serveErr:
		return fmt.Errorf("serve: %w", err)
	case <-ctx.Done():
	}

	log.Info(name + " shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), ShutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	if err := <-serveErr; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("serve: %w", err)
	}
	log.Info(name + " stopped")
	return nil
}
