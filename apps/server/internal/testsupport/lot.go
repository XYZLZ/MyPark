package testsupport

import (
	"testing"

	"github.com/XYZLZ/MyPark/apps/server/internal/features/lots"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/tariffs"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/database/dbgen"
)

// SeedPilotLot seeds the pilot lot, its spaces and occupancy, and its default
// tariff and policy on the App pool, and returns the lot id.
func SeedPilotLot(t testing.TB, db *TestDB) (lotID int64) {
	t.Helper()
	q := dbgen.New(db.App)
	lotID, err := lots.Seed(t.Context(), q)
	if err != nil {
		t.Fatalf("seed pilot lot: %v", err)
	}
	if err := tariffs.SeedDefaults(t.Context(), q, lotID); err != nil {
		t.Fatalf("seed default tariff: %v", err)
	}
	return lotID
}
