package testsupport

import (
	"context"
	"errors"
	"net/url"
	"runtime"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestNewDB_ConnectsAsApp(t *testing.T) {
	pool := NewDB(t)
	var user string
	if err := pool.QueryRow(t.Context(), "select current_user").Scan(&user); err != nil {
		t.Fatalf("select current_user: %v", err)
	}
	if user != "mypark_app" {
		t.Errorf("current_user = %q, want mypark_app", user)
	}
}

func TestNewDBURL_ConnectsAsApp(t *testing.T) {
	conn, err := pgx.Connect(t.Context(), NewDBURL(t))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(t.Context())
	var user string
	if err := conn.QueryRow(t.Context(), "select current_user").Scan(&user); err != nil {
		t.Fatalf("select current_user: %v", err)
	}
	if user != "mypark_app" {
		t.Errorf("current_user = %q, want mypark_app", user)
	}
}

func TestNewTestDB_MigratedByMigrator(t *testing.T) {
	db := NewTestDB(t)
	var owner string
	err := db.Super.QueryRow(t.Context(), "select tableowner from pg_tables where tablename = 'lots'").Scan(&owner)
	if err != nil {
		t.Fatalf("select tableowner: %v", err)
	}
	if owner != "mypark_migrator" {
		t.Errorf("lots owner = %q, want mypark_migrator", owner)
	}
	var user string
	if err := db.Migrator.QueryRow(t.Context(), "select current_user").Scan(&user); err != nil {
		t.Fatalf("select current_user: %v", err)
	}
	if user != "mypark_migrator" {
		t.Errorf("Migrator current_user = %q, want mypark_migrator", user)
	}
}

// fakeTB lets a test observe what NewEmptyDB does when it fails, without
// failing the real test: Fatalf records and stops the goroutine.
type fakeTB struct {
	testing.TB
	cleanups []func()
	failed   bool
}

func (f *fakeTB) Helper()               {}
func (f *fakeTB) Cleanup(fn func())     { f.cleanups = append(f.cleanups, fn) }
func (f *fakeTB) Errorf(string, ...any) { f.failed = true }
func (f *fakeTB) Fatalf(string, ...any) { f.failed = true; runtime.Goexit() }
func (f *fakeTB) Skipf(string, ...any)  { runtime.Goexit() }
func (f *fakeTB) runCleanups() {
	for i := len(f.cleanups) - 1; i >= 0; i-- {
		f.cleanups[i]()
	}
}

func TestNewEmptyDB_DropRegisteredBeforePool(t *testing.T) {
	super := NewEmptyDB(t) // also skips when no database is configured

	var dbURL string
	orig := newPoolFunc
	newPoolFunc = func(_ context.Context, url string, _ int32) (*pgxpool.Pool, error) {
		dbURL = url
		return nil, errors.New("injected pool failure")
	}
	t.Cleanup(func() { newPoolFunc = orig })

	fake := &fakeTB{TB: t}
	done := make(chan struct{})
	go func() {
		defer close(done)
		newEmptyDB(fake)
	}()
	<-done
	fake.runCleanups()

	if !fake.failed || dbURL == "" {
		t.Fatalf("expected the injected pool failure to be reached (failed=%v, url set=%v)", fake.failed, dbURL != "")
	}
	u, err := url.Parse(dbURL)
	if err != nil {
		t.Fatalf("parse url: %v", err)
	}
	name := strings.TrimPrefix(u.Path, "/")
	var exists bool
	if err := super.QueryRow(t.Context(), "select exists (select from pg_database where datname = $1)", name).Scan(&exists); err != nil {
		t.Fatalf("query pg_database: %v", err)
	}
	if exists {
		t.Errorf("database %s leaked after a pool error", name)
	}
}
