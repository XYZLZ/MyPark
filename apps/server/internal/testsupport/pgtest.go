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

// newPoolFunc builds pools; tests replace it to inject failures.
var newPoolFunc = database.NewPool

// Roles the test databases are used through, exactly as in production.
const (
	roleMigrator = "mypark_migrator"
	roleApp      = "mypark_app"
)

// TestDB is a fresh database migrated and used like production: the schema
// is built by mypark_migrator and the application connects as mypark_app.
type TestDB struct {
	// App runs as mypark_app (DML only). Use it for everything the
	// application does.
	App *pgxpool.Pool
	// Migrator runs as mypark_migrator (DDL). Use it to re-run migrations or
	// to create scratch tables; mypark_app has no TEMPORARY privilege.
	Migrator *pgxpool.Pool
	// Super is the superuser pool of TEST_DATABASE_URL, bound to this database.
	Super *pgxpool.Pool
	// AppURL is a connection URL that logs in as the superuser and switches
	// to mypark_app through the role runtime parameter.
	AppURL string
}

// NewTestDB returns a fresh database owned by mypark_migrator with every
// migration applied by mypark_migrator. The database is dropped when the
// test ends. See NewEmptyDB for when the test is skipped or fails.
func NewTestDB(t *testing.T) *TestDB {
	t.Helper()
	super, superURL := newEmptyDB(t)
	migrator := newRolePool(t, superURL, roleMigrator)
	if err := database.MigrateUp(t.Context(), stdlib.OpenDBFromPool(migrator)); err != nil {
		t.Fatalf("testsupport: migrate: %v", err)
	}
	appURL, err := withRole(superURL, roleApp)
	if err != nil {
		t.Fatalf("testsupport: %v", err)
	}
	return &TestDB{
		App:      newRolePool(t, superURL, roleApp),
		Migrator: migrator,
		Super:    super,
		AppURL:   appURL,
	}
}

// NewDB returns the mypark_app pool of a fresh, migrated database.
func NewDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	return NewTestDB(t).App
}

// NewDBURL is NewDB for tests that open their own connections (for example
// through cmd/api's run): it returns the connection URL of a fresh, migrated
// database, which logs in as mypark_app.
func NewDBURL(t *testing.T) string {
	t.Helper()
	return NewTestDB(t).AppURL
}

// NewEmptyDB returns a superuser pool on a fresh database named
// mypark_test_<random> on the TEST_DATABASE_URL server, owned by
// mypark_migrator, with the MyPark roles ensured but no migration applied.
// The database is dropped when the test ends.
//
// When TEST_DATABASE_URL is unset the test is skipped, unless CI=true, where
// a missing database is a configuration error and the test fails.
func NewEmptyDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool, _ := newEmptyDB(t)
	return pool
}

func newEmptyDB(t testing.TB) (*pgxpool.Pool, string) {
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
	if _, err := admin.Exec(ctx, "create database "+pgx.Identifier{name}.Sanitize()+" owner "+roleMigrator); err != nil {
		t.Fatalf("testsupport: create database %s: %v", name, err)
	}
	// Registered right after create database, before anything that can fail,
	// so a later error never leaks the database. Cleanups run LIFO: pools
	// registered later close first, then the drop runs. They use their own
	// context because t.Context() is canceled by then.
	t.Cleanup(func() { dropDatabase(t, adminURL, name) })

	dbURL, err := withDatabase(adminURL, name)
	if err != nil {
		t.Fatalf("testsupport: %v", err)
	}
	pool, err := newPoolFunc(ctx, dbURL, testMaxConns)
	if err != nil {
		t.Fatalf("testsupport: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool, dbURL
}

// newRolePool returns a pool whose connections run as role, set through the
// role runtime parameter (a startup SET ROLE). The pool closes when the test
// ends, before the database is dropped.
func newRolePool(t testing.TB, dbURL, role string) *pgxpool.Pool {
	t.Helper()
	u, err := withRole(dbURL, role)
	if err != nil {
		t.Fatalf("testsupport: %v", err)
	}
	pool, err := newPoolFunc(t.Context(), u, testMaxConns)
	if err != nil {
		t.Fatalf("testsupport: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func dropDatabase(t testing.TB, adminURL, name string) {
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

// withRole returns rawURL with the role runtime parameter set.
func withRole(rawURL, role string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", errInvalidURL
	}
	q := u.Query()
	q.Set("role", role)
	u.RawQuery = q.Encode()
	return u.String(), nil
}

var errInvalidURL = errors.New(EnvTestDatabaseURL + " is not a valid URL")
