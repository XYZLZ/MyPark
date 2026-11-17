package gate_test

import (
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/XYZLZ/MyPark/apps/server/internal/devicefleet"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/gate"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/database/dbgen"
	"github.com/XYZLZ/MyPark/apps/server/internal/testsupport"
)

// The gate queries are scoped by lot (M2C-21) and the totals count only
// succeeded payments.
func TestGateQueries_LotScopedAndTotals(t *testing.T) {
	db := testsupport.NewTestDB(t)
	lotID := testsupport.SeedPilotLot(t, db)
	q := dbgen.New(db.App)
	ctx := t.Context()
	if _, err := gate.SeedCards(ctx, q, lotID, testFleet, testPepper); err != nil {
		t.Fatal(err)
	}
	hash := func(code string) []byte { return gate.HashCardUID(testPepper, devicefleet.CardUID(testFleet, code)) }

	card, err := q.GetActiveCardByHash(ctx, dbgen.GetActiveCardByHashParams{LotID: lotID, Hash: hash("EMP-007")})
	if err != nil {
		t.Fatalf("active card: %v", err)
	}
	if _, err := q.GetActiveCardByHash(ctx, dbgen.GetActiveCardByHashParams{LotID: lotID, Hash: hash("EMP-031")}); !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("revoked card: %v, want no rows", err)
	}
	if _, err := q.GetActiveCardByHash(ctx, dbgen.GetActiveCardByHashParams{LotID: lotID + 1, Hash: hash("EMP-007")}); !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("card of another lot: %v, want no rows", err)
	}

	var tariffID int64
	if err := db.App.QueryRow(ctx, "select id from tariff_versions where lot_id = $1", lotID).Scan(&tariffID); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 14, 10, 0, 0, 0, time.UTC)
	ts := func(t time.Time) pgtype.Timestamptz { return pgtype.Timestamptz{Time: t, Valid: true} }
	stay, err := q.InsertStay(ctx, dbgen.InsertStayParams{
		LotID: lotID, CredentialKind: "employee_card", EmployeeCardID: pgtype.Int8{Int64: card.ID, Valid: true},
		Status: "active", TariffVersionID: tariffID, Now: ts(now), EnteredAt: ts(now),
	})
	if err != nil {
		t.Fatalf("InsertStay: %v", err)
	}
	if got, err := q.GetStayForUpdate(ctx, dbgen.GetStayForUpdateParams{LotID: lotID, PublicID: stay.PublicID}); err != nil || got.ID != stay.ID {
		t.Errorf("GetStayForUpdate = %v, %v", got.ID, err)
	}
	if _, err := q.GetStayForUpdate(ctx, dbgen.GetStayForUpdateParams{LotID: lotID + 1, PublicID: stay.PublicID}); !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("stay of another lot: %v, want no rows", err)
	}
	if got, err := q.GetOpenStayByCardForUpdate(ctx, dbgen.GetOpenStayByCardForUpdateParams{LotID: lotID, EmployeeCardID: pgtype.Int8{Int64: card.ID, Valid: true}}); err != nil || got.ID != stay.ID {
		t.Errorf("GetOpenStayByCardForUpdate = %v, %v", got.ID, err)
	}

	totals, err := q.PaymentTotals(ctx, dbgen.PaymentTotalsParams{LotID: lotID, StayID: pgtype.Int8{Int64: stay.ID, Valid: true}})
	if err != nil || totals.PaidCents != 0 || totals.LastSucceededAt.Valid {
		t.Fatalf("empty totals = %+v, %v", totals, err)
	}
	pay := func(amount int64, status, key string, at time.Time) {
		t.Helper()
		if _, err := q.InsertPayment(ctx, dbgen.InsertPaymentParams{
			Kind: "parking_fee", StayID: pgtype.Int8{Int64: stay.ID, Valid: true}, Method: "cash", AmountCents: amount, Status: status,
			IdempotencyKey: key, Now: ts(now), SucceededAt: ts(at),
		}); err != nil {
			t.Fatalf("InsertPayment %s: %v", key, err)
		}
	}
	pay(5000, "succeeded", "a", now.Add(time.Minute))
	pay(2500, "succeeded", "b", now.Add(2*time.Minute))
	pay(9999, "pending", "c", now.Add(3*time.Minute))
	totals, err = q.PaymentTotals(ctx, dbgen.PaymentTotalsParams{LotID: lotID, StayID: pgtype.Int8{Int64: stay.ID, Valid: true}})
	if err != nil || totals.PaidCents != 7500 || !totals.LastSucceededAt.Time.Equal(now.Add(2*time.Minute)) {
		t.Errorf("totals = %+v, %v, want 7500 at +2m", totals, err)
	}
	if other, err := q.PaymentTotals(ctx, dbgen.PaymentTotalsParams{LotID: lotID + 1, StayID: pgtype.Int8{Int64: stay.ID, Valid: true}}); err != nil || other.PaidCents != 0 {
		t.Errorf("totals through another lot = %+v, %v, want 0", other, err)
	}

	// Ticket lookup.
	tk := []byte("ticket-hash-32-bytes-long-123456")
	tstay, err := q.InsertStay(ctx, dbgen.InsertStayParams{
		LotID: lotID, CredentialKind: "ticket", TicketCodeHash: tk, Status: "entry_pending", TariffVersionID: tariffID, Now: ts(now),
	})
	if err != nil {
		t.Fatalf("InsertStay ticket: %v", err)
	}
	if got, err := q.GetStayByTicketHashForUpdate(ctx, dbgen.GetStayByTicketHashForUpdateParams{LotID: lotID, TicketCodeHash: tk}); err != nil || got.ID != tstay.ID {
		t.Errorf("GetStayByTicketHashForUpdate = %v, %v", got.ID, err)
	}
}
