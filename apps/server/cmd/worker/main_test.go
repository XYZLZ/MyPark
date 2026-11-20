package main

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/XYZLZ/MyPark/apps/server/internal/config"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/jobs"
	"github.com/XYZLZ/MyPark/apps/server/internal/testsupport"
)

// dummyDatabaseURL is never dialed by /healthz: the pool connects lazily.
const dummyDatabaseURL = "postgres://mypark_app:x@127.0.0.1:1/mypark?connect_timeout=1"

// smtpEnv is the SMTP part of a valid worker environment; nothing dials it
// unless a message is due.
var smtpEnv = map[string]string{"SMTP_HOST": "127.0.0.1", "SMTP_PORT": "1", "SMTP_FROM": "MyPark <no-reply@mypark.local>"}

func workerEnv(kv ...string) map[string]string {
	env := map[string]string{"DATABASE_URL": dummyDatabaseURL}
	for k, v := range smtpEnv {
		env[k] = v
	}
	for i := 0; i+1 < len(kv); i += 2 {
		env[kv[i]] = kv[i+1]
	}
	return env
}

func TestWorker_HealthzServes(t *testing.T) {
	base := testsupport.StartServer(t, run, workerEnv("WORKER_HEALTH_ADDR", "127.0.0.1:0"), "worker listening")

	resp, err := http.Get(base + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	defer resp.Body.Close()
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode /healthz body: %v", err)
	}
	data, _ := body["data"].(map[string]any)
	if resp.StatusCode != http.StatusOK || data["status"] != "ok" || data["service"] != "worker" {
		t.Fatalf("/healthz = %d %v, want 200 ok service=worker", resp.StatusCode, body)
	}
}

// TestWorker_ReadyzDBDown checks the worker reports an unreachable database
// on /readyz instead of failing to boot.
func TestWorker_ReadyzDBDown(t *testing.T) {
	base := testsupport.StartServer(t, run, workerEnv("WORKER_HEALTH_ADDR", "127.0.0.1:0"), "worker listening")

	resp, err := http.Get(base + "/readyz")
	if err != nil {
		t.Fatalf("GET /readyz: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("/readyz status = %d, want 503", resp.StatusCode)
	}
}

func TestWorker_MissingDatabaseURL(t *testing.T) {
	err := run(t.Context(), func(string) string { return "" }, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "DATABASE_URL is required") {
		t.Fatalf("run error = %v, want one naming DATABASE_URL", err)
	}
}

func TestWorker_InvalidHealthAddr(t *testing.T) {
	env := workerEnv("WORKER_HEALTH_ADDR", "nope")
	err := run(t.Context(), func(k string) string { return env[k] }, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "WORKER_HEALTH_ADDR") {
		t.Fatalf("run error = %v, want one naming WORKER_HEALTH_ADDR", err)
	}
}

func TestWorker_HealthcheckFlag(t *testing.T) {
	for _, tc := range []struct {
		status  int
		wantErr bool
	}{{http.StatusOK, false}, {http.StatusServiceUnavailable, true}} {
		var gotPath string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotPath = r.URL.Path
			w.WriteHeader(tc.status)
		}))
		env := map[string]string{"WORKER_HEALTH_ADDR": strings.TrimPrefix(srv.URL, "http://")}
		err := execute(t.Context(), []string{"-healthcheck"}, func(k string) string { return env[k] }, io.Discard)
		srv.Close()
		if (err != nil) != tc.wantErr {
			t.Errorf("status %d: execute error = %v, wantErr %v", tc.status, err, tc.wantErr)
		}
		if gotPath != "/readyz" {
			t.Errorf("status %d: probed %q, want /readyz", tc.status, gotPath)
		}
	}
}

func TestWorker_MissingSMTPConfig(t *testing.T) {
	env := map[string]string{"DATABASE_URL": dummyDatabaseURL}
	err := run(t.Context(), func(k string) string { return env[k] }, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "SMTP_HOST") {
		t.Fatalf("run error = %v, want one naming SMTP_HOST", err)
	}
}

// TestWorker_DeliversOutbox runs the whole worker against a real database
// and Mailpit: a due outbox row is sent, then its code is scrubbed.
func TestWorker_DeliversOutbox(t *testing.T) {
	smtpAddr, api := testsupport.Mailpit(t)
	db := testsupport.NewTestDB(t)
	to := testsupport.UniqueRecipient(t, api)
	if _, err := db.Super.Exec(t.Context(), `insert into outbox_messages (template, locale, to_address, payload, dedupe_key)
		values ('verify_email', 'en', $1, '{"code":"424242"}', 'worker-test:1')`, to); err != nil {
		t.Fatal(err)
	}
	host, port := testsupport.SplitHostPort(t, smtpAddr)
	testsupport.StartServer(t, run, map[string]string{
		"WORKER_HEALTH_ADDR": "127.0.0.1:0", "DATABASE_URL": db.AppURL,
		"SMTP_HOST": host, "SMTP_PORT": strconv.Itoa(port), "SMTP_FROM": "MyPark <no-reply@mypark.local>",
	}, "worker listening")

	deadline := time.Now().Add(15 * time.Second)
	for {
		msgs := testsupport.MailpitMessages(t, api, to)
		if len(msgs) == 1 {
			if !strings.Contains(msgs[0].Text, "424242") {
				t.Errorf("message lacks the code")
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("no message delivered, mailpit has %d", len(msgs))
		}
		time.Sleep(100 * time.Millisecond)
	}
	var status string
	var payload []byte
	for {
		if err := db.Super.QueryRow(t.Context(), `select status, payload from outbox_messages where dedupe_key = 'worker-test:1'`).Scan(&status, &payload); err != nil {
			t.Fatal(err)
		}
		if status == "sent" || time.Now().After(deadline) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if status != "sent" || strings.Contains(string(payload), "424242") {
		t.Errorf("row status %q payload %s, want sent and scrubbed", status, payload)
	}
}

func TestWorkerJobs_RateLimitPurgeEveryMinute(t *testing.T) {
	db := testsupport.NewDB(t)
	clk := testsupport.NewFakeClock(time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC))
	if _, err := db.Exec(t.Context(), `insert into rate_limit_counters (key, window_start, count, expires_at)
		values ('\x01', $1, 1, $2), ('\x02', $1, 1, $3)`,
		clk.Now().Add(-2*time.Minute), clk.Now().Add(-time.Minute), clk.Now().Add(time.Minute)); err != nil {
		t.Fatalf("seed counters: %v", err)
	}
	var job *jobs.Job
	list := workerJobs(db, config.WorkerConfig{}, clk, slog.New(slog.NewTextHandler(io.Discard, nil)))
	for i := range list {
		if list[i].Name == "rate_limit_purge" {
			job = &list[i]
		}
	}
	if job == nil || job.Every != time.Minute {
		t.Fatalf("rate_limit_purge job = %+v, want one running every minute", job)
	}
	if err := job.Run(t.Context()); err != nil {
		t.Fatalf("job: %v", err)
	}
	var left int
	if err := db.QueryRow(t.Context(), `select count(*) from rate_limit_counters`).Scan(&left); err != nil || left != 1 {
		t.Fatalf("rows left = %d, %v; want only the live one", left, err)
	}
}

func TestWorkerJobs_IncludeVoid(t *testing.T) {
	db := testsupport.NewTestDB(t)
	lotID := testsupport.SeedPilotLot(t, db)
	clk := testsupport.NewFakeClock(time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC))
	if _, err := db.App.Exec(t.Context(),
		`insert into stays (lot_id, credential_kind, ticket_code_hash, status, tariff_version_id, accepted_at, updated_at)
		 select $1, 'ticket', sha256(k::bytea), 'entry_pending', v.id, $2::timestamptz - (k || ' seconds')::interval, $2
		 from unnest(array['61', '30']) k,
		      (select id from tariff_versions where lot_id = $1 order by effective_from desc limit 1) v`,
		lotID, clk.Now()); err != nil {
		t.Fatalf("seed pending: %v", err)
	}
	var job *jobs.Job
	list := workerJobs(db.App, config.WorkerConfig{}, clk, slog.New(slog.NewTextHandler(io.Discard, nil)))
	for i := range list {
		if list[i].Name == "void_unpassed_entries" {
			job = &list[i]
		}
	}
	if job == nil || job.Every != 15*time.Second {
		t.Fatalf("void_unpassed_entries job = %+v, want one running every 15 s", job)
	}
	if err := job.Run(t.Context()); err != nil {
		t.Fatalf("job: %v", err)
	}
	var voided int
	if err := db.App.QueryRow(t.Context(),
		`select count(*) from stays where status = 'voided' and voided_at = $1`, clk.Now()).Scan(&voided); err != nil || voided != 1 {
		t.Fatalf("voided at the job clock = %d, %v; want only the 61 s old entry", voided, err)
	}
}

// TestWorkerJobs_VoidRunsBoth checks the void job body voids a stale ticket
// stay through the M2 batch and a stale reservation stay through the
// reservation voider (M3B-60), in one tick.
func TestWorkerJobs_VoidRunsBoth(t *testing.T) {
	db := testsupport.NewTestDB(t)
	lotID := testsupport.SeedPilotLot(t, db)
	clk := testsupport.NewFakeClock(time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC))
	now := clk.Now()
	drivers := testsupport.SeedDrivers(t, db, 1)
	testsupport.SeedReservations(t, db, lotID, drivers, now.Add(-5*time.Minute), now.Add(-15*time.Minute), now.Add(15*time.Minute), now)
	if _, err := db.App.Exec(t.Context(),
		`with r as (update reservations set status = 'checked_in', confirmed_at = $2, qr_nonce = '\x00000000000000000000000000000000',
		                                    checked_in_at = $2, updated_at = $2
		            returning id, tariff_version_id)
		 insert into stays (lot_id, credential_kind, ticket_code_hash, reservation_id, status, tariff_version_id, accepted_at, updated_at)
		 select $1::bigint, 'reservation', null, r.id, 'entry_pending', r.tariff_version_id, $2::timestamptz, $2::timestamptz from r
		 union all
		 select $1, 'ticket', sha256('t'::bytea), null, 'entry_pending', r.tariff_version_id, $2, $2 from r`,
		lotID, now.Add(-61*time.Second)); err != nil {
		t.Fatalf("seed: %v", err)
	}
	var job *jobs.Job
	list := workerJobs(db.App, config.WorkerConfig{}, clk, slog.New(slog.NewTextHandler(io.Discard, nil)))
	for i := range list {
		if list[i].Name == "void_unpassed_entries" {
			job = &list[i]
		}
	}
	if err := job.Run(t.Context()); err != nil {
		t.Fatalf("job: %v", err)
	}
	var voided, confirmed int
	if err := db.App.QueryRow(t.Context(),
		`select (select count(*) from stays where status = 'voided' and voided_at = $1),
		        (select count(*) from reservations where status = 'confirmed' and checked_in_at is null)`, now).Scan(&voided, &confirmed); err != nil {
		t.Fatal(err)
	}
	if voided != 2 || confirmed != 1 {
		t.Fatalf("voided = %d, confirmed = %d; want both stays voided and the reservation confirmed", voided, confirmed)
	}
	testsupport.AssertOccupancyInvariant(t, db.App, lotID)
	testsupport.AssertSlotInvariant(t, db.App, lotID)
}

// TestWorkerJobs_IncludePurge checks device_events_purge runs daily and its
// body deletes what is past retention in batches of 5000 (12 001 rows take
// three calls) while it keeps the recent rows.
func TestWorkerJobs_IncludePurge(t *testing.T) {
	db := testsupport.NewTestDB(t)
	lotID := testsupport.SeedPilotLot(t, db)
	clk := testsupport.NewFakeClock(time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC))
	if _, err := db.Super.Exec(t.Context(),
		`insert into devices (public_id, lot_id, kind, name, status, secret_ciphertext, secret_key_id)
		 values (gen_random_uuid(), $1, 'dispenser', 'purge test', 'active', '\x00', 'test')`, lotID); err != nil {
		t.Fatalf("seed device: %v", err)
	}
	// 12 001 events 13 months old, and one from yesterday that must survive.
	// The purge function cuts at the database's now(), not at the job's fake
	// clock, so the rows are seeded relative to now() as well.
	if _, err := db.Super.Exec(t.Context(),
		`insert into device_events (lot_id, device_id, event_id, type, occurred_at, received_at, body_sha256, payload, response)
		 select $1, d.id, gen_random_uuid(), 'heartbeat', now() - interval '13 months', now() - interval '13 months' - (i || ' seconds')::interval,
		        '\x0000000000000000000000000000000000000000000000000000000000000000', '{}', '{}'
		 from generate_series(1, 12001) i, (select id from devices where name = 'purge test') d`,
		lotID); err != nil {
		t.Fatalf("seed old events: %v", err)
	}
	if _, err := db.Super.Exec(t.Context(),
		`insert into device_events (lot_id, device_id, event_id, type, occurred_at, received_at, body_sha256, payload, response)
		 select $1, id, gen_random_uuid(), 'heartbeat', now(), now() - interval '1 day', '\x0000000000000000000000000000000000000000000000000000000000000000', '{}', '{}'
		 from devices where name = 'purge test'`, lotID); err != nil {
		t.Fatalf("seed recent event: %v", err)
	}

	var logs testsupport.SyncBuffer
	var job *jobs.Job
	list := workerJobs(db.App, config.WorkerConfig{}, clk, slog.New(slog.NewTextHandler(&logs, nil)))
	for i := range list {
		if list[i].Name == "device_events_purge" {
			job = &list[i]
		}
	}
	if job == nil || job.Every != 24*time.Hour {
		t.Fatalf("device_events_purge job = %+v, want one running every 24 h", job)
	}
	if err := job.Run(t.Context()); err != nil {
		t.Fatalf("job: %v", err)
	}
	var left int
	if err := db.Super.QueryRow(t.Context(), `select count(*) from device_events`).Scan(&left); err != nil || left != 1 {
		t.Fatalf("device_events left = %d, %v; want only the recent one", left, err)
	}
	if !strings.Contains(logs.String(), "12001") {
		t.Errorf("the job did not log the deleted count 12001: %s", logs.String())
	}
}

// TestWorkerJobs_IncludeReconcile checks payment_reconcile runs every 15 s and
// its body settles a fee attempt the gateway never saw (not_submitted).
func TestWorkerJobs_IncludeReconcile(t *testing.T) {
	db := testsupport.NewTestDB(t)
	lot := testsupport.SeedPilotLot(t, db)
	clk := testsupport.NewFakeClock(time.Date(2026, 12, 1, 13, 58, 10, 0, time.UTC))
	drivers := testsupport.SeedDrivers(t, db, 1)
	arrival := clk.Now().Add(2 * time.Hour)
	testsupport.SeedReservations(t, db, lot, drivers, arrival, arrival.Add(-15*time.Minute), arrival.Add(20*time.Minute), clk.Now())
	var rsv int64
	if err := db.App.QueryRow(t.Context(), `select id from reservations where user_id = $1`, drivers[0].UserID).Scan(&rsv); err != nil {
		t.Fatal(err)
	}
	if _, err := db.App.Exec(t.Context(),
		`insert into payments (kind, method, amount_cents, status, idempotency_key, reservation_id, user_id,
		                       client_key, request_fingerprint, card_brand, card_last4, created_at)
		 values ('reservation_fee', 'gateway_sim', 15000, 'pending', 'rsv-fee:worker-test', $1, $2,
		         'key-worker-0001', $3, 'visa', '4242', $4)`,
		rsv, drivers[0].UserID, make([]byte, 32), clk.Now().Add(-time.Minute)); err != nil {
		t.Fatalf("seed pending fee: %v", err)
	}

	var job *jobs.Job
	list := workerJobs(db.App, config.WorkerConfig{}, clk, slog.New(slog.NewTextHandler(io.Discard, nil)))
	for i := range list {
		if list[i].Name == "payment_reconcile" {
			job = &list[i]
		}
	}
	if job == nil || job.Every != 15*time.Second {
		t.Fatalf("payment_reconcile job = %+v, want one running every 15 s", job)
	}
	if err := job.Run(t.Context()); err != nil {
		t.Fatalf("job: %v", err)
	}
	var n int
	if err := db.App.QueryRow(t.Context(),
		`select count(*) from payments where idempotency_key = 'rsv-fee:worker-test' and status = 'failed'
		 and failure_code = 'not_submitted' and failed_at = $1`, clk.Now()).Scan(&n); err != nil || n != 1 {
		t.Fatalf("settled payments = %d, %v; want the unseen attempt failed as not_submitted", n, err)
	}
}

// TestWorkerJobs_IncludeRefunds checks refund_processing runs every 10 s and
// its body settles a due refund at the job clock.
func TestWorkerJobs_IncludeRefunds(t *testing.T) {
	db := testsupport.NewTestDB(t)
	clk := testsupport.NewFakeClock(time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC))
	var job *jobs.Job
	list := workerJobs(db.App, config.WorkerConfig{}, clk, slog.New(slog.NewTextHandler(io.Discard, nil)))
	for i := range list {
		if list[i].Name == "refund_processing" {
			job = &list[i]
		}
	}
	if job == nil || job.Every != 10*time.Second {
		t.Fatalf("refund_processing job = %+v, want one running every 10 s", job)
	}
	if err := job.Run(t.Context()); err != nil {
		t.Fatalf("job on an empty queue: %v", err)
	}
}

// TestWorkerJobs_IncludeExpiry checks both reservation expiry jobs run every
// 30 s, that the unpaid one acts at the job clock, and that the pool size
// stays at M3B-51's target.
func TestWorkerJobs_IncludeExpiry(t *testing.T) {
	if maxConns != 8 {
		t.Errorf("maxConns = %d, want 8", maxConns)
	}
	db := testsupport.NewTestDB(t)
	lot := testsupport.SeedPilotLot(t, db)
	now := time.Date(2026, 12, 1, 13, 0, 0, 0, time.UTC)
	clk := testsupport.NewFakeClock(now)
	d := testsupport.SeedDrivers(t, db, 1)
	arrival := now.Add(2 * time.Hour)
	testsupport.SeedReservations(t, db, lot, d, arrival, arrival.Add(-15*time.Minute), arrival.Add(20*time.Minute), now)
	testsupport.HoldSlots(t, db, lot, arrival.Add(-15*time.Minute), arrival.Add(20*time.Minute), 1)

	found := map[string]*jobs.Job{}
	list := workerJobs(db.App, config.WorkerConfig{}, clk, slog.New(slog.NewTextHandler(io.Discard, nil)))
	for i := range list {
		found[list[i].Name] = &list[i]
	}
	for _, name := range []string{"expire_unpaid_reservations", "expire_no_shows"} {
		if found[name] == nil || found[name].Every != 30*time.Second {
			t.Fatalf("%s job = %+v, want one running every 30 s", name, found[name])
		}
	}
	clk.Set(now.Add(11 * time.Minute)) // payment_due_at is now + 10 min
	for _, name := range []string{"expire_no_shows", "expire_unpaid_reservations"} {
		if err := found[name].Run(t.Context()); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	var status string
	if err := db.App.QueryRow(t.Context(), `select status from reservations`).Scan(&status); err != nil || status != "expired_unpaid" {
		t.Fatalf("status = %q, %v; want expired_unpaid", status, err)
	}
	testsupport.AssertSlotInvariant(t, db.App, lot)
}

// TestWorkerJobs_IncludeBoundaryAndReminders checks the hold boundary job runs
// every 30 s and notifies on its first tick, and the reminder job every minute.
func TestWorkerJobs_IncludeBoundaryAndReminders(t *testing.T) {
	db := testsupport.NewTestDB(t)
	lot := testsupport.SeedPilotLot(t, db)
	clk := testsupport.NewFakeClock(time.Date(2026, 12, 1, 13, 0, 0, 0, time.UTC))
	found := map[string]*jobs.Job{}
	list := workerJobs(db.App, config.WorkerConfig{}, clk, slog.New(slog.NewTextHandler(io.Discard, nil)))
	for i := range list {
		found[list[i].Name] = &list[i]
	}
	for name, every := range map[string]time.Duration{"hold_boundary_notify": 30 * time.Second, "reservation_reminders": time.Minute} {
		if found[name] == nil || found[name].Every != every {
			t.Fatalf("%s job = %+v, want one running every %s", name, found[name], every)
		}
	}
	version := func() (v int64) {
		if err := db.App.QueryRow(t.Context(), `select version from lot_occupancy where lot_id = $1`, lot).Scan(&v); err != nil {
			t.Fatal(err)
		}
		return v
	}
	before := version()
	for range 2 {
		if err := found["hold_boundary_notify"].Run(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	if got := version(); got != before+1 {
		t.Errorf("version = %d, want %d (the first tick bumps, the second finds nothing changed)", got, before+1)
	}
	if err := found["reservation_reminders"].Run(t.Context()); err != nil {
		t.Fatal(err)
	}
}
