package main

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/XYZLZ/MyPark/apps/server/internal/devicefleet"
	alertspg "github.com/XYZLZ/MyPark/apps/server/internal/features/alerts/postgres"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/auth"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/legal"
	notificationspg "github.com/XYZLZ/MyPark/apps/server/internal/features/notifications/postgres"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/payments"
	paymentspg "github.com/XYZLZ/MyPark/apps/server/internal/features/payments/postgres"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/reservations"
	reservationspg "github.com/XYZLZ/MyPark/apps/server/internal/features/reservations/postgres"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/vehicles"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/database"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/paymentsim"
	"github.com/XYZLZ/MyPark/apps/server/internal/simulator"
	"github.com/XYZLZ/MyPark/apps/server/internal/testsupport"
)

// The M3 scenarios run the reserved-car flows over the same rig: the simulated
// drivers book and pay over HTTP, and the worker's M3 jobs run on the pacer's
// virtual 15-second tick (reconcile, refunds, both expiries and the hold
// boundary), so holds, expiries and refunds move with the clock.

// m3Jobs are the worker's M3 job bodies, built as cmd/worker builds them.
type m3Jobs struct {
	reconcile *payments.Reconciler
	refunds   *payments.RefundProcessor
	expiry    *reservations.Jobs
	boundary  *reservations.HoldBoundary
}

func newM3Jobs(e *deviceEnv, sleep func(context.Context, time.Duration) error) *m3Jobs {
	pool, log := e.d.pool, e.d.log
	tx := database.NewTxRunner(pool)
	fees := paymentspg.NewRepo(pool)
	gw := paymentsim.New(pool, paymentsim.Config{Clock: e.clk, Sleep: sleep})
	notify := notificationspg.NewNotifier(e.clk)
	rsv := func(extra func(*reservations.Deps)) *reservations.Service {
		d := reservations.Deps{Tx: tx, Repo: reservationspg.NewRepo(pool), Clock: e.clk, Log: log, Fees: fees, Notify: notify}
		extra(&d)
		return reservations.NewService(d)
	}
	return &m3Jobs{
		reconcile: &payments.Reconciler{
			Tx: tx, Fees: fees, Gateway: gw, ChargeTimeout: e.cfg.PaymentChargeTimeout(), Log: log,
			Outcomes: rsv(func(d *reservations.Deps) {
				d.Alerts, d.QRKey, d.ChargeTimeout = alertspg.NewRecorder(), e.cfg.QRHMACKey(), e.cfg.PaymentChargeTimeout()
			}),
		},
		refunds: &payments.RefundProcessor{
			Tx: tx, Refunds: paymentspg.NewRefundRepo(pool), Gateway: gw, Alerts: alertspg.NewRecorder(), Log: log,
			Notice: rsv(func(*reservations.Deps) {}),
		},
		expiry:   reservations.NewJobs(reservations.Deps{Tx: tx, Repo: reservationspg.NewRepo(pool), Clock: e.clk, Log: log, Fees: fees, Notify: notify}),
		boundary: reservations.NewHoldBoundary(pool, log),
	}
}

// run is one worker tick of the M3 jobs at now, in the worker's list order.
func (j *m3Jobs) run(ctx context.Context, now time.Time) error {
	_, e1 := j.reconcile.Run(ctx, now)
	_, e2 := j.expiry.ExpireUnpaid(ctx, now)
	_, e3 := j.expiry.ExpireNoShows(ctx, now)
	_, e4 := j.boundary.Tick(ctx, now)
	_, e5 := j.refunds.Run(ctx, now)
	return errors.Join(e1, e2, e3, e4, e5)
}

// driverTap records the driver client's requests (method, scrubbed path,
// status). It never reads a body: the pay body holds a card.
type driverTap struct {
	next  http.RoundTripper
	mu    sync.Mutex
	lines []string
}

func (d *driverTap) RoundTrip(r *http.Request) (*http.Response, error) {
	res, err := d.next.RoundTrip(r)
	if err != nil {
		return res, err
	}
	d.mu.Lock()
	d.lines = append(d.lines, r.Method+" "+uuidPattern.ReplaceAllString(r.URL.Path, "<id>")+" "+http.StatusText(res.StatusCode))
	d.mu.Unlock()
	return res, nil
}

func (d *driverTap) trace() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]string(nil), d.lines...)
}

// withDrivers seeds the 20 simulated drivers (each with a vehicle) and wires a
// DriverClient over the handler transport, as the simulator process does.
func (r *scenarioRig) withDrivers() {
	r.t.Helper()
	err := database.NewTxRunner(r.d.pool).InTx(r.t.Context(), func(ctx context.Context) error {
		for n := 1; n <= devicefleet.SimDriverCount; n++ {
			email := devicefleet.SimDriverEmail(n)
			if _, err := auth.SeedDriver(ctx, auth.SeedDeps{Hasher: r.hasher, Clock: r.clk}, auth.SeedDriverInput{
				Email: email, FirstName: "Sim", LastName: "Driver", Password: devicefleet.SimDriverPassword(simFleet, email),
				TermsVersion: legal.InitialVersion, PrivacyVersion: legal.InitialVersion,
			}); err != nil {
				return err
			}
			id, _, err := auth.SeedDriverID(ctx, email)
			if err != nil {
				return err
			}
			if err := vehicles.SeedVehicle(ctx, id, devicefleet.SimDriverPlate(n), "Toyota", "Corolla", "Gris", r.clk.Now()); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		r.t.Fatalf("seed simulated drivers: %v", err)
	}
	r.pace.slots = true
	r.dtap = &driverTap{next: simulator.HandlerTransport(r.h)}
	r.drivers = simulator.NewDriverClient(simulator.DriverClientConfig{
		BaseURL: "http://api.test", HTTP: &http.Client{Transport: r.dtap}, Fleet: simFleet, Clock: r.clk, LotID: r.publicID,
	})
}

// drain closes the run and lets every car leave, with a bounded wait so a
// regression fails the test instead of hanging it.
func (r *scenarioRig) drain() {
	r.t.Helper()
	if _, err := r.runner.StopAsync(true); err != nil {
		r.t.Fatalf("stop: %v", err)
	}
	r.pace.release()
	done := make(chan struct{})
	go func() { r.runner.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(settleWait):
		r.t.Fatal("the drain did not finish")
	}
}

// runQuiet runs the engine on in 30-second steps until no confirmed
// reservation waits for its car (a Stop would drop a car still walking to the
// gate), bounded so it cannot spin.
func (r *scenarioRig) runQuiet() {
	r.t.Helper()
	for range 120 {
		if r.count(`select count(*) from reservations where status = 'confirmed'`) == 0 {
			return
		}
		r.pace.runTo(r.t, r.pace.Now().Add(30*time.Second))
	}
	r.t.Fatal("a confirmed reservation never reached the gate")
}

// finish runs the worker's M3 jobs once more at the clock's now (so every
// queued refund is processed) and asserts the run's invariants: occupancy and
// slots, no pending or failed refund, no 5xx answer and no error in the log.
func (r *scenarioRig) finish() {
	r.t.Helper()
	if err := r.jobs.run(r.t.Context(), r.clk.Now()); err != nil {
		r.t.Fatalf("final job tick: %v", err)
	}
	testsupport.AssertOccupancyInvariant(r.t, r.d.pool, r.lotID)
	testsupport.AssertSlotInvariant(r.t, r.d.pool, r.lotID)
	if n := r.count(`select count(*) from refunds where status <> 'succeeded'`); n != 0 {
		r.t.Errorf("refunds not processed = %d, want 0", n)
	}
	if out := r.logs.String(); strings.Contains(out, "status=500") || strings.Contains(out, "level=ERROR") {
		r.t.Errorf("the API logged a 500 or an error:\n%s", errorLines(out))
	}
	for _, body := range r.tap.errorsSeen() {
		r.t.Errorf("device answer >= 400: %s", body)
	}
}

func errorLines(out string) string {
	var bad []string
	for line := range strings.SplitSeq(out, "\n") {
		if strings.Contains(line, "status=500") || strings.Contains(line, "level=ERROR") {
			bad = append(bad, line)
		}
	}
	return strings.Join(bad[:min(len(bad), 5)], "\n")
}

func (c *traceTap) errorsSeen() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.errors...)
}

// qrEntries counts the log entries of the cars that presented a reservation QR
// at the barrier, by result.
func (r *scenarioRig) qrEntries() (open, other int) {
	for _, e := range r.log.After(0) {
		if e.Credential != "reservation_qr" || e.Type != "entry.credential_presented" {
			continue
		}
		if e.Result == "open" {
			open++
		} else {
			other++
		}
	}
	return open, other
}
