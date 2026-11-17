package database_test

import (
	"testing"

	"github.com/XYZLZ/MyPark/apps/server/internal/testsupport"
)

// TestGrants_M3Tables checks the privileges of mypark_app on the M3 tables:
// DML is allowed, and nothing financial or auditable can be deleted or
// truncated (policies 1: five-year retention).
func TestGrants_M3Tables(t *testing.T) {
	db := testsupport.NewTestDB(t)

	// Each privilege is checked on its own: has_table_privilege with a list
	// is true when ANY privilege is held.
	allowed := map[string][]string{
		"reservations":           {"SELECT", "INSERT", "UPDATE"},
		"reservation_slot_loads": {"SELECT", "INSERT", "UPDATE", "DELETE"},
		"refunds":                {"SELECT", "INSERT", "UPDATE"},
		"sim_gateway_charges":    {"SELECT", "INSERT", "UPDATE"},
		"reconciliation_alerts":  {"SELECT", "INSERT", "UPDATE"},
	}
	for table, privs := range allowed {
		for _, priv := range privs {
			var ok bool
			if err := db.Super.QueryRow(t.Context(),
				"select has_table_privilege('mypark_app', $1, $2)", table, priv).Scan(&ok); err != nil {
				t.Fatalf("has_table_privilege(%s, %s): %v", table, priv, err)
			}
			if !ok {
				t.Errorf("mypark_app lacks %s on %s", priv, table)
			}
		}
	}

	for _, table := range []string{"reservations", "payments", "refunds", "sim_gateway_charges", "reconciliation_alerts"} {
		for _, stmt := range []string{"delete from " + table, "truncate " + table} {
			if got := execCode(t, db.App, stmt); got != sqlStateInsufficientPrivilege {
				t.Errorf("%q as mypark_app: SQLSTATE %q, want %s", stmt, got, sqlStateInsufficientPrivilege)
			}
		}
	}
}
