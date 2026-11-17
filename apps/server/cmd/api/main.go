// Command api is the MyPark HTTP API server.
//
// With -healthcheck it does not serve: it probes GET /readyz on the local
// HTTP_ADDR port and exits 0 only on a 2xx answer. The distroless image has
// no curl, so the Docker healthcheck runs "/app/api -healthcheck".
package main

import (
	"context"
	"crypto/rand"
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
	alertspg "github.com/XYZLZ/MyPark/apps/server/internal/features/alerts/postgres"
	auditpg "github.com/XYZLZ/MyPark/apps/server/internal/features/audit/postgres"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/auth"
	authpg "github.com/XYZLZ/MyPark/apps/server/internal/features/auth/postgres"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/availability"
	availabilitypg "github.com/XYZLZ/MyPark/apps/server/internal/features/availability/postgres"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/devices"
	devicespg "github.com/XYZLZ/MyPark/apps/server/internal/features/devices/postgres"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/gate"
	gatepg "github.com/XYZLZ/MyPark/apps/server/internal/features/gate/postgres"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/health"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/legal"
	legalpg "github.com/XYZLZ/MyPark/apps/server/internal/features/legal/postgres"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/lots"
	lotspg "github.com/XYZLZ/MyPark/apps/server/internal/features/lots/postgres"
	notificationspg "github.com/XYZLZ/MyPark/apps/server/internal/features/notifications/postgres"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/payments"
	paymentspg "github.com/XYZLZ/MyPark/apps/server/internal/features/payments/postgres"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/reservations"
	reservationspg "github.com/XYZLZ/MyPark/apps/server/internal/features/reservations/postgres"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/tariffs"
	tariffspg "github.com/XYZLZ/MyPark/apps/server/internal/features/tariffs/postgres"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/users"
	userspg "github.com/XYZLZ/MyPark/apps/server/internal/features/users/postgres"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/vehicles"
	vehiclespg "github.com/XYZLZ/MyPark/apps/server/internal/features/vehicles/postgres"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/clock"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/database"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/hashing"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/jwt"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/paymentsim"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/pubsub"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/ratelimit"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/secretbox"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/validation"
	"github.com/XYZLZ/MyPark/apps/server/internal/transport/http/authn"
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
	pool, err := database.NewPool(ctx, cfg.DatabaseURL(), requestPoolConns(cfg.DBMaxConns()))
	if err != nil {
		return err
	}
	defer pool.Close()
	sqlDB := stdlib.OpenDBFromPool(pool)
	defer sqlDB.Close()

	codes, err := hashing.NewCodeHasher(cfg.AuthCodePepper())
	if err != nil {
		return err
	}
	signer, err := jwt.NewSigner(cfg.JWTKeyID(), cfg.JWTSigningSeed(), clock.System{}, jwt.AccessTTL)
	if err != nil {
		return err
	}
	keys, err := ratelimit.NewKeyHasher(cfg.RateLimitPepper())
	if err != nil {
		return err
	}
	current, old := cfg.SecretboxKeys()
	sealer, err := secretbox.NewSealer(current, old...)
	if err != nil {
		return err
	}
	// One hub fans lot changes out to every stream. Its listener holds a
	// dedicated connection, hijacked out of the request pool (design §11.6).
	// REST and SSE share one availability service, so both see the same
	// reservation holds.
	availSvc := newAvailabilityService(pool, clock.System{})
	hub := availability.NewHub(availSvc, clock.System{}, log)
	listener := pubsub.New(pool, log, pubsub.Handlers{
		LotChanged: hub.LotChanged, SpaceChanged: hub.SpaceChanged, Reconnected: hub.Resync,
	})
	listenCtx, stopListener := context.WithCancel(ctx)
	listenerDone := make(chan struct{})
	go func() {
		defer close(listenerDone)
		_ = listener.Run(listenCtx)
	}()
	// Runs before the deferred pool.Close.
	defer func() {
		stopListener()
		<-listenerDone
		hub.Close()
	}()
	d := deps{
		signer: signer,
		sealer: sealer,
		log:    log,
		cfg:    cfg,
		pool:   pool,
		clock:  clock.System{},
		// Argon2id with the spec 7.1 parameters; at most 4 derivations at once.
		hasher:  hashing.NewArgon2id(hashing.DefaultParams, maxConcurrentHashes),
		codes:   codes,
		keys:    keys,
		limiter: ratelimit.NewComposite(ratelimit.NewPostgres(pool, clock.System{}, keys), ratelimit.NewMemory(clock.System{})),
		health:  health.NewHandler(serviceName, version, pool, database.NewMigrationStatus(sqlDB)),

		timers:     clock.System{},
		avail:      availSvc,
		hub:        hub,
		listenerUp: listener.Connected,
	}
	srv := newServer(cfg.HTTPAddr(), newHandler(d), log, hub)
	// The config is logged only through Config.LogValue, which redacts the
	// database password.
	return httpserver.Serve(ctx, log, srv, serviceName, "version", version, "config", cfg)
}

// newServer returns the API server. Shutdown closes the hub first, which ends
// every open stream, so srv.Shutdown does not wait on them (design §11.4).
func newServer(addr string, h http.Handler, log *slog.Logger, hub *availability.Hub) *http.Server {
	srv := httpserver.New(addr, h, log)
	srv.RegisterOnShutdown(hub.Close)
	return srv
}

// requestPoolConns is the request pool size for DB_MAX_CONNS: the listener
// hijacks one connection out of the pool, which the pool then replaces, so
// the pool keeps one less to stay within DB_MAX_CONNS (design §11.6). It is
// never below 1; DB_MAX_CONNS=1 thus means two connections in total.
func requestPoolConns(dbMaxConns int32) int32 {
	return max(dbMaxConns-1, 1)
}

// legalCacheTTL is how long the current legal versions are cached.
const legalCacheTTL = 60 * time.Second

// maxConcurrentHashes bounds concurrent argon2id derivations (OC-14).
const maxConcurrentHashes = 4

// deps are the dependencies newHandler wires. Tests build it directly.
type deps struct {
	log    *slog.Logger
	cfg    config.Config
	pool   *pgxpool.Pool
	clock  clock.Clock
	health *health.Handler

	signer  *jwt.Signer
	sealer  *secretbox.Sealer // opens device secrets (PF8)
	hasher  hashing.PasswordHasher
	codes   hashing.CodeHasher
	limiter ratelimit.Limiter
	keys    ratelimit.KeyHasher // hashes logged and stored rate-limit keys

	// Built in run (PF8). Handler-only tests leave them nil, and
	// newHandlerWithRoutes then builds a hub no listener feeds.
	avail      *availability.Service // nil: newHandlerWithRoutes builds one on pool and clock
	hub        *availability.Hub
	timers     clock.Timers // the hub and stream heartbeats; nil: clock.System
	listenerUp func() bool  // the LISTEN connection is up (/readyz); nil: no listener check

	// gateway charges and refunds cards; nil means the simulated gateway on pool.
	gateway payments.Gateway
}

// newHandler builds the API router with every feature handler and wraps it
// in the shared middleware chain (see httpserver.NewHandler).
func newHandler(d deps) http.Handler {
	h, _ := newHandlerWithRoutes(d)
	return h
}

// newHandlerWithRoutes is newHandler plus the route registry, so tests can
// check every registered route against its access policy.
func newHandlerWithRoutes(d deps) (http.Handler, []router.Route) {
	mux := http.NewServeMux()
	if d.listenerUp != nil {
		up := d.listenerUp
		d.health.WithCheck("listener", func(context.Context) bool { return up() })
	}
	d.health.Register(mux)

	// The router composes the gates per route, so they are built first.
	legalSvc := legal.NewService(legalpg.NewRepo(d.pool), d.clock, legalCacheTTL)
	authRepo := authpg.NewRepo(d.pool)
	verifier := jwt.NewVerifier(d.cfg.JWTVerifyKeys(), d.clock)
	gates := authn.New(auth.NewAuthenticator(verifier, authRepo, legalSvc, d.clock), d.log, authn.WithLegal(legalSvc))
	deviceRepo := devicespg.NewRepo(d.pool)
	deviceAuth := devices.NewAuthenticator(deviceRepo, d.sealer, d.clock, d.keys, d.log)
	r := router.New(mux, router.Middlewares{
		ClientIP:    middleware.ClientIP(d.cfg.TrustedProxyCIDRs()),
		RateLimit:   middleware.RateLimit(d.limiter, d.keys, d.log),
		Authn:       gates.Authn,
		Roles:       gates.Roles,
		Legal:       gates.Legal,
		DeviceAuth:  deviceAuth.Middleware,
		DeviceLimit: ratelimit.DeviceRule(d.cfg.DeviceRateLimitPerS()),
	})
	legal.NewHandler(legalSvc, d.log).Register(r)

	tariffSvc := tariffs.NewService(tariffspg.NewRepo(d.pool))
	lotRepo := lotspg.NewRepo(d.pool)
	tariffs.NewHandler(tariffSvc, lotRepo, d.clock, d.log).Register(r)

	availSvc := d.avail
	if availSvc == nil {
		availSvc = newAvailabilityService(d.pool, d.clock)
	}
	lots.NewHandler(lots.NewService(lotRepo), availSvc, d.log).Register(r)
	availability.NewHandler(availSvc, lotRepo, d.log).Register(r)
	timers := d.timers
	if timers == nil {
		timers = clock.System{}
	}
	hub := d.hub
	if hub == nil {
		hub = availability.NewHub(availSvc, timers, d.log)
	}
	availability.RegisterStream(r, availability.NewStreamHandler(availability.StreamDeps{
		Hub: hub, Service: availSvc, Lots: lotRepo, Timers: timers, Clock: timers, Log: d.log,
		Caps: availability.NewStreamCaps(d.cfg.SSEMaxPerIP(), d.cfg.SSEMaxStreams()),
	}))

	tx := database.NewTxRunner(d.pool)
	auditRec := auditpg.NewRecorder()
	notifier := notificationspg.NewNotifier(d.clock)
	gateway := d.gateway
	if gateway == nil {
		gateway = paymentsim.New(d.pool, paymentsim.Config{
			Clock: d.clock, Latency: d.cfg.SimGatewayLatency(), TimeoutDelay: d.cfg.SimGatewayTimeoutDelay(),
		})
	}
	reservationSvc := reservations.NewService(reservations.Deps{
		Tx: tx, Repo: reservationspg.NewRepo(d.pool), Tariffs: tariffSvc, Avail: availSvc, Clock: d.clock, Log: d.log,
		Fees: paymentspg.NewRepo(d.pool), Gateway: gateway, Notify: notifier, Alerts: alertspg.NewRecorder(),
		QRKey: d.cfg.QRHMACKey(), ChargeTimeout: d.cfg.PaymentChargeTimeout(), Rand: rand.Reader,
	})
	devices.NewHandler(devices.HandlerDeps{
		Tx:    tx,
		Store: deviceRepo,
		Gate: gate.NewService(gate.Deps{
			Repo: gatepg.NewRepo(), Clock: d.clock, Log: d.log, Tariffs: tariffSvc, Audit: auditRec,
			// Real reservation holds since M3; ticket codes come from crypto/rand.
			Holds: reservations.NewHolds(d.pool), CardPepper: d.cfg.CardUIDPepper(), Rand: rand.Reader,
			Reservations: reservations.NewGateAccess(reservationSvc),
		}),
		Sealer:     d.sealer,
		CardPepper: d.cfg.CardUIDPepper(),
		Clock:      d.clock,
		Log:        d.log,
	}).Register(r)

	authSvc := auth.NewService(auth.Deps{
		Tx:     tx,
		Repo:   authRepo,
		Hasher: d.hasher,
		Codes:  d.codes,
		Legal:  legalSvc,
		Audit:  auditRec,
		Notify: notifier,
		Clock:  d.clock,
		Signer: d.signer,
	})
	auth.NewHandler(authSvc, validation.New(), d.limiter, d.keys, d.log).Register(r)

	// A vehicle with a pending, confirmed or checked-in reservation cannot be deleted.
	vehicleSvc := vehicles.NewService(tx, vehiclespg.NewRepo(d.pool),
		reservations.NewVehicleChecker(d.pool), d.cfg.MaxVehiclesPerUser(), d.clock)
	vehicles.NewHandler(vehicleSvc, validation.New(), d.cfg.MaxVehiclesPerUser(), d.log).Register(r)

	reservations.NewHandler(reservationSvc, validation.New(), d.log).Register(r)

	payments.NewReceiptHandler(payments.NewReceiptService(tx, paymentspg.NewReceiptRepo(d.pool)), d.log).Register(r)

	// Account deletion reaches vehicles and auth only through their ports.
	usersSvc := users.NewService(users.Deps{
		Tx: tx, Repo: userspg.NewRepo(d.pool), Accounts: auth.NewAccounts(authSvc),
		Vehicles: vehicleSvc, Notify: notifier, Legal: legalSvc, Audit: auditRec, Clock: d.clock,
	})
	users.NewHandler(usersSvc, validation.New(), d.log).Register(r)

	return httpserver.NewHandler(d.log, mux, d.cfg.CORSAllowedOrigins()), r.Routes()
}

// newAvailabilityService builds the availability service over the real
// reservation holds.
func newAvailabilityService(pool *pgxpool.Pool, clk clock.Clock) *availability.Service {
	return availability.NewService(availabilitypg.NewRepo(pool), reservations.NewHolds(pool), clk)
}
