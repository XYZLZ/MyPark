package tariffs

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/database/dbgen"
)

// SeedDefaults inserts the pilot lot's first tariff (DefaultTariff) and
// booking policy, both effective from the tariff's EffectiveFrom. It is
// idempotent: a version already stored for that instant is left untouched.
func SeedDefaults(ctx context.Context, q *dbgen.Queries, lotID int64) error {
	t := DefaultTariff()
	from := pgtype.Timestamptz{Time: t.EffectiveFrom, Valid: true}
	err := q.InsertTariffVersion(ctx, dbgen.InsertTariffVersionParams{
		LotID:               lotID,
		EffectiveFrom:       from,
		FirstHourCents:      t.FirstHour,
		AdditionalHourCents: t.AdditionalHour,
		ExitGraceMin:        minutes(t.ExitGrace),
		PostPaymentExitMin:  minutes(t.PostPaymentExit),
		DailyCapCents:       t.DailyCap,
		LostTicketCents:     t.LostTicket,
		ReservationFeeCents: t.ReservationFee,
	})
	if err != nil {
		return fmt.Errorf("seed: insert tariff version: %w", err)
	}
	// Policy values are design §5.1 (spec §5.3); M3 consumes them.
	err = q.InsertPolicyVersion(ctx, dbgen.InsertPolicyVersionParams{
		LotID:                lotID,
		EffectiveFrom:        from,
		MaxReservablePct:     30,
		BookingHorizonDays:   7,
		ArrivalGraceMin:      20,
		EarlyArrivalMin:      15,
		PaymentTimeoutMin:    10,
		MaxActivePerUser:     1,
		SlotMin:              5,
		NoshowBlockThreshold: 3,
		NoshowWindowDays:     30,
		NoshowBlockDays:      7,
	})
	if err != nil {
		return fmt.Errorf("seed: insert policy version: %w", err)
	}
	return nil
}

// minutes converts a whole-minute duration to the int32 the columns hold.
func minutes(d time.Duration) int32 { return int32(d / time.Minute) }
