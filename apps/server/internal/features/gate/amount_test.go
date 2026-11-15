package gate_test

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/XYZLZ/MyPark/apps/server/internal/domain/authz"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/gate"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/tariffs"
	"github.com/XYZLZ/MyPark/apps/server/internal/testsupport"
)

func TestReceiptNo(t *testing.T) {
	for id, want := range map[int64]string{1: "MP-00000001", 42: "MP-00000042", 99999999: "MP-99999999", 123456789: "MP-123456789"} {
		if got := gate.ReceiptNo(id); got != want {
			t.Errorf("ReceiptNo(%d) = %q, want %q", id, got, want)
		}
	}
}

// scanRepo is just enough of a gate.Repo for a scan of one ticket stay; any
// other method panics through the nil embedded interface.
type scanRepo struct {
	gate.Repo
	stay gate.Stay
}

func (r *scanRepo) StayByTicketHashForUpdate(context.Context, int64, []byte) (gate.Stay, error) {
	return r.stay, nil
}

func (r *scanRepo) PaymentTotals(context.Context, int64, int64) (int64, error) { return 0, nil }

type fixedTariff struct{}

func (fixedTariff) AtTime(context.Context, int64, time.Time) (tariffs.Version, error) {
	return tariffs.Version{}, nil
}

func (fixedTariff) ByID(context.Context, int64) (tariffs.Version, error) {
	return tariffs.Version{ID: 1, Tariff: tariffs.Tariff{
		FirstHour: 5000, AdditionalHour: 2500, DailyCap: 40000, LostTicket: 50000,
		ExitGrace: 10 * time.Minute, PostPaymentExit: 15 * time.Minute,
	}}, nil
}

// An overstay is billed from stays.paid_at. A stay with an exit deadline but
// no paid_at would bill from the year 1, so pricing it is an error (500,
// nothing stored), never an amount.
func TestScan_DeadlineWithoutPaidAtIsAnError(t *testing.T) {
	now := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	stay := gate.Stay{
		ID: 1, PublicID: uuid.New(), LotID: 1, CredentialKind: gate.KindTicket, Status: gate.StatusActive,
		TariffVersionID: 1, EnteredAt: now.Add(-3 * time.Hour), ExitDeadlineAt: now.Add(-time.Hour),
	}
	svc := gate.NewService(gate.Deps{
		Repo: &scanRepo{stay: stay}, Clock: testsupport.NewFakeClock(now), Tariffs: fixedTariff{},
		Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	ev := gate.Event{ID: uuid.New(), Type: gate.EventPayScan,
		Payload: gate.PayScan{Credential: gate.Credential{Kind: gate.KindTicket, Value: "7K3M9QZX0ABC1DEF"}}}
	res, err := svc.Handle(t.Context(), authz.Device{LotID: 1}, ev)
	if err == nil {
		t.Fatalf("scan = %+v (amount %v), want an error", res, res.AmountCents)
	}
	if res != (gate.Result{}) {
		t.Errorf("result = %+v, want none with the error", res)
	}

	// With paid_at the same stay prices its segment from it.
	stay.PaidAt = now.Add(-30 * time.Minute)
	svc = gate.NewService(gate.Deps{
		Repo: &scanRepo{stay: stay}, Clock: testsupport.NewFakeClock(now), Tariffs: fixedTariff{},
		Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	res, err = svc.Handle(t.Context(), authz.Device{LotID: 1}, ev)
	if err != nil || res.AmountCents == nil || *res.AmountCents != 5000 || res.Breakdown.BilledFrom == nil || !res.Breakdown.BilledFrom.Equal(stay.PaidAt) {
		t.Fatalf("scan = %+v, %v; want 5000 billed from paid_at", res, err)
	}
}
