package main

import (
	"io"
	"strings"
	"testing"
)

func TestRun_MissingMigratorDatabaseURL(t *testing.T) {
	err := run(t.Context(), func(string) string { return "" }, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "MIGRATOR_DATABASE_URL is required") {
		t.Fatalf("run error = %v, want one naming MIGRATOR_DATABASE_URL", err)
	}
}

func TestRun_InvalidMigratorDatabaseURL(t *testing.T) {
	env := map[string]string{"MIGRATOR_DATABASE_URL": "s3cret-not-a-url"}
	err := run(t.Context(), func(k string) string { return env[k] }, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "MIGRATOR_DATABASE_URL must be a valid URL") {
		t.Fatalf("run error = %v, want one naming MIGRATOR_DATABASE_URL", err)
	}
	if strings.Contains(err.Error(), "s3cret") {
		t.Errorf("error echoes the secret value: %v", err)
	}
}

func TestConnConfig_RuntimeParams(t *testing.T) {
	cfg, err := connConfig("postgres://mypark_migrator:x@localhost:5432/mypark")
	if err != nil {
		t.Fatalf("connConfig: %v", err)
	}
	if got := cfg.RuntimeParams["lock_timeout"]; got != "10s" {
		t.Errorf("lock_timeout = %q, want 10s", got)
	}
	if got := cfg.RuntimeParams["application_name"]; got != "mypark-migrate" {
		t.Errorf("application_name = %q, want mypark-migrate", got)
	}
	if _, ok := cfg.RuntimeParams["statement_timeout"]; ok {
		t.Error("statement_timeout is set, want it unset: migrations may run long")
	}
}

func TestConnConfig_URLParamsWin(t *testing.T) {
	cfg, err := connConfig("postgres://mypark_migrator:x@localhost:5432/mypark?lock_timeout=30s&application_name=ops")
	if err != nil {
		t.Fatalf("connConfig: %v", err)
	}
	if got := cfg.RuntimeParams["lock_timeout"]; got != "30s" {
		t.Errorf("lock_timeout = %q, want 30s from the URL", got)
	}
	if got := cfg.RuntimeParams["application_name"]; got != "ops" {
		t.Errorf("application_name = %q, want ops from the URL", got)
	}
}
