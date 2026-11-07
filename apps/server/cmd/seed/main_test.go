package main

import (
	"io"
	"strings"
	"testing"

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
