package gate_test

import (
	"context"
	"testing"
	"time"

	"github.com/XYZLZ/MyPark/apps/server/internal/features/gate"
	"github.com/XYZLZ/MyPark/apps/server/internal/testsupport"
)

// insertPending inserts n entry_pending ticket stays accepted at at.
func insertPending(t *testing.T, db *testsupport.TestDB, lotID int64, n int, tag string, at time.Time) {
	t.Helper()
	if _, err := db.App.Exec(t.Context(),
		`insert into stays (lot_id, credential_kind, ticket_code_hash, status, tariff_version_id, accepted_at, updated_at)
		 select $1, 'ticket', sha256(($3 || i)::bytea), 'entry_pending', v.id, $4, $4
		 from generate_series(1, $2::int) i, (select id from tariff_versions where lot_id = $1 order by effective_from desc limit 1) v`,
		lotID, n, tag, at); err != nil {
		t.Fatalf("insert pending: %v", err)
	}
}

func TestVoidUnpassed_FakeClock(t *testing.T) {
	db := testsupport.NewTestDB(t)
	lotID := testsupport.SeedPilotLot(t, db)
	clk := testsupport.NewFakeClock(time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC))
	now := clk.Now()
	insertPending(t, db, lotID, 3, "stale", now.Add(-61*time.Second))
	insertPending(t, db, lotID, 1, "fresh", now.Add(-30*time.Second))

	var version int64
	if err := db.App.QueryRow(t.Context(), `select version from lot_occupancy where lot_id = $1`, lotID).Scan(&version); err != nil {
		t.Fatal(err)
	}
	conn, err := db.Super.Acquire(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	if _, err := conn.Exec(t.Context(), `listen lot_changed`); err != nil {
		t.Fatal(err)
	}

	n, err := gate.VoidUnpassedEntries(t.Context(), db.App, now)
	if err != nil || n != 3 {
		t.Fatalf("VoidUnpassedEntries = %d, %v; want 3", n, err)
	}
	var voided, pending int64
	if err := db.App.QueryRow(t.Context(),
		`select count(*) filter (where status = 'voided' and voided_at = $2 and updated_at = $2),
		        count(*) filter (where status = 'entry_pending')
		 from stays where lot_id = $1`, lotID, now).Scan(&voided, &pending); err != nil {
		t.Fatal(err)
	}
	if voided != 3 || pending != 1 {
		t.Errorf("voided at now = %d, pending = %d; want 3 and the fresh one", voided, pending)
	}
	var occupied, after int64
	if err := db.App.QueryRow(t.Context(), `select occupied, version from lot_occupancy where lot_id = $1`, lotID).Scan(&occupied, &after); err != nil {
		t.Fatal(err)
	}
	if occupied != 0 || after != version {
		t.Errorf("occupancy = %d version %d, want 0 and %d unchanged", occupied, after, version)
	}

	// Notifications arrive in commit order: the sentinel comes first only if
	// the job sent none.
	if _, err := db.Super.Exec(t.Context(), `select pg_notify('lot_changed', 'sentinel')`); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	if got, err := conn.Conn().WaitForNotification(ctx); err != nil || got.Payload != "sentinel" {
		t.Errorf("first notification = %v, %v; want the sentinel (the job notifies nothing)", got, err)
	}
	testsupport.AssertOccupancyInvariant(t, db.App, lotID)
}

func TestVoidUnpassed_Batches(t *testing.T) {
	db := testsupport.NewTestDB(t)
	lotID := testsupport.SeedPilotLot(t, db)
	now := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	insertPending(t, db, lotID, 450, "stale", now.Add(-2*time.Minute))

	n, err := gate.VoidUnpassedEntries(t.Context(), db.App, now)
	if err != nil || n != 450 {
		t.Fatalf("VoidUnpassedEntries = %d, %v; want 450 over 3 batches", n, err)
	}
	var left int64
	if err := db.App.QueryRow(t.Context(), `select count(*) from stays where status = 'entry_pending'`).Scan(&left); err != nil || left != 0 {
		t.Errorf("pending left = %d, %v; want 0", left, err)
	}
	testsupport.AssertOccupancyInvariant(t, db.App, lotID)
}
