package database_test

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/XYZLZ/MyPark/apps/server/internal/testsupport"
)

const (
	insertCard = `insert into employee_cards (lot_id, card_uid_hash, holder_name, employee_code)
		values ($1, $2, 'Empleado Prueba', $3) returning id`
	// $1 lot, $2 kind, $3 ticket hash, $4 card id, $5 status, $6 tariff,
	// $7 entered_at, $8 paid_at, $9 exit_deadline_at, $10 exited_at,
	// $11 voided_at, $12 lost_ticket.
	insertStayRow = `insert into stays (lot_id, credential_kind, ticket_code_hash, employee_card_id, status,
		tariff_version_id, entered_at, paid_at, exit_deadline_at, exited_at, voided_at, lost_ticket)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`
)

type stayFixture struct {
	pool            *pgxpool.Pool
	lotID, tariffID int64
}

func newStayFixture(t *testing.T) stayFixture {
	t.Helper()
	db := testsupport.NewTestDB(t)
	lotID := testsupport.SeedPilotLot(t, db)
	var tariffID int64
	if err := db.App.QueryRow(t.Context(), "select id from tariff_versions where lot_id = $1", lotID).Scan(&tariffID); err != nil {
		t.Fatalf("tariff id: %v", err)
	}
	return stayFixture{pool: db.App, lotID: lotID, tariffID: tariffID}
}

func (f stayFixture) card(t *testing.T, tag byte) int64 {
	t.Helper()
	hash := make([]byte, 32)
	hash[0] = tag
	var id int64
	if err := f.pool.QueryRow(t.Context(), insertCard, f.lotID, hash, "E"+string(rune('0'+tag))).Scan(&id); err != nil {
		t.Fatalf("insert card: %v", err)
	}
	return id
}

// stay inserts a stay; nil means NULL.
// stayErr is stay returning the Postgres error itself.
func (f stayFixture) stayErr(t *testing.T, kind string, ticket []byte, card any, status string, entered, paid, deadline, exited, voided any, lost bool) *pgconn.PgError {
	t.Helper()
	var tk any
	if ticket != nil {
		tk = ticket
	}
	return pgError(t, f.pool, insertStayRow, f.lotID, kind, tk, card, status, f.tariffID, entered, paid, deadline, exited, voided, lost)
}

func (f stayFixture) stay(t *testing.T, kind string, ticket []byte, card any, status string, entered, paid, deadline, exited, voided any, lost bool) string {
	t.Helper()
	var tk any
	if ticket != nil {
		tk = ticket
	}
	return execCode(t, f.pool, insertStayRow, f.lotID, kind, tk, card, status, f.tariffID, entered, paid, deadline, exited, voided, lost)
}

func grantsGate(t *testing.T) {
	f := newStayFixture(t)
	cardID := f.card(t, 1)
	if got := f.stay(t, "employee_card", nil, cardID, "entry_pending", nil, nil, nil, nil, nil, false); got != "" {
		t.Fatalf("insert stays as mypark_app: SQLSTATE %q", got)
	}
	var stayID int64
	if err := f.pool.QueryRow(t.Context(), "select id from stays").Scan(&stayID); err != nil {
		t.Fatalf("select stays: %v", err)
	}
	if got := execCode(t, f.pool, "update stays set updated_at = now() where id = $1", stayID); got != "" {
		t.Errorf("update stays: SQLSTATE %q", got)
	}
	if got := execCode(t, f.pool, "update employee_cards set status = 'active' where id = $1", cardID); got != "" {
		t.Errorf("update employee_cards: SQLSTATE %q", got)
	}
	if got := execCode(t, f.pool, `insert into payments (kind, stay_id, method, amount_cents, status, idempotency_key, succeeded_at)
		values ('parking_fee', $1, 'cash', 100, 'succeeded', 'k-grants', now())`, stayID); got != "" {
		t.Errorf("insert payments: SQLSTATE %q", got)
	}
	if got := execCode(t, f.pool, "update payments set succeeded_at = now()"); got != "" {
		t.Errorf("update payments: SQLSTATE %q", got)
	}
	// Financial records are kept (policies 1): no delete, no truncate.
	for _, stmt := range []string{"delete from payments", "delete from stays", "truncate payments", "truncate stays"} {
		if got := execCode(t, f.pool, stmt); got != sqlStateInsufficientPrivilege {
			t.Errorf("%q as mypark_app: SQLSTATE %q, want %s", stmt, got, sqlStateInsufficientPrivilege)
		}
	}
	for _, table := range []string{"employee_cards", "stays", "payments"} {
		if got := execCode(t, f.pool, "select count(*) from "+table); got != "" {
			t.Errorf("select %s: SQLSTATE %q", table, got)
		}
	}
}

func TestStaysSchema_StateChecks(t *testing.T) {
	f := newStayFixture(t)
	now := time.Now()
	hash := make([]byte, 32)
	cardID := f.card(t, 1)
	cases := []struct {
		name, constraint string
		err              *pgconn.PgError
	}{
		{"active without entered_at", "stays_entered_check", f.stayErr(t, "ticket", hash, nil, "active", nil, nil, nil, nil, nil, false)},
		{"paid without paid_at and exit_deadline_at", "stays_paid_check", f.stayErr(t, "ticket", hash, nil, "paid", now, nil, nil, nil, nil, false)},
		{"paid without exit_deadline_at", "stays_paid_check", f.stayErr(t, "ticket", hash, nil, "paid", now, now, nil, nil, nil, false)},
		{"exit_deadline_at without paid_at", "stays_deadline_needs_paid_check", f.stayErr(t, "ticket", hash, nil, "active", now, nil, now, nil, nil, false)},
		{"exited without exited_at", "stays_exited_check", f.stayErr(t, "ticket", hash, nil, "exited", now, nil, nil, nil, nil, false)},
		{"voided without voided_at", "stays_voided_check", f.stayErr(t, "ticket", hash, nil, "voided", nil, nil, nil, nil, nil, false)},
		{"ticket without ticket_code_hash", "stays_ticket_hash_check", f.stayErr(t, "ticket", nil, nil, "entry_pending", nil, nil, nil, nil, nil, false)},
		{"card with a ticket hash", "stays_ticket_hash_check", f.stayErr(t, "employee_card", hash, cardID, "entry_pending", nil, nil, nil, nil, nil, false)},
		{"employee_card without card id", "stays_card_id_check", f.stayErr(t, "employee_card", nil, nil, "entry_pending", nil, nil, nil, nil, nil, false)},
		{"lost_ticket on employee_card", "stays_lost_ticket_check", f.stayErr(t, "employee_card", nil, cardID, "entry_pending", nil, nil, nil, nil, nil, true)},
		{"unknown status", "stays_status_check", f.stayErr(t, "ticket", hash, nil, "lost", nil, nil, nil, nil, nil, false)},
	}
	for _, tc := range cases {
		if tc.err == nil || tc.err.Code != sqlStateCheckViolation || tc.err.ConstraintName != tc.constraint {
			t.Errorf("%s = %+v, want 23514 %s", tc.name, tc.err, tc.constraint)
		}
	}
	ok := []struct {
		name string
		got  string
	}{
		{"valid paid", f.stay(t, "ticket", hash, nil, "paid", now, now, now.Add(time.Minute), nil, nil, false)},
		{"valid lost ticket", f.stay(t, "ticket", []byte("x"), nil, "active", now, nil, nil, nil, nil, true)},
	}
	for _, tc := range ok {
		if tc.got != "" {
			t.Errorf("%s: SQLSTATE %q, want success", tc.name, tc.got)
		}
	}
}

func TestStaysSchema_OneOpenStayPerCard(t *testing.T) {
	f := newStayFixture(t)
	cardID := f.card(t, 1)
	now := time.Now()
	if got := f.stay(t, "employee_card", nil, cardID, "entry_pending", nil, nil, nil, nil, nil, false); got != "" {
		t.Fatalf("first stay: SQLSTATE %q", got)
	}
	e := pgError(t, f.pool, insertStayRow, f.lotID, "employee_card", nil, cardID, "entry_pending", f.tariffID, nil, nil, nil, nil, nil, false)
	if e == nil || e.Code != sqlStateUniqueViolation || e.ConstraintName != "stays_card_open_key" {
		t.Fatalf("second open stay = %+v, want 23505 stays_card_open_key", e)
	}
	if got := execCode(t, f.pool, "update stays set status = 'exited', entered_at = $1, exited_at = $1", now); got != "" {
		t.Fatalf("exit first stay: SQLSTATE %q", got)
	}
	if got := f.stay(t, "employee_card", nil, cardID, "entry_pending", nil, nil, nil, nil, nil, false); got != "" {
		t.Errorf("stay after the first exited: SQLSTATE %q, want success", got)
	}
}

func TestPaymentsSchema(t *testing.T) {
	f := newStayFixture(t)
	if got := f.stay(t, "ticket", make([]byte, 32), nil, "entry_pending", nil, nil, nil, nil, nil, false); got != "" {
		t.Fatalf("stay: SQLSTATE %q", got)
	}
	var stayID int64
	if err := f.pool.QueryRow(t.Context(), "select id from stays").Scan(&stayID); err != nil {
		t.Fatal(err)
	}
	const ins = `insert into payments (kind, stay_id, method, amount_cents, status, idempotency_key, succeeded_at)
		values ('parking_fee', $1, $2, $3, 'succeeded', $4, now())`
	if got := execCode(t, f.pool, ins, stayID, "none", 100, "a"); got != sqlStateCheckViolation {
		t.Errorf("none with 100: SQLSTATE %q, want %s", got, sqlStateCheckViolation)
	}
	if got := execCode(t, f.pool, ins, stayID, "cash", 0, "b"); got != sqlStateCheckViolation {
		t.Errorf("cash with 0: SQLSTATE %q, want %s", got, sqlStateCheckViolation)
	}
	if got := execCode(t, f.pool, ins, stayID, "cash", -5, "c"); got != sqlStateCheckViolation {
		t.Errorf("negative amount: SQLSTATE %q, want %s", got, sqlStateCheckViolation)
	}
	if got := execCode(t, f.pool, ins, stayID, "none", 0, "zero:1"); got != "" {
		t.Fatalf("first zero payment: SQLSTATE %q", got)
	}
	e := pgError(t, f.pool, ins, stayID, "none", 0, "zero:2")
	if e == nil || e.Code != sqlStateUniqueViolation || e.ConstraintName != "payments_zero_per_stay_key" {
		t.Errorf("second zero payment = %+v, want 23505 payments_zero_per_stay_key", e)
	}
	for i, key := range []string{"cash:1", "cash:2"} {
		if got := execCode(t, f.pool, ins, stayID, "cash", 100+i, key); got != "" {
			t.Errorf("cash payment %s: SQLSTATE %q, want success", key, got)
		}
	}
	if got := execCode(t, f.pool, ins, stayID, "cash", 100, "cash:1"); got != sqlStateUniqueViolation {
		t.Errorf("duplicate idempotency key: SQLSTATE %q, want %s", got, sqlStateUniqueViolation)
	}
}

func TestPaymentsSchema_SuccessTime(t *testing.T) {
	f := newStayFixture(t)
	if got := f.stay(t, "ticket", make([]byte, 32), nil, "entry_pending", nil, nil, nil, nil, nil, false); got != "" {
		t.Fatalf("stay: SQLSTATE %q", got)
	}
	var stayID int64
	if err := f.pool.QueryRow(t.Context(), "select id from stays").Scan(&stayID); err != nil {
		t.Fatal(err)
	}
	// M3 added payments_refunded_check and payments_failed_check, so the status-specific
	// timestamps are supplied here; the check under test is succeeded_at alone.
	const ins = `insert into payments (kind, stay_id, method, amount_cents, status, idempotency_key, succeeded_at,
		refunded_at, failed_at, failure_code)
		values ('parking_fee', $1, 'cash', 100, $2, $3, $4,
			case when $2 = 'refunded' then now() end, case when $2 = 'failed' then now() end,
			case when $2 = 'failed' then 'card_declined' end)`
	for _, status := range []string{"succeeded", "refunded"} {
		e := pgError(t, f.pool, ins, stayID, status, "k-"+status, nil)
		if e == nil || e.Code != sqlStateCheckViolation || e.ConstraintName != "payments_succeeded_at_check" {
			t.Errorf("%s without succeeded_at = %+v, want 23514 payments_succeeded_at_check", status, e)
		}
		if got := execCode(t, f.pool, ins, stayID, status, "ok-"+status, time.Now()); got != "" {
			t.Errorf("%s with succeeded_at: SQLSTATE %q, want success", status, got)
		}
	}
	for _, status := range []string{"pending", "failed"} {
		if got := execCode(t, f.pool, ins, stayID, status, "p-"+status, nil); got != "" {
			t.Errorf("%s without succeeded_at: SQLSTATE %q, want success", status, got)
		}
	}
}
