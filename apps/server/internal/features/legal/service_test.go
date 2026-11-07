package legal_test

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/XYZLZ/MyPark/apps/server/internal/domain/apperr"
	"github.com/XYZLZ/MyPark/apps/server/internal/domain/errcodes"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/legal"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/legal/postgres"
	"github.com/XYZLZ/MyPark/apps/server/internal/testsupport"
)

var t0 = time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)

func insertDoc(t *testing.T, pool *pgxpool.Pool, kind, version string, publishedAt time.Time) {
	t.Helper()
	_, err := pool.Exec(t.Context(),
		`insert into legal_documents (kind, version, published_at, url_es, url_en)
		 values ($1, $2, $3, 'https://x/es/'||$1, 'https://x/en/'||$1)`, kind, version, publishedAt)
	if err != nil {
		t.Fatalf("insert %s %s: %v", kind, version, err)
	}
}

func newService(t *testing.T) (*legal.Service, *pgxpool.Pool, *testsupport.FakeClock) {
	t.Helper()
	pool := testsupport.NewDB(t)
	clk := testsupport.NewFakeClock(t0)
	return legal.NewService(postgres.NewRepo(pool), clk, time.Minute), pool, clk
}

func TestCurrent_PicksLatestPublishedNotFuture(t *testing.T) {
	svc, pool, _ := newService(t)
	for _, kind := range []string{"terms", "privacy"} {
		insertDoc(t, pool, kind, "2026-11-16", time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC))
		insertDoc(t, pool, kind, "2027-01-01", time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC))
	}
	got, err := svc.Versions(t.Context())
	if err != nil {
		t.Fatalf("Versions: %v", err)
	}
	if got.Terms.Version != "2026-11-16" || got.Privacy.Version != "2026-11-16" {
		t.Errorf("versions = %q / %q, want 2026-11-16 for both", got.Terms.Version, got.Privacy.Version)
	}
	if got.Terms.Kind != "terms" || got.Privacy.Kind != "privacy" {
		t.Errorf("kinds = %q / %q", got.Terms.Kind, got.Privacy.Kind)
	}
	if got.Privacy.URLEn != "https://x/en/privacy" || got.Terms.URLEs != "https://x/es/terms" {
		t.Errorf("urls not mapped: %+v", got)
	}
}

func TestCurrent_CacheExpiresWithClock(t *testing.T) {
	svc, pool, clk := newService(t)
	for _, kind := range []string{"terms", "privacy"} {
		insertDoc(t, pool, kind, "2026-11-16", time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC))
	}
	if got, err := svc.Versions(t.Context()); err != nil || got.Terms.Version != "2026-11-16" {
		t.Fatalf("first Versions = %+v, %v", got, err)
	}
	for _, kind := range []string{"terms", "privacy"} {
		insertDoc(t, pool, kind, "2026-11-19", time.Date(2026, 11, 19, 0, 0, 0, 0, time.UTC))
	}
	clk.Advance(59 * time.Second)
	if got, _ := svc.Versions(t.Context()); got.Terms.Version != "2026-11-16" {
		t.Errorf("within ttl: version = %q, want the cached 2026-11-16", got.Terms.Version)
	}
	clk.Advance(2 * time.Second)
	got, err := svc.Versions(t.Context())
	if err != nil || got.Terms.Version != "2026-11-19" || got.Privacy.Version != "2026-11-19" {
		t.Errorf("after ttl: %+v, %v, want 2026-11-19", got, err)
	}
}

func TestCurrent_MissingDocumentIsAnError(t *testing.T) {
	svc, _, _ := newService(t)
	if _, err := svc.Versions(t.Context()); err == nil {
		t.Fatal("Versions with no legal documents returned nil, want an error")
	}
}

func TestValidateAcceptance_Outdated(t *testing.T) {
	svc, pool, _ := newService(t)
	for _, kind := range []string{"terms", "privacy"} {
		insertDoc(t, pool, kind, "2026-11-16", time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC))
	}
	if err := svc.ValidateAcceptance(t.Context(), "2026-11-16", "2026-11-16"); err != nil {
		t.Fatalf("current versions: %v", err)
	}
	for _, tc := range [][2]string{{"2026-01-01", "2026-11-16"}, {"2026-11-16", "old"}, {"", ""}} {
		err := svc.ValidateAcceptance(t.Context(), tc[0], tc[1])
		ae, ok := err.(*apperr.Error)
		if !ok {
			t.Fatalf("ValidateAcceptance(%q, %q) = %v, want *apperr.Error", tc[0], tc[1], err)
		}
		if ae.Kind != apperr.KindConflict || ae.Code != errcodes.LegalVersionOutdated {
			t.Errorf("kind/code = %v/%s, want conflict/legal.version_outdated", ae.Kind, ae.Code)
		}
		if ae.Details["terms_version"] != "2026-11-16" || ae.Details["privacy_version"] != "2026-11-16" {
			t.Errorf("details = %v, want the current versions", ae.Details)
		}
	}
}

func TestSeedDocuments_Idempotent(t *testing.T) {
	db := testsupport.NewTestDB(t)
	for range 2 {
		if err := legal.SeedDocuments(t.Context(), db.App); err != nil {
			t.Fatalf("SeedDocuments: %v", err)
		}
	}
	var n int
	if err := db.App.QueryRow(t.Context(), "select count(*) from legal_documents").Scan(&n); err != nil || n != 2 {
		t.Fatalf("rows = %d, %v, want 2", n, err)
	}
	var url string
	if err := db.App.QueryRow(t.Context(),
		"select url_es from legal_documents where kind = 'terms' and version = $1", legal.InitialVersion).Scan(&url); err != nil {
		t.Fatalf("select: %v", err)
	}
	if url != "http://localhost:3000/es-DO/legal/terms" {
		t.Errorf("url_es = %q", url)
	}
}
