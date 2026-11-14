package testsupport

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/XYZLZ/MyPark/apps/server/internal/features/tariffs"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/database/dbgen"
)

// AssertOccupancyInvariant fails t unless the lot's occupancy counter equals
// its stays inside the lot: occupied = count(status in ('active','paid'))
// (design §9.6). Call it at the end of every gate integration test.
func AssertOccupancyInvariant(t testing.TB, db *pgxpool.Pool, lotID int64) {
	t.Helper()
	var occupied, inside int64
	if err := db.QueryRow(t.Context(),
		`select o.occupied,
		        (select count(*) from stays s where s.lot_id = o.lot_id and s.status in ('active', 'paid'))
		 from lot_occupancy o where o.lot_id = $1`, lotID).Scan(&occupied, &inside); err != nil {
		t.Fatalf("testsupport: read occupancy of lot %d: %v", lotID, err)
	}
	if occupied != inside {
		t.Errorf("occupancy invariant broken for lot %d: occupied = %d, active or paid stays = %d", lotID, occupied, inside)
	}
}

// FillLot inserts n active ticket stays in the lot, entered and accepted an
// hour before now (the test clock), under the lot's tariff version, and sets
// the occupancy counter to n, so the invariant holds.
func FillLot(t testing.TB, db *TestDB, lotID int64, n int, now time.Time) {
	t.Helper()
	if _, err := db.App.Exec(t.Context(),
		`insert into stays (lot_id, credential_kind, ticket_code_hash, status, tariff_version_id,
		                    accepted_at, entered_at, updated_at)
		 select $1, 'ticket', sha256(('fill:' || $1 || ':' || i)::bytea), 'active', v.id,
		        $3::timestamptz - interval '1 hour', $3::timestamptz - interval '1 hour', $3::timestamptz
		 from generate_series(1, $2::int) i,
		      (select id from tariff_versions where lot_id = $1 order by effective_from desc limit 1) v`,
		lotID, n, now); err != nil {
		t.Fatalf("testsupport: fill lot %d: %v", lotID, err)
	}
	if _, err := db.App.Exec(t.Context(),
		`update lot_occupancy set occupied = $2, version = version + 1, updated_at = $3 where lot_id = $1`, lotID, n, now); err != nil {
		t.Fatalf("testsupport: set occupancy of lot %d: %v", lotID, err)
	}
}

// SeedSecondLot seeds another small lot with its occupancy row and default
// tariff, for lot-scoping tests, and returns its id.
func SeedSecondLot(t testing.TB, db *TestDB) int64 {
	t.Helper()
	q := dbgen.New(db.App)
	lotID, err := q.UpsertLotBySlug(t.Context(), dbgen.UpsertLotBySlugParams{
		Slug: "second-lot", Name: "Second test lot", Address: "Test address", Capacity: 10,
	})
	if err != nil {
		t.Fatalf("testsupport: seed second lot: %v", err)
	}
	if err := q.InitLotOccupancy(t.Context(), lotID); err != nil {
		t.Fatalf("testsupport: second lot occupancy: %v", err)
	}
	if err := tariffs.SeedDefaults(t.Context(), q, lotID); err != nil {
		t.Fatalf("testsupport: second lot tariff: %v", err)
	}
	return lotID
}
