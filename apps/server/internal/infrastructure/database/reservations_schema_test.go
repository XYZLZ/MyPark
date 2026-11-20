package database_test

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/XYZLZ/MyPark/apps/server/internal/testsupport"
)

// m3Fixture is a lot with its seeded tariff and policy, and one driver with a
// vehicle. Users, vehicles and reservations are added per test.
type m3Fixture struct {
	pool                                 *pgxpool.Pool
	lotID, tariffID, policyID, vehicleID int64
	userID                               int64
	seq                                  int
}

func newM3Fixture(t *testing.T) *m3Fixture {
	t.Helper()
	db := testsupport.NewTestDB(t)
	f := &m3Fixture{pool: db.App, lotID: testsupport.SeedPilotLot(t, db)}
	ctx := t.Context()
	if err := f.pool.QueryRow(ctx, "select id from tariff_versions where lot_id = $1", f.lotID).Scan(&f.tariffID); err != nil {
		t.Fatalf("tariff id: %v", err)
	}
	if err := f.pool.QueryRow(ctx, "select id from policy_versions where lot_id = $1", f.lotID).Scan(&f.policyID); err != nil {
		t.Fatalf("policy id: %v", err)
	}
	f.userID, f.vehicleID = f.driver(t)
	return f
}

// driver inserts a user with one vehicle.
func (f *m3Fixture) driver(t *testing.T) (userID, vehicleID int64) {
	t.Helper()
	f.seq++
	ctx := t.Context()
	if err := f.pool.QueryRow(ctx, `insert into users (email, first_name, last_name)
		values ($1, 'Ana', 'P') returning id`, fmt.Sprintf("d%d@x.do", f.seq)).Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if err := f.pool.QueryRow(ctx, `insert into vehicles (user_id, plate_normalized, plate_display, make, model, color)
		values ($1, $2, $2, 'Kia', 'Rio', 'Rojo') returning id`, userID, fmt.Sprintf("A%d", 1000+f.seq)).Scan(&vehicleID); err != nil {
		t.Fatalf("insert vehicle: %v", err)
	}
	return userID, vehicleID
}

var (
	rsvArrival = time.Date(2027, 1, 10, 14, 0, 0, 0, time.UTC)
	rsvWindow  = "[2027-01-10T13:45:00Z,2027-01-10T14:20:00Z)"
)

// rsv holds the reservation columns the tests vary; zero values mean NULL
// except where newRsv sets a default.
type rsv struct {
	userID, vehicleID int64
	window            string
	status            string
	qrNonce           []byte
	confirmedAt       any
	cancelledAt       any
	cancelledBy       any
	refundReason      any
	checkedInAt       any
	expiredAt         any
}

func (f *m3Fixture) newRsv(status string) rsv {
	r := rsv{userID: f.userID, vehicleID: f.vehicleID, window: rsvWindow, status: status}
	at := rsvArrival.Add(-time.Hour)
	switch status {
	case "confirmed", "checked_in", "completed":
		r.qrNonce = make([]byte, 16)
		r.confirmedAt = at
	case "cancelled":
		r.cancelledAt, r.cancelledBy = at, "driver"
	case "expired_unpaid", "expired_no_show":
		r.expiredAt = at
	}
	if status == "checked_in" {
		r.checkedInAt = at
	}
	return r
}

const insertReservation = `insert into reservations (user_id, vehicle_id, lot_id, arrival_at, hold_window, status,
	fee_cents, tariff_version_id, policy_version_id, qr_nonce, payment_due_at, updated_at,
	confirmed_at, cancelled_at, cancelled_by, refund_reason, checked_in_at, expired_at)
	values ($1, $2, $3, $4, $5::tstzrange, $6, 15000, $7, $8, $9, $4, $4, $10, $11, $12, $13, $14, $15)
	returning id`

func (f *m3Fixture) rsvErr(t *testing.T, r rsv) (int64, string) {
	t.Helper()
	var nonce any
	if r.qrNonce != nil {
		nonce = r.qrNonce
	}
	var id int64
	err := f.pool.QueryRow(t.Context(), insertReservation, r.userID, r.vehicleID, f.lotID, rsvArrival, r.window,
		r.status, f.tariffID, f.policyID, nonce, r.confirmedAt, r.cancelledAt, r.cancelledBy, r.refundReason,
		r.checkedInAt, r.expiredAt).Scan(&id)
	return id, sqlStateOf(t, err)
}

func (f *m3Fixture) reservation(t *testing.T, r rsv) int64 {
	t.Helper()
	id, code := f.rsvErr(t, r)
	if code != "" {
		t.Fatalf("insert %s reservation: SQLSTATE %s", r.status, code)
	}
	return id
}

const insertFeePayment = `insert into payments (kind, method, amount_cents, status, idempotency_key, reservation_id,
	user_id, client_key, request_fingerprint, card_brand, card_last4, provider_ref, succeeded_at, failed_at,
	failure_code)
	values ('reservation_fee', 'gateway_sim', 15000, $1, $2, $3, $4, $5, $6, 'visa', '4242', $7, $8, $9, $10)`

// feePayment inserts a reservation_fee payment in the given status. key makes
// the idempotency and client keys unique.
func (f *m3Fixture) feePaymentErr(t *testing.T, reservationID int64, status, key string) string {
	t.Helper()
	var providerRef, succeededAt, failedAt, failureCode any
	switch status {
	case "succeeded", "refunded":
		providerRef, succeededAt = "sim_ch_"+key, time.Now()
	case "failed":
		failedAt, failureCode = time.Now(), "card_declined"
	}
	_, err := f.pool.Exec(t.Context(), insertFeePayment, status, "fee:"+key, reservationID, f.userID,
		"client-key-"+key, make([]byte, 32), providerRef, succeededAt, failedAt, failureCode)
	return sqlStateOf(t, err)
}

func (f *m3Fixture) feePayment(t *testing.T, reservationID int64, status, key string) int64 {
	t.Helper()
	if code := f.feePaymentErr(t, reservationID, status, key); code != "" {
		t.Fatalf("insert %s fee payment: SQLSTATE %s", status, code)
	}
	var id int64
	if err := f.pool.QueryRow(t.Context(), "select id from payments where idempotency_key = $1", "fee:"+key).Scan(&id); err != nil {
		t.Fatalf("payment id: %v", err)
	}
	return id
}

func TestSchema_OneLiveFeePayment(t *testing.T) {
	f := newM3Fixture(t)
	rid := f.reservation(t, f.newRsv("pending_payment"))
	f.feePayment(t, rid, "pending", "a1111111")

	e := pgError(t, f.pool, insertFeePayment, "pending", "fee:a2222222", rid, f.userID, "client-key-a2222222",
		make([]byte, 32), nil, nil, nil, nil)
	if e == nil || e.Code != sqlStateUniqueViolation || e.ConstraintName != "payments_reservation_live_key" {
		t.Errorf("second pending fee payment = %+v, want 23505 payments_reservation_live_key", e)
	}

	// A failed attempt does not count as live.
	if code := f.feePaymentErr(t, rid, "failed", "a3333333"); code != "" {
		t.Errorf("failed fee payment beside a pending one: SQLSTATE %q, want success", code)
	}
	// The same client key twice for one reservation is the idempotency scope.
	e = pgError(t, f.pool, insertFeePayment, "failed", "fee:other", rid, f.userID, "client-key-a3333333",
		make([]byte, 32), nil, nil, time.Now(), "card_declined")
	if e == nil || e.Code != sqlStateUniqueViolation || e.ConstraintName != "payments_reservation_client_key" {
		t.Errorf("repeated client key = %+v, want 23505 payments_reservation_client_key", e)
	}
}

const insertRefund = `insert into refunds (payment_id, amount_cents, reason, status, idempotency_key, next_attempt_at,
	provider_ref, succeeded_at, failed_at)
	values ($1, 15000, 'cancelled_before_arrival', $2, $3, now(), $4, $5, $6)`

func TestSchema_OneLiveRefund(t *testing.T) {
	f := newM3Fixture(t)
	rid := f.reservation(t, f.newRsv("cancelled"))
	pid := f.feePayment(t, rid, "succeeded", "b1111111")

	if code := execCode(t, f.pool, insertRefund, pid, "failed", "refund:1", nil, nil, time.Now()); code != "" {
		t.Fatalf("failed refund: SQLSTATE %q", code)
	}
	if code := execCode(t, f.pool, insertRefund, pid, "pending", "refund:2", nil, nil, nil); code != "" {
		t.Fatalf("pending refund after a failed one: SQLSTATE %q, want success", code)
	}
	e := pgError(t, f.pool, insertRefund, pid, "pending", "refund:3", nil, nil, nil)
	if e == nil || e.Code != sqlStateUniqueViolation || e.ConstraintName != "refunds_payment_live_key" {
		t.Errorf("second pending refund = %+v, want 23505 refunds_payment_live_key", e)
	}
	if code := execCode(t, f.pool, insertRefund, pid, "succeeded", "refund:4", "sim_rf_x", time.Now(), nil); code != sqlStateUniqueViolation {
		t.Errorf("succeeded refund beside a pending one: SQLSTATE %q, want %s", code, sqlStateUniqueViolation)
	}
	if code := execCode(t, f.pool, insertRefund, pid, "failed", "refund:5", nil, nil, nil); code != sqlStateCheckViolation {
		t.Errorf("failed refund without failed_at: SQLSTATE %q, want %s", code, sqlStateCheckViolation)
	}
}

func TestSchema_OneActiveReservationPerUser(t *testing.T) {
	f := newM3Fixture(t)
	f.reservation(t, f.newRsv("pending_payment"))

	_, code := f.rsvErr(t, f.newRsv("pending_payment"))
	if code != sqlStateUniqueViolation {
		t.Errorf("second pending_payment: SQLSTATE %q, want %s", code, sqlStateUniqueViolation)
	}
	_, code = f.rsvErr(t, f.newRsv("confirmed"))
	if code != sqlStateUniqueViolation {
		t.Errorf("confirmed beside pending_payment: SQLSTATE %q, want %s", code, sqlStateUniqueViolation)
	}
	// checked_in is no longer "active" for FR-RES-3.
	if _, code = f.rsvErr(t, f.newRsv("checked_in")); code != "" {
		t.Errorf("checked_in beside pending_payment: SQLSTATE %q, want success", code)
	}
	// Another driver is unaffected.
	other, otherVehicle := f.driver(t)
	r := f.newRsv("pending_payment")
	r.userID, r.vehicleID = other, otherVehicle
	if _, code = f.rsvErr(t, r); code != "" {
		t.Errorf("other driver's pending_payment: SQLSTATE %q, want success", code)
	}
}

func TestSchema_ReservationChecks(t *testing.T) {
	f := newM3Fixture(t)
	tests := []struct {
		name string
		mod  func(r *rsv)
	}{
		{"window not half-open", func(r *rsv) { r.window = "[2027-01-10T13:45:00Z,2027-01-10T14:20:00Z]" }},
		{"arrival outside the window", func(r *rsv) { r.window = "[2027-01-10T14:05:00Z,2027-01-10T14:20:00Z)" }},
		{"confirmed without qr_nonce", func(r *rsv) { r.qrNonce = nil }},
		{"cancelled without cancelled_by", func(r *rsv) { r.cancelledBy = nil }},
		{"refund_reason on confirmed", func(r *rsv) { r.refundReason = "none_after_arrival" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status := "confirmed"
			if tt.name == "cancelled without cancelled_by" {
				status = "cancelled"
			}
			r := f.newRsv(status)
			tt.mod(&r)
			if _, code := f.rsvErr(t, r); code != sqlStateCheckViolation {
				t.Errorf("SQLSTATE %q, want %s", code, sqlStateCheckViolation)
			}
		})
	}

	t.Run("slot_start not 5-minute aligned", func(t *testing.T) {
		const ins = `insert into reservation_slot_loads (lot_id, slot_start, held) values ($1, $2, 0)`
		if code := execCode(t, f.pool, ins, f.lotID, rsvArrival.Add(time.Minute)); code != sqlStateCheckViolation {
			t.Errorf("misaligned slot: SQLSTATE %q, want %s", code, sqlStateCheckViolation)
		}
		if code := execCode(t, f.pool, ins, f.lotID, rsvArrival); code != "" {
			t.Errorf("aligned slot: SQLSTATE %q, want success", code)
		}
		if code := execCode(t, f.pool, "update reservation_slot_loads set held = -1"); code != sqlStateCheckViolation {
			t.Errorf("negative held: SQLSTATE %q, want %s", code, sqlStateCheckViolation)
		}
	})
}

func TestSchema_StayReservationLink(t *testing.T) {
	f := newM3Fixture(t)
	rid := f.reservation(t, f.newRsv("confirmed"))
	const ins = `insert into stays (lot_id, credential_kind, ticket_code_hash, status, tariff_version_id, reservation_id)
		values ($1, $2, $3, 'entry_pending', $4, $5)`

	if code := execCode(t, f.pool, ins, f.lotID, "reservation", nil, f.tariffID, nil); code != sqlStateCheckViolation {
		t.Errorf("reservation stay without reservation_id: SQLSTATE %q, want %s", code, sqlStateCheckViolation)
	}
	if code := execCode(t, f.pool, ins, f.lotID, "ticket", make([]byte, 32), f.tariffID, rid); code != sqlStateCheckViolation {
		t.Errorf("ticket stay with reservation_id: SQLSTATE %q, want %s", code, sqlStateCheckViolation)
	}
	if code := execCode(t, f.pool, ins, f.lotID, "reservation", nil, f.tariffID, rid); code != "" {
		t.Fatalf("valid reservation stay: SQLSTATE %q", code)
	}
	e := pgError(t, f.pool, ins, f.lotID, "reservation", nil, f.tariffID, rid)
	if e == nil || e.Code != sqlStateUniqueViolation || e.ConstraintName != "stays_reservation_key" {
		t.Errorf("second stay for one reservation = %+v, want 23505 stays_reservation_key", e)
	}
}

func TestSchema_M2RowsStillValid(t *testing.T) {
	f := newM3Fixture(t)
	var stayID int64
	if err := f.pool.QueryRow(t.Context(), `insert into stays (lot_id, credential_kind, ticket_code_hash, status,
		tariff_version_id, entered_at) values ($1, 'ticket', $2, 'active', $3, now()) returning id`,
		f.lotID, make([]byte, 32), f.tariffID).Scan(&stayID); err != nil {
		t.Fatalf("insert stay: %v", err)
	}
	const cash = `insert into payments (kind, stay_id, method, amount_cents, status, idempotency_key, succeeded_at)
		values ('parking_fee', $1, 'cash', 5000, 'succeeded', 'cash:dev:1', now())`
	if code := execCode(t, f.pool, cash, stayID); code != "" {
		t.Errorf("M2 cash parking_fee: SQLSTATE %q, want success", code)
	}

	rid := f.reservation(t, f.newRsv("pending_payment"))
	e := pgError(t, f.pool, `insert into payments (kind, reservation_id, method, amount_cents, status, idempotency_key,
		succeeded_at) values ('parking_fee', $1, 'cash', 5000, 'succeeded', 'cash:dev:2', now())`, rid)
	if e == nil || e.Code != sqlStateCheckViolation || e.ConstraintName != "payments_kind_target_check" {
		t.Errorf("parking_fee with reservation_id = %+v, want 23514 payments_kind_target_check", e)
	}
}

func TestSchema_OutboxTemplates(t *testing.T) {
	f := newM3Fixture(t)
	const ins = `insert into outbox_messages (template, locale, to_address, payload, dedupe_key)
		values ($1, 'es-DO', 'ana@x.do', '{}', $1)`
	for _, name := range []string{"verify_email", "reservation_confirmed", "reservation_reminder",
		"reservation_cancelled", "reservation_no_show", "refund_succeeded"} {
		if code := execCode(t, f.pool, ins, name); code != "" {
			t.Errorf("template %s: SQLSTATE %q, want success", name, code)
		}
	}
	if code := execCode(t, f.pool, ins, "nope"); code != sqlStateCheckViolation {
		t.Errorf("template nope: SQLSTATE %q, want %s", code, sqlStateCheckViolation)
	}
}

// sqlStateOf returns the SQLSTATE of err, or "" when err is nil.
func sqlStateOf(t *testing.T, err error) string {
	t.Helper()
	if err == nil {
		return ""
	}
	pgErr, ok := errors.AsType[*pgconn.PgError](err)
	if !ok {
		t.Fatalf("non-postgres error: %v", err)
	}
	return pgErr.Code
}
