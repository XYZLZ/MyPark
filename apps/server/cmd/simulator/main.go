// Command simulator is the MyPark parking-sensor simulator. In M0 it runs no
// scenarios: it only serves the liveness probe /healthz on SIMULATOR_ADDR
// (default :8090). It has no dependencies, so it has no /readyz.
//
// With -healthcheck it does not serve: it probes GET /healthz on the local
// SIMULATOR_ADDR port and exits 0 only on a 2xx answer.
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

	"github.com/XYZLZ/MyPark/apps/server/internal/config"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/health"
	"github.com/XYZLZ/MyPark/apps/server/internal/transport/http/healthprobe"
	"github.com/XYZLZ/MyPark/apps/server/internal/transport/http/httpserver"
)

// version is the build version reported by /healthz. Release builds set it
// with -ldflags "-X main.version=...".
var version = "dev"

const (
	serviceName = "simulator"
	// EnvAddr names the listen address of the simulator.
	EnvAddr = "SIMULATOR_ADDR"
	// DefaultAddr is used when SIMULATOR_ADDR is unset.
	DefaultAddr = ":8090"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := execute(ctx, os.Args[1:], os.Getenv, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "simulator:", err)
		stop()
		os.Exit(1)
	}
}

// execute parses the command-line flags, then either runs the -healthcheck
// probe or runs the simulator.
func execute(ctx context.Context, args []string, getenv func(string) string, stdout io.Writer) error {
	fs := flag.NewFlagSet(serviceName, flag.ContinueOnError)
	healthcheck := fs.Bool("healthcheck", false, "probe GET /healthz on the local "+EnvAddr+" port and exit")
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
		return healthprobe.Local(getenv, EnvAddr, DefaultAddr, "/healthz")
	}
	return run(ctx, getenv, stdout)
}

// run validates the environment, then serves /healthz until ctx is canceled
// and shuts down gracefully.
func run(ctx context.Context, getenv func(string) string, stdout io.Writer) error {
	addr, err := config.ListenAddrFrom(getenv, EnvAddr, DefaultAddr)
	if err != nil {
		return err
	}
	log := slog.New(slog.NewJSONHandler(stdout, nil))
	slog.SetDefault(log)

	mux := http.NewServeMux()
	health.RegisterLiveness(mux, serviceName, version)
	srv := httpserver.New(addr, httpserver.NewHandler(log, mux, nil), log)
	return httpserver.Serve(ctx, log, srv, serviceName, "version", version)
}
