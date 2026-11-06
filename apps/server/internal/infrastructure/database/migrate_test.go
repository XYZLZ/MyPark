package database_test

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/stdlib"

	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/database"
	"github.com/XYZLZ/MyPark/apps/server/internal/testsupport"
)

// baselineVersion is the version of 00001_baseline.sql, the highest embedded
// migration in M0.
const baselineVersion = 1

func TestMigrateUp_CreatesBaseline(t *testing.T) {
	pool := testsupport.NewDB(t)
	for _, table := range []string{"public.lots", "public.spaces", "public.lot_occupancy"} {
		var exists bool
		err := pool.QueryRow(t.Context(), "select to_regclass($1) is not null", table).Scan(&exists)
		if err != nil {
			t.Fatalf("to_regclass(%s): %v", table, err)
		}
		if !exists {
			t.Errorf("table %s does not exist after MigrateUp", table)
		}
	}
}

func TestMigrateUp_Idempotent(t *testing.T) {
	pool := testsupport.NewDB(t)
	// NewDB already migrated; a second run must be a no-op.
	if err := database.MigrateUp(t.Context(), stdlib.OpenDBFromPool(pool)); err != nil {
		t.Fatalf("second MigrateUp: %v", err)
	}
}

func TestMigrationStatus_UpToDate(t *testing.T) {
	pool := testsupport.NewDB(t)
	current, expected, err := database.NewMigrationStatus(stdlib.OpenDBFromPool(pool)).Status(t.Context())
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if current != baselineVersion || expected != baselineVersion {
		t.Errorf("Status = (%d, %d), want (%d, %d)", current, expected, baselineVersion, baselineVersion)
	}
}

// TestReadyz_MigrationsBehind_Integration covers review focus 2: the database
// answers but no migration has run, so /readyz must see current < expected.
func TestReadyz_MigrationsBehind_Integration(t *testing.T) {
	pool := testsupport.NewEmptyDB(t)
	current, expected, err := database.NewMigrationStatus(stdlib.OpenDBFromPool(pool)).Status(t.Context())
	if err != nil {
		t.Fatalf("Status on an unmigrated database: %v", err)
	}
	if current != 0 || expected != baselineVersion {
		t.Errorf("Status = (%d, %d), want (0, %d)", current, expected, baselineVersion)
	}
	// Status is a read-only probe: it must not create the goose version table.
	var created bool
	if err := pool.QueryRow(t.Context(), "select to_regclass('public.goose_db_version') is not null").Scan(&created); err != nil {
		t.Fatalf("to_regclass: %v", err)
	}
	if created {
		t.Error("Status created goose_db_version; it must not run DDL")
	}
}

// TestMigrationStatus_AsAppRole checks the privilege boundary: the app role
// (DML only) must be able to read the migration status that /readyz reports.
func TestMigrationStatus_AsAppRole(t *testing.T) {
	pool := testsupport.NewDB(t)
	conn, err := pool.Acquire(t.Context())
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	defer conn.Release()
	if _, err := conn.Exec(t.Context(), "set role mypark_app"); err != nil {
		t.Fatalf("set role: %v", err)
	}
	defer conn.Exec(t.Context(), "reset role") //nolint:errcheck // best effort before release

	var current int64
	err = conn.QueryRow(t.Context(), "select coalesce(max(version_id), 0) from goose_db_version where is_applied").Scan(&current)
	if err != nil {
		t.Fatalf("read goose_db_version as mypark_app: %v", err)
	}
	if current != baselineVersion {
		t.Errorf("current = %d, want %d", current, baselineVersion)
	}
	// And it must not be able to run DDL.
	_, err = conn.Exec(t.Context(), "create table app_should_not_create (id int)")
	if pgErr, ok := errors.AsType[*pgconn.PgError](err); !ok || pgErr.Code != "42501" {
		t.Errorf("create table as mypark_app: err = %v, want SQLSTATE 42501 insufficient_privilege", err)
	}
}

func TestSpaces_StatusCheckRejectsUnknown(t *testing.T) {
	pool := testsupport.NewDB(t)
	ctx := t.Context()
	var lotID int64
	err := pool.QueryRow(ctx,
		`insert into lots (slug, name, address, capacity) values ('t', 'T', 'A', 1) returning id`).Scan(&lotID)
	if err != nil {
		t.Fatalf("insert lot: %v", err)
	}
	if _, err := pool.Exec(ctx, `insert into spaces (lot_id, code) values ($1, 'E001')`, lotID); err != nil {
		t.Fatalf("insert space: %v", err)
	}

	_, err = pool.Exec(ctx, `update spaces set status = 'parked' where lot_id = $1`, lotID)
	pgErr, ok := errors.AsType[*pgconn.PgError](err)
	if !ok || pgErr.Code != "23514" {
		t.Fatalf("update status='parked': err = %v, want SQLSTATE 23514 check_violation", err)
	}
}
