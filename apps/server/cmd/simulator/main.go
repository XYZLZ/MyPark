// Command simulator is the MyPark device simulator (design §13). It drives the
// pilot lot's devices through the API's signed device API only, never the
// database, and serves on SIMULATOR_ADDR (default :8090):
//
//   - /healthz, liveness;
//   - /readyz, 200 while the API's /readyz answered 200 within the last 30 s
//     (polled every 10 s);
//   - /control/..., the control API (design §13.3), which requires
//     X-Simulator-Token. It is meant for the compose network: the web BFF
//     calls it, and compose publishes the port on 127.0.0.1 only.
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
	"sync"
	"syscall"
	"time"

	"github.com/XYZLZ/MyPark/apps/server/internal/config"
	"github.com/XYZLZ/MyPark/apps/server/internal/domain/errcodes"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/health"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/clock"
	"github.com/XYZLZ/MyPark/apps/server/internal/simulator"
	"github.com/XYZLZ/MyPark/apps/server/internal/transport/http/healthprobe"
	"github.com/XYZLZ/MyPark/apps/server/internal/transport/http/httpserver"
	"github.com/XYZLZ/MyPark/apps/server/internal/transport/http/response"
)

// version is the build version reported by /healthz. Release builds set it
// with -ldflags "-X main.version=...".
var version = "dev"

const (
	serviceName = "simulator"
	// EnvAddr names the listen address of the simulator.
	EnvAddr = config.EnvSimulatorAddr
	// DefaultAddr is used when SIMULATOR_ADDR is unset.
	DefaultAddr = config.DefaultSimulatorAddr

	// The API counts as reachable while its /readyz answered 200 within
	// readyWindow; it is polled every pollEvery (design §13.4).
	readyWindow  = 30 * time.Second
	pollEvery    = 10 * time.Second
	probeTimeout = 5 * time.Second
	// eventLogSize is the control API's ring buffer (design §13.3).
	eventLogSize = 500
	// spacesTimeout bounds reading the lot's spaces at a start.
	spacesTimeout = 10 * time.Second
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

// run serves the simulator on the real clock.
func run(ctx context.Context, getenv func(string) string, stdout io.Writer) error {
	return runWith(ctx, getenv, stdout, clock.System{})
}

// newControlRoutes returns the control API patterns served under /control,
// for the OpenAPI parity test.
func newControlRoutes() []string { return simulator.ControlRoutes() }

// runWith validates the environment, then serves until ctx is canceled. timers
// drives the API readiness poll; the device client signs and the runner
// times runs on the system clock. Runs are bound to ctx, so a shutdown cuts
// them (their stays stay active, design §13.4).
func runWith(ctx context.Context, getenv func(string) string, stdout io.Writer, timers clock.Timers) error {
	cfg, err := config.LoadSimulator(getenv)
	if err != nil {
		return err
	}
	log := slog.New(slog.NewJSONHandler(stdout, nil))
	slog.SetDefault(log)
	log.Info("simulator configuration", "config", cfg)

	events := simulator.NewEventLog(eventLogSize)
	runner := simulator.NewRunner(simulator.RunnerDeps{
		Client: simulator.NewDeviceClient(simulator.ClientConfig{
			BaseURL: cfg.APIURL(), SignClock: clock.System{}, FleetSecret: cfg.FleetSecret(),
		}),
		FleetSecret: cfg.FleetSecret(),
		Wall:        clock.System{},
		Log:         events,
		Ctx:         ctx,
		Spaces:      simulator.PublicSpaces(&http.Client{Timeout: spacesTimeout}, cfg.APIURL()),
	})
	ready := &apiReadiness{hc: &http.Client{Timeout: probeTimeout}, url: cfg.APIURL() + "/readyz", clk: timers}

	mux := http.NewServeMux()
	health.RegisterLiveness(mux, serviceName, version)
	mux.HandleFunc("GET /readyz", ready.serve)
	mux.Handle("/control/", simulator.NewControl(runner, events, cfg.ControlToken(), log))

	var wg sync.WaitGroup
	pollCtx, stopPoll := context.WithCancel(ctx)
	wg.Go(func() { ready.poll(pollCtx) })
	defer func() {
		stopPoll()
		wg.Wait()
		runner.Wait()
	}()

	srv := httpserver.New(cfg.Addr(), httpserver.NewHandler(log, mux, nil), log)
	return httpserver.Serve(ctx, log, srv, serviceName, "version", version)
}

// apiReadiness tracks when the API's /readyz last answered 200.
type apiReadiness struct {
	hc  *http.Client
	url string
	clk clock.Timers

	mu     sync.Mutex
	lastOK time.Time
}

// poll probes the API at once, then every pollEvery until ctx is done.
func (a *apiReadiness) poll(ctx context.Context) {
	t := a.clk.NewTicker(pollEvery)
	defer t.Stop()
	for {
		a.probe(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C():
		}
	}
}

func (a *apiReadiness) probe(ctx context.Context) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.url, nil)
	if err != nil {
		return
	}
	res, err := a.hc.Do(req)
	if err != nil {
		return
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 64<<10))
	_ = res.Body.Close()
	if res.StatusCode == http.StatusOK {
		a.mu.Lock()
		a.lastOK = a.clk.Now()
		a.mu.Unlock()
	}
}

func (a *apiReadiness) ready() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return !a.lastOK.IsZero() && a.clk.Now().Sub(a.lastOK) <= readyWindow
}

type readinessBody struct {
	Status string            `json:"status"`
	Checks map[string]string `json:"checks"`
}

// serve answers /readyz: 200 while the API is reachable, else 503
// health.not_ready.
func (a *apiReadiness) serve(w http.ResponseWriter, r *http.Request) {
	if a.ready() {
		response.OK(w, r, http.StatusOK, readinessBody{Status: "ready", Checks: map[string]string{"api": "ok"}})
		return
	}
	response.Error(w, r, http.StatusServiceUnavailable, errcodes.HealthNotReady, "service is not ready", map[string]any{"api": "fail"})
}
