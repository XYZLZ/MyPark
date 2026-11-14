package database_test

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/XYZLZ/MyPark/apps/server/internal/features/devices"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/tariffs"
	"github.com/XYZLZ/MyPark/apps/server/internal/testsupport"
)

// TestGrants_M2Tables has one subtest per M2 migration.
func TestGrants_M2Tables(t *testing.T) {
	t.Run("devices", grantsDevices)

	t.Run("tariffs", func(t *testing.T) {
		db := testsupport.NewTestDB(t)
		lotID := testsupport.SeedPilotLot(t, db)
		const insertPolicy = `insert into policy_versions (lot_id, effective_from, max_reservable_pct,
			booking_horizon_days, arrival_grace_min, early_arrival_min, payment_timeout_min, max_active_per_user,
			slot_min, noshow_block_threshold, noshow_window_days, noshow_block_days)
			values ($1, '2027-01-01T00:00:00-04:00', 30, 7, 20, 15, 10, 1, 5, 3, 30, 7)`
		const insertTariff = `insert into tariff_versions (lot_id, effective_from, first_hour_cents,
			additional_hour_cents, exit_grace_min, post_payment_exit_min, daily_cap_cents, lost_ticket_cents,
			reservation_fee_cents) values ($1, '2027-01-01T00:00:00-04:00', 5000, 2500, 10, 15, 40000, 50000, 15000)`

		for table, insert := range map[string]string{"tariff_versions": insertTariff, "policy_versions": insertPolicy} {
			if got := execCode(t, db.App, "select count(*) from "+table); got != "" {
				t.Errorf("%s: select as mypark_app: SQLSTATE %q, want success", table, got)
			}
			if got := execCode(t, db.App, insert, lotID); got != "" {
				t.Errorf("%s: insert as mypark_app: SQLSTATE %q, want success", table, got)
			}
			for _, stmt := range []string{
				"update " + table + " set created_at = now()",
				"delete from " + table,
				"truncate " + table,
			} {
				if got := execCode(t, db.App, stmt); got != sqlStateInsufficientPrivilege {
					t.Errorf("%q as mypark_app: SQLSTATE %q, want %s", stmt, got, sqlStateInsufficientPrivilege)
				}
			}
		}
	})

	t.Run("tariffs checks and uniqueness", func(t *testing.T) {
		db := testsupport.NewTestDB(t)
		lotID := testsupport.SeedPilotLot(t, db)
		def := tariffs.DefaultTariff()
		const q = `insert into tariff_versions (lot_id, effective_from, first_hour_cents, additional_hour_cents,
			exit_grace_min, post_payment_exit_min, daily_cap_cents, lost_ticket_cents, reservation_fee_cents)
			values ($1, $2, $3, 2500, 10, 15, $4, 50000, 15000)`
		if got := execCode(t, db.App, q, lotID, def.EffectiveFrom, 5000, 40000); got != sqlStateUniqueViolation {
			t.Errorf("duplicate (lot_id, effective_from): SQLSTATE %q, want %s", got, sqlStateUniqueViolation)
		}
		later := def.EffectiveFrom.AddDate(0, 1, 0)
		if got := execCode(t, db.App, q, lotID, later, 5000, 4999); got != sqlStateCheckViolation {
			t.Errorf("daily cap below first hour: SQLSTATE %q, want %s", got, sqlStateCheckViolation)
		}
		if got := execCode(t, db.App, q, lotID, later, -1, 40000); got != sqlStateCheckViolation {
			t.Errorf("negative first hour: SQLSTATE %q, want %s", got, sqlStateCheckViolation)
		}
	})
}

const (
	insertDevice = `insert into devices (lot_id, kind, name, secret_ciphertext, secret_key_id)
		values ($1, $2, $3, $4, 'k1') returning id`
	insertDeviceEvent = `insert into device_events (lot_id, device_id, event_id, type, occurred_at, received_at,
		body_sha256, payload, response) values ($1, $2, $3, 'heartbeat', now(), $4, $5, '{}', '{}')`
)

func pgError(t *testing.T, pool *pgxpool.Pool, sql string, args ...any) *pgconn.PgError {
	t.Helper()
	_, err := pool.Exec(t.Context(), sql, args...)
	if err == nil {
		return nil
	}
	pgErr, ok := errors.AsType[*pgconn.PgError](err)
	if !ok {
		t.Fatalf("exec %q: non-postgres error: %v", sql, err)
	}
	return pgErr
}

func newDevice(t *testing.T, pool *pgxpool.Pool, lotID int64, kind, name string) int64 {
	t.Helper()
	var id int64
	if err := pool.QueryRow(t.Context(), insertDevice, lotID, kind, name, make([]byte, 48)).Scan(&id); err != nil {
		t.Fatalf("insert device %s: %v", name, err)
	}
	return id
}

func grantsDevices(t *testing.T) {
	db := testsupport.NewTestDB(t)
	lotID := testsupport.SeedPilotLot(t, db)
	deviceID := newDevice(t, db.App, lotID, "podium", "podium-test")
	if got := execCode(t, db.App, "select count(*) from devices"); got != "" {
		t.Errorf("select devices: SQLSTATE %q", got)
	}
	if got := execCode(t, db.App, "update devices set last_seen_at = now() where id = $1", deviceID); got != "" {
		t.Errorf("update devices: SQLSTATE %q, want success", got)
	}
	now := time.Now()
	hash := make([]byte, 32)
	if got := execCode(t, db.App, insertDeviceEvent, lotID, deviceID, uuid.New(), now, hash); got != "" {
		t.Errorf("insert device_events as mypark_app: SQLSTATE %q, want success", got)
	}
	for _, stmt := range []string{
		"update device_events set payload = '{}'",
		"delete from device_events",
		"truncate device_events",
	} {
		if got := execCode(t, db.App, stmt); got != sqlStateInsufficientPrivilege {
			t.Errorf("%q as mypark_app: SQLSTATE %q, want %s", stmt, got, sqlStateInsufficientPrivilege)
		}
	}
	// Retention: only the security definer function may delete, and only
	// rows older than 12 months.
	old, recent := uuid.New(), uuid.New()
	for id, at := range map[uuid.UUID]time.Time{old: now.AddDate(0, -13, 0), recent: now.AddDate(0, -11, 0)} {
		if got := execCode(t, db.Migrator, insertDeviceEvent, lotID, deviceID, id, at, hash); got != "" {
			t.Fatalf("migrator back-dated insert: SQLSTATE %q", got)
		}
	}
	n, err := devices.PurgeEvents(t.Context(), db.App, 100)
	if err != nil || n != 1 {
		t.Fatalf("PurgeEvents = %d, %v, want 1", n, err)
	}
	var left int
	if err := db.App.QueryRow(t.Context(), "select count(*) from device_events where event_id = any($1)", []uuid.UUID{old, recent}).Scan(&left); err != nil || left != 1 {
		t.Errorf("rows left among the two back-dated = %d, %v, want only the 11-month one", left, err)
	}
	var ok bool
	if err := db.App.QueryRow(t.Context(), "select exists(select 1 from device_events where event_id = $1)", recent).Scan(&ok); err != nil || !ok {
		t.Errorf("11-month row missing after purge: %v, %v", ok, err)
	}
	// A batch of zero is clamped to 1 inside the function: with two expired
	// rows it deletes exactly one, and it never touches the 11-month row.
	for _, id := range []uuid.UUID{uuid.New(), uuid.New()} {
		if got := execCode(t, db.Migrator, insertDeviceEvent, lotID, deviceID, id, now.AddDate(0, -13, 0), hash); got != "" {
			t.Fatalf("migrator back-dated insert: SQLSTATE %q", got)
		}
	}
	if n, err := devices.PurgeEvents(t.Context(), db.App, 0); err != nil || n != 1 {
		t.Errorf("PurgeEvents(batch 0) with two expired rows = %d, %v, want 1", n, err)
	}
	if n, err := devices.PurgeEvents(t.Context(), db.App, 100); err != nil || n != 1 {
		t.Errorf("PurgeEvents(100) for the remaining expired row = %d, %v, want 1", n, err)
	}

	// The function's security properties, from the catalog.
	var secDef bool
	var config []string
	var owner string
	err = db.App.QueryRow(t.Context(), `select prosecdef, coalesce(proconfig, '{}'), pg_get_userbyid(proowner)
		from pg_proc where proname = 'purge_device_events'`).Scan(&secDef, &config, &owner)
	if err != nil {
		t.Fatalf("pg_proc: %v", err)
	}
	if !secDef || owner != "mypark_migrator" || !slices.Contains(config, `search_path=""`) {
		t.Errorf("purge_device_events: security definer=%v owner=%q config=%v, want true, mypark_migrator, search_path=\"\"", secDef, owner, config)
	}
}

func TestDevicesSchema_Checks(t *testing.T) {
	db := testsupport.NewTestDB(t)
	lotID := testsupport.SeedPilotLot(t, db)
	deviceID := newDevice(t, db.App, lotID, "podium", "podium-test")
	const insertDev = `insert into devices (lot_id, kind, name, secret_ciphertext, secret_key_id, status, revoked_at)
		values ($1, $2, $3, $4, 'k1', $5, $6)`
	ct := make([]byte, 48)
	if got := execCode(t, db.App, insertDev, lotID, "drone", "d1", ct, "active", nil); got != sqlStateCheckViolation {
		t.Errorf("kind drone: SQLSTATE %q, want %s", got, sqlStateCheckViolation)
	}
	if got := execCode(t, db.App, insertDev, lotID, "podium", "d2", ct, "revoked", nil); got != sqlStateCheckViolation {
		t.Errorf("revoked without revoked_at: SQLSTATE %q, want %s", got, sqlStateCheckViolation)
	}
	if got := execCode(t, db.App, insertDev, lotID, "podium", "d3", ct, "active", time.Now()); got != sqlStateCheckViolation {
		t.Errorf("active with revoked_at: SQLSTATE %q, want %s", got, sqlStateCheckViolation)
	}
	if got := execCode(t, db.App, insertDev, lotID, "podium", "podium-test", ct, "active", nil); got != sqlStateUniqueViolation {
		t.Errorf("duplicate (lot_id, name): SQLSTATE %q, want %s", got, sqlStateUniqueViolation)
	}
	if got := execCode(t, db.App, insertDev, lotID, "podium", "d4", ct, "revoked", time.Now()); got != "" {
		t.Errorf("valid revoked device: SQLSTATE %q, want success", got)
	}
	if got := execCode(t, db.App, insertDeviceEvent, lotID, deviceID, uuid.New(), time.Now(), make([]byte, 31)); got != sqlStateCheckViolation {
		t.Errorf("body_sha256 of 31 bytes: SQLSTATE %q, want %s", got, sqlStateCheckViolation)
	}
	if got := execCode(t, db.App, `insert into device_events (lot_id, device_id, event_id, type, occurred_at, body_sha256, payload, response)
		values ($1, $2, $3, 'reboot', now(), $4, '{}', '{}')`, lotID, deviceID, uuid.New(), make([]byte, 32)); got != sqlStateCheckViolation {
		t.Errorf("unknown event type: SQLSTATE %q, want %s", got, sqlStateCheckViolation)
	}
	eventID := uuid.New()
	if got := execCode(t, db.App, insertDeviceEvent, lotID, deviceID, eventID, time.Now(), make([]byte, 32)); got != "" {
		t.Fatalf("first event insert: SQLSTATE %q", got)
	}
	e := pgError(t, db.App, insertDeviceEvent, lotID, deviceID, eventID, time.Now(), make([]byte, 32))
	if e == nil || e.Code != sqlStateUniqueViolation || e.ConstraintName != "device_events_device_id_event_id_key" {
		t.Errorf("duplicate (device_id, event_id) = %+v, want 23505 device_events_device_id_event_id_key", e)
	}
}
