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
