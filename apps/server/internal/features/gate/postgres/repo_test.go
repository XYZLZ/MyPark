package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/XYZLZ/MyPark/apps/server/internal/features/gate"
	gatepg "github.com/XYZLZ/MyPark/apps/server/internal/features/gate/postgres"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/database"
	"github.com/XYZLZ/MyPark/apps/server/internal/testsupport"
)

// A reservation stay is inserted with its reservation, found by it within
// the lot only, reopened from voided (voided_at kept) and refreshed.
func TestRepo_ReservationStay(t *testing.T) {
	db := testsupport.NewTestDB(t)
	lotID := testsupport.SeedPilotLot(t, db)
	now := time.Date(2026, 12, 1, 14, 0, 0, 0, time.UTC)
	d := testsupport.SeedDrivers(t, db, 1)
	testsupport.SeedReservations(t, db, lotID, d, now.Add(5*time.Minute), now.Add(-10*time.Minute), now.Add(25*time.Minute), now)
	var rsvID, tariffID int64
	if err := db.App.QueryRow(t.Context(), `select id, tariff_version_id from reservations`).Scan(&rsvID, &tariffID); err != nil {
		t.Fatal(err)
	}
	repo := gatepg.NewRepo()
	tx := database.NewTxRunner(db.App)
	run := func(fn func(ctx context.Context) error) {
		t.Helper()
		if err := tx.InTx(t.Context(), fn); err != nil {
			t.Fatal(err)
		}
	}

	var stayID int64
	run(func(ctx context.Context) error {
		if _, err := repo.ReservationStay(ctx, lotID, rsvID); !errors.Is(err, gate.ErrNotFound) {
			t.Errorf("before insert: %v, want ErrNotFound", err)
		}
		if _, err := repo.InsertStay(ctx, gate.NewStay{LotID: lotID, ReservationID: rsvID, TariffVersionID: tariffID, AcceptedAt: now}); err != nil {
			return err
		}
		s, err := repo.ReservationStayForUpdate(ctx, lotID, rsvID)
		if err != nil {
			return err
		}
		if s.CredentialKind != gate.KindReservation || s.Status != gate.StatusEntryPending || s.ReservationID != rsvID || s.ReservationUserID != d[0].UserID {
			t.Errorf("stay = %+v, want an entry_pending reservation stay of user %d", s, d[0].UserID)
		}
		if _, err := repo.ReservationStay(ctx, lotID+1, rsvID); !errors.Is(err, gate.ErrNotFound) {
			t.Errorf("other lot: %v, want ErrNotFound", err)
		}
		stayID = s.ID
		return repo.RefreshAccepted(ctx, lotID, stayID, now.Add(20*time.Second))
	})

	voidedAt := now.Add(90 * time.Second)
	if _, err := db.App.Exec(t.Context(), `update stays set status = 'voided', voided_at = $2 where id = $1`, stayID, voidedAt); err != nil {
		t.Fatal(err)
	}
	reopenAt := now.Add(3 * time.Minute)
	run(func(ctx context.Context) error {
		if err := repo.RefreshAccepted(ctx, lotID, stayID, reopenAt); err == nil {
			t.Error("RefreshAccepted on a voided stay succeeded")
		}
		return nil
	})
	run(func(ctx context.Context) error { return repo.ReopenReservationStay(ctx, lotID, stayID, reopenAt) })
	run(func(ctx context.Context) error {
		if err := repo.ReopenReservationStay(ctx, lotID, stayID, reopenAt); err == nil {
			t.Error("reopening an entry_pending stay succeeded")
		}
		return nil
	})

	var status string
	var accepted, voided time.Time
	if err := db.App.QueryRow(t.Context(), `select status, accepted_at, voided_at from stays where id = $1`, stayID).Scan(&status, &accepted, &voided); err != nil {
		t.Fatal(err)
	}
	if status != "entry_pending" || !accepted.Equal(reopenAt) || !voided.Equal(voidedAt) {
		t.Errorf("stay = %s accepted %v voided %v; want entry_pending at %v, voided_at kept", status, accepted, voided, reopenAt)
	}
}
