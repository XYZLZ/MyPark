package lots_test

import (
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/XYZLZ/MyPark/apps/server/internal/features/lots"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/database/dbgen"
	"github.com/XYZLZ/MyPark/apps/server/internal/testsupport"
)

const seedCapacity = 280

func TestSeed_Creates280Spaces(t *testing.T) {
	pool := testsupport.NewDB(t)
	ctx := t.Context()
	q := dbgen.New(pool)

	lotID, err := lots.Seed(ctx, q)
	if err != nil {
		t.Fatalf("Seed: %v", err)
	}
	n, err := q.CountSpacesByLot(ctx, lotID)
	if err != nil {
		t.Fatalf("CountSpacesByLot: %v", err)
	}
	if n != seedCapacity {
		t.Errorf("spaces = %d, want %d", n, seedCapacity)
	}

	var first, last string
	err = pool.QueryRow(ctx, "select min(code), max(code) from spaces where lot_id = $1", lotID).Scan(&first, &last)
	if err != nil {
		t.Fatalf("select codes: %v", err)
	}
	if first != "E001" || last != "E280" {
		t.Errorf("codes = %s..%s, want E001..E280", first, last)
	}

	var occupied int32
	err = pool.QueryRow(ctx, "select occupied from lot_occupancy where lot_id = $1", lotID).Scan(&occupied)
	if err != nil {
		t.Fatalf("select lot_occupancy: %v", err)
	}
	if occupied != 0 {
		t.Errorf("occupied = %d, want 0", occupied)
	}

	var slug, name string
	var capacity int32
	var latNull bool
	err = pool.QueryRow(ctx, "select slug, name, capacity, latitude is null from lots where id = $1", lotID).
		Scan(&slug, &name, &capacity, &latNull)
	if err != nil {
		t.Fatalf("select lot: %v", err)
	}
	if slug != "zona-colonial" || name != "Parqueo Banreservas Zona Colonial" || capacity != seedCapacity || !latNull {
		t.Errorf("lot = (%q, %q, %d, latitude null %v), want seed values", slug, name, capacity, latNull)
	}
}

// TestSeed_Idempotent covers review focus 3: a compose restart re-runs the
// seed, which must not duplicate the lot or its spaces.
func TestSeed_Idempotent(t *testing.T) {
	pool := testsupport.NewDB(t)
	ctx := t.Context()

	first, err := lots.SeedTx(ctx, pool)
	if err != nil {
		t.Fatalf("first SeedTx: %v", err)
	}
	second, err := lots.SeedTx(ctx, pool)
	if err != nil {
		t.Fatalf("second SeedTx: %v", err)
	}
	if first != second {
		t.Errorf("lot IDs = %d then %d, want the same lot", first, second)
	}
	assertCount(t, pool, "select count(*) from lots", 1)
	assertCount(t, pool, "select count(*) from spaces", seedCapacity)
	assertCount(t, pool, "select count(*) from lot_occupancy", 1)
}

// TestSeed_AsAppRole checks the seed runs with DML-only privileges, as
// cmd/seed does in production (DATABASE_URL is the mypark_app role).
func TestSeed_AsAppRole(t *testing.T) {
	pool := testsupport.NewDB(t)
	ctx := t.Context()
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, "set role mypark_app"); err != nil {
		t.Fatalf("set role: %v", err)
	}
	defer conn.Exec(ctx, "reset role") //nolint:errcheck // best effort before release

	if _, err := lots.Seed(ctx, dbgen.New(conn)); err != nil {
		t.Fatalf("Seed as mypark_app: %v", err)
	}
}

func assertCount(t *testing.T, pool *pgxpool.Pool, query string, want int64) {
	t.Helper()
	var got int64
	if err := pool.QueryRow(t.Context(), query).Scan(&got); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	if got != want {
		t.Errorf("%s = %d, want %d", query, got, want)
	}
}
