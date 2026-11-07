package main

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/XYZLZ/MyPark/apps/server/internal/config"
	"github.com/XYZLZ/MyPark/apps/server/internal/testsupport"
)

func TestRun_MissingDatabaseURL(t *testing.T) {
	err := run(t.Context(), func(string) string { return "" }, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "DATABASE_URL is required") {
		t.Fatalf("run error = %v, want one naming DATABASE_URL", err)
	}
}

func TestRun_InvalidDatabaseURL(t *testing.T) {
	env := map[string]string{"DATABASE_URL": "s3cret-not-a-url"}
	err := run(t.Context(), func(k string) string { return env[k] }, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "DATABASE_URL must be a valid URL") {
		t.Fatalf("run error = %v, want one naming DATABASE_URL", err)
	}
	if strings.Contains(err.Error(), "s3cret") {
		t.Errorf("error echoes the secret value: %v", err)
	}
}

func TestSeed_InsertsLegalDocumentsIdempotently(t *testing.T) {
	db := testsupport.NewTestDB(t)
	env := map[string]string{"DATABASE_URL": db.AppURL}
	for range 2 {
		if err := run(t.Context(), func(k string) string { return env[k] }, io.Discard); err != nil {
			t.Fatalf("run: %v", err)
		}
	}
	var docs, lots int
	if err := db.App.QueryRow(t.Context(), "select count(*) from legal_documents").Scan(&docs); err != nil {
		t.Fatalf("count legal_documents: %v", err)
	}
	if err := db.App.QueryRow(t.Context(), "select count(*) from lots").Scan(&lots); err != nil {
		t.Fatalf("count lots: %v", err)
	}
	if docs != 2 || lots != 1 {
		t.Errorf("legal_documents = %d, lots = %d, want 2 and 1", docs, lots)
	}
}

func adminSeedEnv(dbURL string) map[string]string {
	return map[string]string{
		"DATABASE_URL": dbURL, "SEED_ADMIN_EMAIL": "admin@mypark.local", "SEED_ADMIN_FIRST_NAME": "MyPark",
		"SEED_ADMIN_LAST_NAME": "Admin", "SEED_ADMIN_PASSWORD": config.DevSeedAdminPassword,
	}
}

func TestSeed_CreatesFirstAdminOnce(t *testing.T) {
	db := testsupport.NewTestDB(t)
	env := adminSeedEnv(db.AppURL)
	var out bytes.Buffer
	for range 2 {
		if err := run(t.Context(), func(k string) string { return env[k] }, &out); err != nil {
			t.Fatalf("run: %v", err)
		}
	}
	var admins int
	var hash string
	if err := db.App.QueryRow(t.Context(),
		"select count(*), coalesce(max(password_hash), '') from users where role = 'admin' and status = 'active'").Scan(&admins, &hash); err != nil {
		t.Fatal(err)
	}
	if admins != 1 || !strings.HasPrefix(hash, "$argon2id$") {
		t.Errorf("admins = %d, hash is argon2id = %v", admins, strings.HasPrefix(hash, "$argon2id$"))
	}
	if !strings.Contains(out.String(), "admin seeded") || !strings.Contains(out.String(), "admin seed skipped") {
		t.Errorf("logs = %s", out.String())
	}
}

func TestCmdSeed_PasswordNeverLogged(t *testing.T) {
	db := testsupport.NewTestDB(t)
	env := adminSeedEnv(db.AppURL)
	var out bytes.Buffer
	for range 2 {
		if err := run(t.Context(), func(k string) string { return env[k] }, &out); err != nil {
			t.Fatalf("run: %v", err)
		}
	}
	if s := out.String(); strings.Contains(s, config.DevSeedAdminPassword) || strings.Contains(s, "admin@mypark.local") {
		t.Errorf("stdout leaks the seed credentials: %s", s)
	}
}

func TestSeed_WithoutAdminEnvSkipsWithWarning(t *testing.T) {
	db := testsupport.NewTestDB(t)
	env := map[string]string{"DATABASE_URL": db.AppURL}
	var out bytes.Buffer
	if err := run(t.Context(), func(k string) string { return env[k] }, &out); err != nil {
		t.Fatalf("run: %v", err)
	}
	var users int
	if err := db.App.QueryRow(t.Context(), "select count(*) from users").Scan(&users); err != nil || users != 0 {
		t.Errorf("users = %d, err = %v", users, err)
	}
	if !strings.Contains(out.String(), "WARN") || !strings.Contains(out.String(), "SEED_ADMIN_EMAIL") {
		t.Errorf("want a warning naming SEED_ADMIN_EMAIL, got %s", out.String())
	}
}

func TestSeed_ProductionRefusesDevAdminPasswordAndWritesNothing(t *testing.T) {
	db := testsupport.NewTestDB(t)
	env := adminSeedEnv(db.AppURL)
	env["APP_ENV"] = "production"
	err := run(t.Context(), func(k string) string { return env[k] }, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "SEED_ADMIN_PASSWORD") || strings.Contains(err.Error(), config.DevSeedAdminPassword) {
		t.Fatalf("run error = %v", err)
	}
	var lots int
	if err := db.App.QueryRow(t.Context(), "select count(*) from lots").Scan(&lots); err != nil || lots != 0 {
		t.Errorf("lots = %d, err = %v: a refused seed must write nothing", lots, err)
	}
}
