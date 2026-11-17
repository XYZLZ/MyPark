package reservations_test

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"

	notificationspg "github.com/XYZLZ/MyPark/apps/server/internal/features/notifications/postgres"
	paymentspg "github.com/XYZLZ/MyPark/apps/server/internal/features/payments/postgres"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/reservations"
	reservationspg "github.com/XYZLZ/MyPark/apps/server/internal/features/reservations/postgres"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/database"
	"github.com/XYZLZ/MyPark/apps/server/internal/testsupport"
)

var jobsNow = time.Date(2026, 12, 1, 15, 0, 0, 0, time.UTC)

type jobEnv struct {
	t    *testing.T
	db   *testsupport.TestDB
	lot  int64
	jobs *reservations.Jobs
}

// hookRepo runs before locks the candidate, so a test can act between the
// job's unlocked list and its lock.
type hookRepo struct {
	reservations.Repo
	before func(id int64)
}

func (r *hookRepo) LockDueUnpaid(ctx context.Context, id int64, now time.Time) (reservations.ApplyRow, bool, error) {
	if r.before != nil {
		r.before(id)
	}
	return r.Repo.LockDueUnpaid(ctx, id, now)
}

func newJobEnv(t *testing.T, hook func(id int64)) *jobEnv {
	t.Helper()
	db := testsupport.NewTestDB(t)
	clk := testsupport.NewFakeClock(jobsNow)
	var repo reservations.Repo = reservationspg.NewRepo(db.App)
	if hook != nil {
		repo = &hookRepo{Repo: repo, before: hook}
	}
	return &jobEnv{t: t, db: db, lot: testsupport.SeedPilotLot(t, db), jobs: reservations.NewJobs(reservations.Deps{
		Tx: database.NewTxRunner(db.App), Repo: repo, Clock: clk, Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Fees: paymentspg.NewRepo(db.App), Notify: notificationspg.NewNotifier(clk),
	})}
}

func (e *jobEnv) exec(sql string, args ...any) {
	e.t.Helper()
	if _, err := e.db.App.Exec(e.t.Context(), sql, args...); err != nil {
		e.t.Fatal(err)
	}
}

// rsv inserts a reservation of the driver whose window is [end-35m, end),
// holding its slots when it is pending_payment or confirmed.
func (e *jobEnv) rsv(d testsupport.Driver, status string, due, end time.Time) int64 {
	e.t.Helper()
	var id int64
	if err := e.db.App.QueryRow(e.t.Context(),
		`insert into reservations (user_id, vehicle_id, lot_id, arrival_at, hold_window, status, fee_cents,
		        tariff_version_id, policy_version_id, payment_due_at, created_at, updated_at, confirmed_at, qr_nonce, expired_at)
		 select $1, v.id, $2, $4::timestamptz - interval '20 minutes', tstzrange($4::timestamptz - interval '35 minutes', $4, '[)'),
		        $3, 15000, (select id from tariff_versions where lot_id = $2 order by effective_from desc limit 1),
		        (select id from policy_versions where lot_id = $2 order by effective_from desc limit 1), $5, $5, $5,
		        case when $3 = 'confirmed' then $5::timestamptz end, case when $3 = 'confirmed' then '\x00112233445566778899aabbccddeeff'::bytea end,
		        case when $3 like 'expired%' then $5::timestamptz end
		 from vehicles v where v.public_id = $6 returning id`,
		d.UserID, e.lot, status, end, due, d.VehicleID).Scan(&id); err != nil {
		e.t.Fatal(err)
	}
	if status == "pending_payment" || status == "confirmed" {
		e.exec(`insert into reservation_slot_loads (lot_id, slot_start, held)
		        select $1, s, 1 from generate_series($2::timestamptz - interval '35 minutes', $2::timestamptz - interval '5 minutes', interval '5 minutes') s
		        on conflict (lot_id, slot_start) do update set held = reservation_slot_loads.held + 1`, e.lot, end)
	}
	return id
}

func (e *jobEnv) status(id int64) (status string) {
	e.t.Helper()
	if err := e.db.App.QueryRow(e.t.Context(), `select status from reservations where id = $1`, id).Scan(&status); err != nil {
		e.t.Fatal(err)
	}
	return status
}

func (e *jobEnv) count(sql string, args ...any) (n int) {
	e.t.Helper()
	if err := e.db.App.QueryRow(e.t.Context(), sql, args...).Scan(&n); err != nil {
		e.t.Fatal(err)
	}
	return n
}

func (e *jobEnv) held() int {
	return e.count(`select coalesce(sum(held), 0)::int from reservation_slot_loads`)
}
func (e *jobEnv) version() int {
	return e.count(`select version from lot_occupancy where lot_id = $1`, e.lot)
}

// fee inserts a reservation fee attempt in the given status.
func (e *jobEnv) fee(rsvID int64, d testsupport.Driver, status string) {
	e.t.Helper()
	e.exec(`insert into payments (kind, method, amount_cents, status, idempotency_key, reservation_id, user_id,
	                              client_key, request_fingerprint, card_brand, card_last4, failure_code, failed_at)
	        values ('reservation_fee', 'gateway_sim', 15000, $1, $2, $3, $4, 'key-jobs-0001', $5, 'visa', '4242',
	                case when $1 = 'failed' then 'card_declined' end, case when $1 = 'failed' then now() end)`,
		status, "rsv-fee:"+uuid.NewString(), rsvID, d.UserID, make([]byte, 32))
}

func (e *jobEnv) done() {
	testsupport.AssertSlotInvariant(e.t, e.db.App, e.lot)
}

func run(t *testing.T, f func(context.Context, time.Time) (int, error), want int) {
	t.Helper()
	if n, err := f(t.Context(), jobsNow); err != nil || n != want {
		t.Fatalf("job = %d, %v; want %d, nil", n, err, want)
	}
}

func TestExpireUnpaid(t *testing.T) {
	e := newJobEnv(t, nil)
	d := testsupport.SeedDrivers(t, e.db, 2)
	due := e.rsv(d[0], "pending_payment", jobsNow, jobsNow.Add(time.Hour))
	later := e.rsv(d[1], "pending_payment", jobsNow.Add(time.Second), jobsNow.Add(time.Hour))
	before := e.version()
	run(t, e.jobs.ExpireUnpaid, 1)
	if s := e.status(due); s != "expired_unpaid" {
		t.Errorf("due status = %s, want expired_unpaid", s)
	}
	if s := e.status(later); s != "pending_payment" {
		t.Errorf("not yet due status = %s, want pending_payment", s)
	}
	if n := e.count(`select count(*) from reservations where id = $1 and expired_at = $2 and refund_reason is null`, due, jobsNow); n != 1 {
		t.Error("want expired_at = now and no refund reason")
	}
	if got := e.held(); got != 7 {
		t.Errorf("held = %d, want 7 (one reservation's 7 slots released)", got)
	}
	if v := e.version(); v != before {
		t.Errorf("lot version %d -> %d, expiry jobs never bump", before, v)
	}
	e.done()
}

func TestExpireUnpaid_SkipsPendingAttempt(t *testing.T) {
	e := newJobEnv(t, nil)
	d := testsupport.SeedDrivers(t, e.db, 1)[0]
	id := e.rsv(d, "pending_payment", jobsNow, jobsNow.Add(time.Hour))
	e.fee(id, d, "pending")
	run(t, e.jobs.ExpireUnpaid, 0)
	if s := e.status(id); s != "pending_payment" {
		t.Fatalf("status = %s, want pending_payment while a charge may be in flight", s)
	}
	e.exec(`update payments set status = 'failed', failure_code = 'card_declined', failed_at = now() where reservation_id = $1`, id)
	run(t, e.jobs.ExpireUnpaid, 1)
	if s := e.status(id); s != "expired_unpaid" {
		t.Errorf("status = %s, want expired_unpaid once the attempt failed", s)
	}
	e.done()
}

func TestExpireUnpaid_RaceWithPay(t *testing.T) {
	var e *jobEnv
	var d testsupport.Driver
	e = newJobEnv(t, func(id int64) { e.fee(id, d, "pending") })
	d = testsupport.SeedDrivers(t, e.db, 1)[0]
	id := e.rsv(d, "pending_payment", jobsNow, jobsNow.Add(time.Hour))
	// The attempt commits after the job listed the row and before it locks it.
	run(t, e.jobs.ExpireUnpaid, 0)
	if s := e.status(id); s != "pending_payment" {
		t.Errorf("status = %s, want pending_payment: the re-check under the lock sees the attempt", s)
	}
	e.done()
}

func TestExpireNoShows_AtUpperExactly(t *testing.T) {
	e := newJobEnv(t, nil)
	d := testsupport.SeedDrivers(t, e.db, 2)
	closed := e.rsv(d[0], "confirmed", jobsNow.Add(-time.Hour), jobsNow)
	open := e.rsv(d[1], "confirmed", jobsNow.Add(-time.Hour), jobsNow.Add(5*time.Minute))
	before := e.version()
	if n, err := e.jobs.ExpireNoShows(t.Context(), jobsNow.Add(-time.Second)); err != nil || n != 0 {
		t.Fatalf("upper - 1s: expired %d, %v; want 0", n, err)
	}
	run(t, e.jobs.ExpireNoShows, 1)
	if s := e.status(closed); s != "expired_no_show" {
		t.Errorf("now == upper: status = %s, want expired_no_show", s)
	}
	if s := e.status(open); s != "confirmed" {
		t.Errorf("upper > now: status = %s, want confirmed", s)
	}
	if got := e.held(); got != 7 {
		t.Errorf("held = %d, want 7", got)
	}
	if n := e.count(`select count(*) from outbox_messages where template = 'reservation_no_show' and user_id = $1
	                  and not payload ? 'blocked_until'`, d[0].UserID); n != 1 {
		t.Errorf("no-show emails = %d, want 1 without blocked_until", n)
	}
	if v := e.version(); v != before {
		t.Errorf("lot version %d -> %d, expiry jobs never bump", before, v)
	}
	// The queue claims nothing twice.
	run(t, e.jobs.ExpireNoShows, 0)
	e.done()
}

// seedNoShows gives the driver n expired_no_show reservations, each ageDays old.
func (e *jobEnv) seedNoShows(d testsupport.Driver, n, ageDays int) {
	e.t.Helper()
	for range n {
		id := e.rsv(d, "expired_no_show", jobsNow.AddDate(0, 0, -ageDays), jobsNow.AddDate(0, 0, -ageDays))
		e.exec(`update reservations set expired_at = $2 where id = $1`, id, jobsNow.AddDate(0, 0, -ageDays))
	}
}

func (e *jobEnv) blockedUntil(d testsupport.Driver) (until *time.Time) {
	e.t.Helper()
	if err := e.db.App.QueryRow(e.t.Context(), `select reservation_blocked_until from users where id = $1`, d.UserID).Scan(&until); err != nil {
		e.t.Fatal(err)
	}
	return until
}

func TestNoShow_BlocksAfterThreshold(t *testing.T) {
	e := newJobEnv(t, nil)
	d := testsupport.SeedDrivers(t, e.db, 1)[0]
	e.seedNoShows(d, 2, 5)
	if u := e.blockedUntil(d); u != nil {
		t.Fatalf("blocked before the third no-show: %v", u)
	}
	e.rsv(d, "confirmed", jobsNow.Add(-time.Hour), jobsNow)
	run(t, e.jobs.ExpireNoShows, 1)
	want := jobsNow.AddDate(0, 0, 7)
	if u := e.blockedUntil(d); u == nil || !u.Equal(want) {
		t.Errorf("blocked until %v, want %v", u, want)
	}
	if n := e.count(`select count(*) from outbox_messages where template = 'reservation_no_show' and payload ->> 'blocked_until' = '2026-12-08 11:00'`); n != 1 {
		t.Error("the email must carry blocked_until (lot time)")
	}
	e.done()
}

func TestNoShow_WindowAndGreatest(t *testing.T) {
	e := newJobEnv(t, nil)
	old := testsupport.SeedDrivers(t, e.db, 1)[0]
	e.seedNoShows(old, 2, 31)
	e.rsv(old, "confirmed", jobsNow.Add(-time.Hour), jobsNow)
	run(t, e.jobs.ExpireNoShows, 1)
	if u := e.blockedUntil(old); u != nil {
		t.Errorf("no-shows 31 days old counted: blocked until %v", u)
	}

	kept := testsupport.SeedDrivers(t, e.db, 1)[0]
	e.seedNoShows(kept, 2, 5)
	later := jobsNow.AddDate(0, 0, 20)
	e.exec(`update users set reservation_blocked_until = $2 where id = $1`, kept.UserID, later)
	e.rsv(kept, "confirmed", jobsNow.Add(-time.Hour), jobsNow)
	run(t, e.jobs.ExpireNoShows, 1)
	if u := e.blockedUntil(kept); u == nil || !u.Equal(later) {
		t.Errorf("blocked until %v, want the later existing block %v", u, later)
	}
	e.done()
}

func TestNoShow_CapturedPolicy(t *testing.T) {
	e := newJobEnv(t, nil)
	d := testsupport.SeedDrivers(t, e.db, 1)[0]
	e.seedNoShows(d, 1, 5)
	e.rsv(d, "confirmed", jobsNow.Add(-time.Hour), jobsNow) // captures the seeded policy (threshold 3)
	e.exec(`insert into policy_versions (lot_id, effective_from, max_reservable_pct, booking_horizon_days, arrival_grace_min,
	            early_arrival_min, payment_timeout_min, max_active_per_user, slot_min, noshow_block_threshold, noshow_window_days,
	            noshow_block_days, created_by)
	        select lot_id, $2, max_reservable_pct, booking_horizon_days, arrival_grace_min, early_arrival_min, payment_timeout_min,
	               max_active_per_user, slot_min, 2, noshow_window_days, noshow_block_days, created_by
	        from policy_versions where lot_id = $1 order by effective_from desc limit 1`, e.lot, jobsNow.Add(-time.Minute))
	run(t, e.jobs.ExpireNoShows, 1)
	if u := e.blockedUntil(d); u != nil {
		t.Errorf("blocked until %v: the newer threshold 2 applied instead of the captured 3", u)
	}
	e.done()
}

func TestJobs_Batches(t *testing.T) {
	e := newJobEnv(t, nil)
	for _, d := range testsupport.SeedDrivers(t, e.db, 450) {
		e.rsv(d, "pending_payment", jobsNow.Add(-time.Minute), jobsNow.Add(time.Hour))
	}
	run(t, e.jobs.ExpireUnpaid, 450)
	if got := e.held(); got != 0 {
		t.Errorf("held = %d, want 0", got)
	}
	e.done()
}
