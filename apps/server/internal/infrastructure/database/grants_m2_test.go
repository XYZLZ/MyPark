package database_test

import (
	"testing"

	"github.com/XYZLZ/MyPark/apps/server/internal/features/tariffs"
	"github.com/XYZLZ/MyPark/apps/server/internal/testsupport"
)

// TestGrants_M2Tables has one subtest per M2 migration.
func TestGrants_M2Tables(t *testing.T) {
	t.Run("tariffs", func(t *testing.T) {
		db := testsupport.NewTestDB(t)
		lotID := testsupport.SeedPilotLot(t, db)
		const insertPolicy = `insert into policy_versions (lot_id, effective_from, max_reservable_pct,
			booking_horizon_days, arrival_grace_min, early_arrival_min, payment_timeout_min, max_active_per_user,
			slot_min, noshow_block_threshold, noshow_window_days, noshow_block_days)
			values ($1, '2027-01-01T00:00:00-04:00', 30, 7, 20, 15, 10, 1, 5, 3, 30, 7)`
		const insertTariff = `insert into tariff_versions (lot_id, effective_from, first_hour_cents,
			additional_hour_cents, exit_grace_min, post_payment_exit_min, daily_cap_cents, lost_ticket_cents,
			reservation_fee_cents) values ($1, '2027-01-01T00:00:00-04:00', 5000, 2500, 10, 15, 40000, 50000, 15000)`

		for table, insert := range map[string]string{"tariff_versions": insertTariff, "policy_versions": insertPolicy} {
			if got := execCode(t, db.App, "select count(*) from "+table); got != "" {
				t.Errorf("%s: select as mypark_app: SQLSTATE %q, want success", table, got)
			}
			if got := execCode(t, db.App, insert, lotID); got != "" {
				t.Errorf("%s: insert as mypark_app: SQLSTATE %q, want success", table, got)
			}
			for _, stmt := range []string{
				"update " + table + " set created_at = now()",
				"delete from " + table,
				"truncate " + table,
			} {
				if got := execCode(t, db.App, stmt); got != sqlStateInsufficientPrivilege {
					t.Errorf("%q as mypark_app: SQLSTATE %q, want %s", stmt, got, sqlStateInsufficientPrivilege)
				}
			}
		}
	})

	t.Run("tariffs checks and uniqueness", func(t *testing.T) {
		db := testsupport.NewTestDB(t)
		lotID := testsupport.SeedPilotLot(t, db)
		def := tariffs.DefaultTariff()
		const q = `insert into tariff_versions (lot_id, effective_from, first_hour_cents, additional_hour_cents,
			exit_grace_min, post_payment_exit_min, daily_cap_cents, lost_ticket_cents, reservation_fee_cents)
			values ($1, $2, $3, 2500, 10, 15, $4, 50000, 15000)`
		if got := execCode(t, db.App, q, lotID, def.EffectiveFrom, 5000, 40000); got != sqlStateUniqueViolation {
			t.Errorf("duplicate (lot_id, effective_from): SQLSTATE %q, want %s", got, sqlStateUniqueViolation)
		}
		later := def.EffectiveFrom.AddDate(0, 1, 0)
		if got := execCode(t, db.App, q, lotID, later, 5000, 4999); got != sqlStateCheckViolation {
			t.Errorf("daily cap below first hour: SQLSTATE %q, want %s", got, sqlStateCheckViolation)
		}
		if got := execCode(t, db.App, q, lotID, later, -1, 40000); got != sqlStateCheckViolation {
			t.Errorf("negative first hour: SQLSTATE %q, want %s", got, sqlStateCheckViolation)
		}
	})
}
