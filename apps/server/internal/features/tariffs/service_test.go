package tariffs_test

import (
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/XYZLZ/MyPark/apps/server/internal/features/tariffs"
	tariffspg "github.com/XYZLZ/MyPark/apps/server/internal/features/tariffs/postgres"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/database/dbgen"
	"github.com/XYZLZ/MyPark/apps/server/internal/testsupport"
)

var ast = time.FixedZone("AST", -4*60*60)

func TestSeedDefaults_Idempotent(t *testing.T) {
	db := testsupport.NewTestDB(t)
	lotID := testsupport.SeedPilotLot(t, db)
	q := dbgen.New(db.App)
	if err := tariffs.SeedDefaults(t.Context(), q, lotID); err != nil {
		t.Fatalf("second SeedDefaults: %v", err)
	}

	var tariffRows, policyRows int
	if err := db.App.QueryRow(t.Context(), "select count(*) from tariff_versions").Scan(&tariffRows); err != nil {
		t.Fatal(err)
	}
	if err := db.App.QueryRow(t.Context(), "select count(*) from policy_versions").Scan(&policyRows); err != nil {
		t.Fatal(err)
	}
	if tariffRows != 1 || policyRows != 1 {
		t.Fatalf("tariff rows = %d, policy rows = %d, want 1 and 1", tariffRows, policyRows)
	}

	// The seeded row is the S2.1 default (design §12.3), value for value.
	var got [7]int64
	err := db.App.QueryRow(t.Context(), `select first_hour_cents, additional_hour_cents, exit_grace_min,
		post_payment_exit_min, daily_cap_cents, lost_ticket_cents, reservation_fee_cents from tariff_versions`).
		Scan(&got[0], &got[1], &got[2], &got[3], &got[4], &got[5], &got[6])
	if err != nil {
		t.Fatal(err)
	}
	if want := [7]int64{5000, 2500, 10, 15, 40000, 50000, 15000}; got != want {
		t.Errorf("seeded tariff = %v, want %v", got, want)
	}

	v, err := tariffs.NewService(tariffspg.NewRepo(db.App)).AtTime(t.Context(), lotID, tariffs.DefaultTariff().EffectiveFrom)
	if err != nil {
		t.Fatalf("AtTime: %v", err)
	}
	want := tariffs.DefaultTariff()
	if !v.Tariff.EffectiveFrom.Equal(want.EffectiveFrom) {
		t.Errorf("effective_from = %v, want %v", v.Tariff.EffectiveFrom, want.EffectiveFrom)
	}
	v.Tariff.EffectiveFrom, want.EffectiveFrom = time.Time{}, time.Time{}
	if v.Tariff != want {
		t.Errorf("seeded tariff = %+v, want DefaultTariff %+v", v.Tariff, want)
	}

	var pol [10]int64
	err = db.App.QueryRow(t.Context(), `select max_reservable_pct, booking_horizon_days, arrival_grace_min,
		early_arrival_min, payment_timeout_min, max_active_per_user, slot_min, noshow_block_threshold,
		noshow_window_days, noshow_block_days from policy_versions`).
		Scan(&pol[0], &pol[1], &pol[2], &pol[3], &pol[4], &pol[5], &pol[6], &pol[7], &pol[8], &pol[9])
	if err != nil {
		t.Fatal(err)
	}
	if want := [10]int64{30, 7, 20, 15, 10, 1, 5, 3, 30, 7}; pol != want {
		t.Errorf("seeded policy = %v, want %v", pol, want)
	}
}

func TestCurrent_AtTimePicksLatestEffective(t *testing.T) {
	db := testsupport.NewTestDB(t)
	lotID := testsupport.SeedPilotLot(t, db)
	dec := time.Date(2026, 12, 1, 0, 0, 0, 0, ast)
	err := dbgen.New(db.App).InsertTariffVersion(t.Context(), dbgen.InsertTariffVersionParams{
		LotID: lotID, EffectiveFrom: pgtype.Timestamptz{Time: dec, Valid: true},
		FirstHourCents: 6000, AdditionalHourCents: 3000, ExitGraceMin: 10, PostPaymentExitMin: 15,
		DailyCapCents: 45000, LostTicketCents: 55000, ReservationFeeCents: 16000,
	})
	if err != nil {
		t.Fatalf("insert second version: %v", err)
	}
	svc := tariffs.NewService(tariffspg.NewRepo(db.App))

	first, err := svc.AtTime(t.Context(), lotID, time.Date(2026, 11, 30, 12, 0, 0, 0, ast))
	if err != nil {
		t.Fatalf("AtTime(Nov 30): %v", err)
	}
	if first.Tariff.FirstHour != 5000 || first.LotID != lotID {
		t.Errorf("Nov 30 version = %+v, want the 5000 one for lot %d", first, lotID)
	}
	second, err := svc.AtTime(t.Context(), lotID, dec)
	if err != nil {
		t.Fatalf("AtTime(Dec 1): %v", err)
	}
	if second.Tariff.FirstHour != 6000 || second.ID == first.ID {
		t.Errorf("Dec 1 version = %+v, want the 6000 one", second)
	}
	byID, err := svc.ByID(t.Context(), first.ID)
	if err != nil || byID.Tariff.FirstHour != 5000 {
		t.Errorf("ByID(first) = %+v, %v", byID, err)
	}

	if _, err := svc.AtTime(t.Context(), lotID, time.Date(2026, 10, 31, 23, 59, 59, 0, ast)); !errors.Is(err, tariffs.ErrNoVersion) {
		t.Errorf("AtTime before every version: %v, want ErrNoVersion", err)
	}
	if _, err := svc.ByID(t.Context(), 999999); !errors.Is(err, tariffs.ErrNoVersion) {
		t.Errorf("ByID(unknown): %v, want ErrNoVersion", err)
	}
}
