package database

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"path"

	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"

	"github.com/XYZLZ/MyPark/apps/server/internal/features/health"
)

// MigrationsFS holds the goose SQL migrations under migrations/.
//
//go:embed migrations/*.sql
var MigrationsFS embed.FS

const (
	migrationsDir = "migrations"
	// versionTable is goose's default version table, created in the
	// connection's default schema (public).
	versionTable = "goose_db_version"
)

// MigrateUp applies every pending embedded migration. It holds a Postgres
// session advisory lock while running, so concurrent migrators (two deploys
// starting at once) apply each migration exactly once. db must connect as a
// role allowed to run DDL (mypark_migrator).
func MigrateUp(ctx context.Context, db *sql.DB) error {
	fsys, err := fs.Sub(MigrationsFS, migrationsDir)
	if err != nil {
		return fmt.Errorf("migrations: %w", err)
	}
	locker, err := lock.NewPostgresSessionLocker()
	if err != nil {
		return fmt.Errorf("migrations: create locker: %w", err)
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, db, fsys, goose.WithSessionLocker(locker))
	if err != nil {
		return fmt.Errorf("migrations: %w", err)
	}
	if _, err := provider.Up(ctx); err != nil {
		return fmt.Errorf("migrations: up: %w", err)
	}
	return nil
}

// migrationStatus implements health.MigrationStatus with read-only queries.
// It deliberately does not use goose.Provider: its version getters create
// the version table when it is missing, which is DDL that the app role may
// not run and that a readiness probe must never run.
type migrationStatus struct {
	db       *sql.DB
	expected int64
}

// NewMigrationStatus returns a health.MigrationStatus that compares the
// highest applied version in the database with the highest embedded
// migration version.
func NewMigrationStatus(db *sql.DB) health.MigrationStatus {
	return &migrationStatus{db: db, expected: expectedVersion()}
}

// Status returns current = 0 when the version table does not exist yet,
// i.e. no migration has ever run.
func (m *migrationStatus) Status(ctx context.Context) (current, expected int64, err error) {
	var exists bool
	if err := m.db.QueryRowContext(ctx, "select to_regclass($1) is not null", versionTable).Scan(&exists); err != nil {
		return 0, m.expected, fmt.Errorf("migrations: check version table: %w", err)
	}
	if !exists {
		return 0, m.expected, nil
	}
	err = m.db.QueryRowContext(ctx,
		"select coalesce(max(version_id), 0) from "+versionTable+" where is_applied").Scan(&current)
	if err != nil {
		return 0, m.expected, fmt.Errorf("migrations: read current version: %w", err)
	}
	return current, m.expected, nil
}

// expectedVersion returns the highest version among the embedded migration
// files. A file name that goose cannot parse is a build defect, so it
// panics; TestMigrationStatus_UpToDate guards it.
func expectedVersion() int64 {
	files, err := fs.Glob(MigrationsFS, path.Join(migrationsDir, "*.sql"))
	if err != nil {
		panic(fmt.Sprintf("migrations: glob: %v", err))
	}
	var highest int64
	for _, f := range files {
		v, err := goose.NumericComponent(f)
		if err != nil {
			panic(fmt.Sprintf("migrations: %s: %v", f, err))
		}
		highest = max(highest, v)
	}
	return highest
}
