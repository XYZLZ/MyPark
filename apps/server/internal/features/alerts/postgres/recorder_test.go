package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/XYZLZ/MyPark/apps/server/internal/features/alerts"
	alertspg "github.com/XYZLZ/MyPark/apps/server/internal/features/alerts/postgres"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/database"
	"github.com/XYZLZ/MyPark/apps/server/internal/testsupport"
)

func TestAlerts_Open(t *testing.T) {
	db := testsupport.NewTestDB(t)
	lot := testsupport.SeedPilotLot(t, db)
	now := time.Date(2026, 12, 1, 13, 58, 10, 0, time.UTC)
	rec := alertspg.NewRecorder()

	err := database.NewTxRunner(db.App).InTx(t.Context(), func(ctx context.Context) error {
		return rec.Open(ctx, lot, alerts.KindRefundFailed, map[string]string{"reservation_id": "r-1", "reason": "duplicate_charge"}, now)
	})
	if err != nil {
		t.Fatalf("Open in a tx: %v", err)
	}
	var kind, reason string
	var opened time.Time
	var resolved *time.Time
	err = db.App.QueryRow(t.Context(),
		`select kind, details->>'reason', opened_at, resolved_at from reconciliation_alerts where lot_id = $1`, lot).
		Scan(&kind, &reason, &opened, &resolved)
	if err != nil {
		t.Fatal(err)
	}
	if kind != "refund_failed" || reason != "duplicate_charge" || !opened.Equal(now) || resolved != nil {
		t.Errorf("row = %s %s %v %v, want an open refund_failed alert opened at the injected now", kind, reason, opened, resolved)
	}

	if err := rec.Open(t.Context(), lot, alerts.KindReservationUnadmitted, nil, now); !errors.Is(err, database.ErrNoTx) {
		t.Errorf("Open outside a tx = %v, want database.ErrNoTx", err)
	}
}
