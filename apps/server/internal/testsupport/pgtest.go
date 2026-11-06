// Package testsupport provides helpers for integration tests that need a
// real Postgres.
package testsupport

import (
	"context"
	"crypto/rand"
	"errors"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"

	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/database"
)

// EnvTestDatabaseURL names the superuser connection URL of the Postgres
// server used by integration tests. Each test gets its own database on it.
const EnvTestDatabaseURL = "TEST_DATABASE_URL"

const (
	testMaxConns = 4
	// adminTimeout bounds the create/drop database round trips.
	adminTimeout = 30 * time.Second
)

// ensureRolesSQL creates the roles the baseline migration grants to, when
// absent. Production creates them with LOGIN and passwords in
// deploy/compose/postgres/init/01-roles.sh; tests only need them to exist,
// and use them through SET ROLE. Roles are cluster-wide and test packages
// run in parallel, so a concurrent create is tolerated.
const ensureRolesSQL = `
do $$
declare
	r text;
begin
	foreach r in array array['mypark_migrator', 'mypark_app'] loop
		if not exists (select from pg_roles where rolname = r) then
			begin
				execute format('create role %I nologin', r);
			exception when duplicate_object or unique_violation then
				null;
			end;
		end if;
	end loop;
end $$`

// NewDB returns a pool on a fresh database with every migration applied.
// The database is dropped when the test ends. See NewEmptyDB for when the
// test is skipped or fails.
func NewDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool := NewEmptyDB(t)
	if err := database.MigrateUp(t.Context(), stdlib.OpenDBFromPool(pool)); err != nil {
		t.Fatalf("testsupport: migrate: %v", err)
	}
	return pool
}

// NewDBURL is NewDB for tests that open their own connections (for example
// through cmd/api's run): it returns the connection URL of a fresh, migrated
// database instead of a pool. The database is dropped when the test ends.
func NewDBURL(t *testing.T) string {
	t.Helper()
	return NewDB(t).Config().ConnString()
}

// NewEmptyDB returns a pool on a fresh database named mypark_test_<random>
// on the TEST_DATABASE_URL server, with the MyPark roles ensured but no
// migration applied. The database is dropped when the test ends.
//
// When TEST_DATABASE_URL is unset the test is skipped, unless CI=true, where
// a missing database is a configuration error and the test fails.
func NewEmptyDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	adminURL := os.Getenv(EnvTestDatabaseURL)
	if adminURL == "" {
		if os.Getenv("CI") == "true" {
			t.Fatalf("testsupport: %s is required when CI=true", EnvTestDatabaseURL)
		}
		t.Skipf("testsupport: %s is not set; skipping integration test", EnvTestDatabaseURL)
	}

	name := "mypark_test_" + strings.ToLower(rand.Text())
	ctx, cancel := context.WithTimeout(t.Context(), adminTimeout)
	defer cancel()
	admin, err := pgx.Connect(ctx, adminURL)
	if err != nil {
		t.Fatalf("testsupport: connect to %s: %v", EnvTestDatabaseURL, err)
	}
	defer admin.Close(context.Background())
	if _, err := admin.Exec(ctx, ensureRolesSQL); err != nil {
		t.Fatalf("testsupport: ensure roles: %v", err)
	}
	if _, err := admin.Exec(ctx, "create database "+pgx.Identifier{name}.Sanitize()); err != nil {
		t.Fatalf("testsupport: create database %s: %v", name, err)
	}

	dbURL, err := withDatabase(adminURL, name)
	if err != nil {
		t.Fatalf("testsupport: %v", err)
	}
	pool, err := database.NewPool(ctx, dbURL, testMaxConns)
	if err != nil {
		t.Fatalf("testsupport: %v", err)
	}
	// Cleanups run after t.Context() is canceled, so they use their own.
	t.Cleanup(func() {
		pool.Close()
		dropDatabase(t, adminURL, name)
	})
	return pool
}

func dropDatabase(t *testing.T, adminURL, name string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), adminTimeout)
	defer cancel()
	admin, err := pgx.Connect(ctx, adminURL)
	if err != nil {
		t.Errorf("testsupport: connect to drop %s: %v", name, err)
		return
	}
	defer admin.Close(ctx)
	if _, err := admin.Exec(ctx, "drop database if exists "+pgx.Identifier{name}.Sanitize()+" with (force)"); err != nil {
		t.Errorf("testsupport: drop database %s: %v", name, err)
	}
}

// withDatabase returns rawURL with its database (the URL path) replaced.
func withDatabase(rawURL, name string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		// Do not echo the URL: it carries the password.
		return "", errInvalidURL
	}
	u.Path = "/" + name
	return u.String(), nil
}

var errInvalidURL = errors.New(EnvTestDatabaseURL + " is not a valid URL")
