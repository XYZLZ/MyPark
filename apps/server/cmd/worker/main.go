// Command worker is the MyPark background job runner. It delivers the email
// outbox over SMTP every 2 s, voids entries not passed within 60 s (every
// 15 s) and purges expired rate-limit counters (every minute), expired auth
// data (hourly), old outbox rows and device events past retention (daily). It
// also reconciles pending fee payments with the gateway (every 15 s), sends
// queued refunds (every 10 s), expires unpaid and no-show reservations (every
// 30 s), and it serves the health probes on
// WORKER_HEALTH_ADDR (default :8091), /healthz for liveness and /readyz for
// the database and migrations.
//
// With -healthcheck it does not serve: it probes GET /readyz on the local
// WORKER_HEALTH_ADDR port and exits 0 only on a 2xx answer.
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
	"sync"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"

	"github.com/XYZLZ/MyPark/apps/server/internal/config"
	alertspg "github.com/XYZLZ/MyPark/apps/server/internal/features/alerts/postgres"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/auth"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/devices"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/gate"
	gatepg "github.com/XYZLZ/MyPark/apps/server/internal/features/gate/postgres"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/health"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/notifications"
	notificationspg "github.com/XYZLZ/MyPark/apps/server/internal/features/notifications/postgres"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/payments"
	paymentspg "github.com/XYZLZ/MyPark/apps/server/internal/features/payments/postgres"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/reservations"
	reservationspg "github.com/XYZLZ/MyPark/apps/server/internal/features/reservations/postgres"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/clock"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/database"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/email"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/jobs"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/paymentsim"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/ratelimit"
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
	// maxConns covers the readiness checks and the jobs, which run one at a
	// time each.
	maxConns = 8

	dispatchEvery       = 2 * time.Second
	rateLimitPurgeEvery = time.Minute
	authPurgeEvery      = time.Hour
	outboxPurgeEvery    = 24 * time.Hour
	voidEvery           = 15 * time.Second
	reconcileEvery      = 15 * time.Second
	refundEvery         = 10 * time.Second
	expireEvery         = 30 * time.Second
	deviceEventsEvery   = 24 * time.Hour
	// purgeBatch is the rows one purge call deletes, so a backlog never holds
	// one long transaction.
	purgeBatch = 5000
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

// run validates the environment, then runs the jobs and serves the health
// endpoints until ctx is canceled, and shuts down gracefully.
func run(ctx context.Context, getenv func(string) string, stdout io.Writer) error {
	cfg, err := config.LoadWorker(getenv)
	if err != nil {
		return err
	}
	log := slog.New(slog.NewJSONHandler(stdout, nil))
	slog.SetDefault(log)

	// The pool connects lazily, as in cmd/api: an unreachable database is
	// reported by /readyz, not by a crash loop.
	pool, err := database.NewPool(ctx, cfg.DatabaseURL(), maxConns)
	if err != nil {
		return err
	}
	defer pool.Close()
	sqlDB := stdlib.OpenDBFromPool(pool)
	defer sqlDB.Close()

	mux := http.NewServeMux()
	health.NewHandler(serviceName, version, pool, database.NewMigrationStatus(sqlDB)).Register(mux)
	srv := httpserver.New(cfg.HealthAddr(), httpserver.NewHandler(log, mux, nil), log)

	// The jobs stop with ctx, or when Serve fails (cancel runs before Wait).
	jobsCtx, cancelJobs := context.WithCancel(ctx)
	var wg sync.WaitGroup
	wg.Go(func() { jobs.Run(jobsCtx, log, workerJobs(pool, cfg, clock.System{}, log)...) })
	defer wg.Wait()
	defer cancelJobs()
	return httpserver.Serve(ctx, log, srv, serviceName, "version", version)
}

// workerJobs lists the periodic jobs.
func workerJobs(pool *pgxpool.Pool, cfg config.WorkerConfig, clk clock.Clock, log *slog.Logger) []jobs.Job {
	sender := email.NewSMTP(email.SMTPConfig{
		Host: cfg.SMTPHost(), Port: cfg.SMTPPort(),
		Username: cfg.SMTPUsername(), Password: cfg.SMTPPassword(), StartTLS: cfg.SMTPStartTLS(),
	})
	dispatcher := notifications.NewDispatcher(pool, sender, clk, cfg.SMTPFrom(), log)
	authPurger := auth.NewPurger(pool, clk)
	reconciler := newReconciler(pool, cfg, clk, log)
	refunds := newRefundProcessor(pool, clk, log)
	voider := newReservationVoider(pool, clk, log)
	expiry := newReservationExpiry(pool, clk, log)
	return []jobs.Job{
		{Name: "outbox-dispatch", Every: dispatchEvery, Run: func(ctx context.Context) error {
			_, err := dispatcher.Tick(ctx)
			return err
		}},
		{Name: "rate_limit_purge", Every: rateLimitPurgeEvery, Run: func(ctx context.Context) error {
			_, err := ratelimit.Purge(ctx, pool, clk.Now())
			return err
		}},
		{Name: "auth-purge", Every: authPurgeEvery, Run: authPurger.Purge},
		{Name: "void_unpassed_entries", Every: voidEvery, Run: func(ctx context.Context) error {
			now := clk.Now()
			// Both run every tick: a failing M2 batch must not hold back the
			// reservation stays.
			n, batchErr := gate.VoidUnpassedEntries(ctx, pool, now)
			r, rsvErr := voider.Run(ctx, pool, now)
			if n > 0 {
				log.InfoContext(ctx, "voided unpassed entries", "count", n)
			}
			if r > 0 {
				log.InfoContext(ctx, "voided unpassed reservation entries", "count", r)
			}
			return errors.Join(batchErr, rsvErr)
		}},
		{Name: "payment_reconcile", Every: reconcileEvery, Run: func(ctx context.Context) error {
			n, err := reconciler.Run(ctx, clk.Now())
			if n > 0 {
				log.InfoContext(ctx, "reconciled pending payments", "count", n)
			}
			return err
		}},
		{Name: "expire_unpaid_reservations", Every: expireEvery, Run: func(ctx context.Context) error {
			n, err := expiry.ExpireUnpaid(ctx, clk.Now())
			if n > 0 {
				log.InfoContext(ctx, "expired unpaid reservations", "count", n)
			}
			return err
		}},
		{Name: "expire_no_shows", Every: expireEvery, Run: func(ctx context.Context) error {
			n, err := expiry.ExpireNoShows(ctx, clk.Now())
			if n > 0 {
				log.InfoContext(ctx, "expired no-show reservations", "count", n)
			}
			return err
		}},
		{Name: "refund_processing", Every: refundEvery, Run: func(ctx context.Context) error {
			n, err := refunds.Run(ctx, clk.Now())
			if n > 0 {
				log.InfoContext(ctx, "refunded payments", "count", n)
			}
			return err
		}},
		{Name: "device_events_purge", Every: deviceEventsEvery, Run: func(ctx context.Context) error {
			var total int64
			for {
				n, err := devices.PurgeEvents(ctx, pool, purgeBatch)
				total += n
				if err != nil || n < purgeBatch {
					if total > 0 {
						log.InfoContext(ctx, "purged device events", "count", total)
					}
					return err
				}
			}
		}},
		{Name: "outbox-purge", Every: outboxPurgeEvery, Run: func(ctx context.Context) error {
			_, err := notifications.PurgeOutbox(ctx, pool, clk)
			return err
		}},
	}
}

// newReconciler builds the payment reconcile job. It re-queries the simulated
// gateway by key and applies the answer through the reservations service, the
// same code the pay request runs.
func newReconciler(pool *pgxpool.Pool, cfg config.WorkerConfig, clk clock.Clock, log *slog.Logger) *payments.Reconciler {
	tx := database.NewTxRunner(pool)
	fees := paymentspg.NewRepo(pool)
	return &payments.Reconciler{
		Tx:      tx,
		Fees:    fees,
		Gateway: paymentsim.New(pool, paymentsim.Config{Clock: clk}),
		Outcomes: reservations.NewService(reservations.Deps{
			Tx: tx, Repo: reservationspg.NewRepo(pool), Clock: clk, Log: log,
			Fees: fees, Notify: notificationspg.NewNotifier(clk), Alerts: alertspg.NewRecorder(),
			QRKey: cfg.QRHMACKey(), ChargeTimeout: cfg.PaymentChargeTimeout(), Rand: rand.Reader,
		}),
		ChargeTimeout: cfg.PaymentChargeTimeout(),
		Log:           log,
	}
}

// newRefundProcessor builds the refund job. The reservations service sends the
// refund_succeeded email in the same transaction that finalizes the refund.
func newRefundProcessor(pool *pgxpool.Pool, clk clock.Clock, log *slog.Logger) *payments.RefundProcessor {
	tx := database.NewTxRunner(pool)
	return &payments.RefundProcessor{
		Tx:      tx,
		Refunds: paymentspg.NewRefundRepo(pool),
		Gateway: paymentsim.New(pool, paymentsim.Config{Clock: clk}),
		Alerts:  alertspg.NewRecorder(),
		Notice: reservations.NewService(reservations.Deps{
			Tx: tx, Repo: reservationspg.NewRepo(pool), Clock: clk, Log: log,
			Fees: paymentspg.NewRepo(pool), Notify: notificationspg.NewNotifier(clk),
		}),
		Log: log,
	}
}

// newReservationVoider builds the reservation-stay part of the void job
// (design 12.7, M3B-60). It only touches reservations through the gate port,
// so the service needs no gateway, mailer or QR key.
func newReservationVoider(pool *pgxpool.Pool, clk clock.Clock, log *slog.Logger) *gate.ReservationVoider {
	tx := database.NewTxRunner(pool)
	svc := reservations.NewService(reservations.Deps{Tx: tx, Repo: reservationspg.NewRepo(pool), Clock: clk, Log: log})
	return &gate.ReservationVoider{Tx: tx, Repo: gatepg.NewRepo(), Reservations: reservations.NewGateAccess(svc), Log: log}
}

// newReservationExpiry builds the unpaid and no-show expiry jobs (design 12).
// They never charge or refund, so the service needs no gateway or QR key.
func newReservationExpiry(pool *pgxpool.Pool, clk clock.Clock, log *slog.Logger) *reservations.Jobs {
	return reservations.NewJobs(reservations.Deps{
		Tx: database.NewTxRunner(pool), Repo: reservationspg.NewRepo(pool), Clock: clk, Log: log,
		Fees: paymentspg.NewRepo(pool), Notify: notificationspg.NewNotifier(clk),
	})
}
