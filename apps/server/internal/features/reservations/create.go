package reservations

import (
	"context"
	"errors"
	"time"

	"github.com/XYZLZ/MyPark/apps/server/internal/domain/apperr"
	"github.com/XYZLZ/MyPark/apps/server/internal/domain/errcodes"
)

// Create books a space: design 7.3 checks 1 to 8, the reservation insert and
// the atomic slot-load cap, in one transaction. The index on one active
// reservation per driver is the guarantee behind check 7; the locked slot
// rows are the guarantee behind the cap. A capacity refusal rolls the insert
// back.
func (s *Service) Create(ctx context.Context, userID int64, in CreateInput) (View, error) {
	var out View
	err := s.tx.InTx(ctx, func(ctx context.Context) error {
		if err := s.repo.SetTxLimits(ctx); err != nil {
			return err
		}
		now := s.clock.Now()
		c, err := s.check(ctx, userID, in.LotID, &in.VehicleID, in.ArrivalAt, now)
		if err != nil {
			return err
		}
		row, err := s.repo.Insert(ctx, NewReservation{
			UserID: userID, VehicleID: c.vehicle.ID, LotID: c.lot.ID,
			Arrival: c.arrival, Window: c.window, FeeCents: c.fee,
			TariffVersionID: c.tariff, PolicyVersionID: c.policy.VersionID,
			PaymentDueAt: now.Add(time.Duration(c.policy.PaymentTimeoutMin) * time.Minute), Now: now,
		})
		if errors.Is(err, ErrActiveExists) {
			return apperr.Conflict(errcodes.ReservationActiveExists)
		}
		if err != nil {
			return err
		}
		held, err := s.repo.EnsureAndLockSlots(ctx, c.lot.ID, c.window)
		if err != nil {
			return err
		}
		if CapReached(held, c.capacity()) {
			return apperr.Conflict(errcodes.ReservationCapacityReached)
		}
		if err := s.repo.IncSlots(ctx, c.lot.ID, c.window); err != nil {
			return err
		}
		out = View{
			ID: row.PublicID, LotID: c.lot.PublicID, Vehicle: c.vehicle, ArrivalAt: row.Arrival,
			Window: c.window, Status: row.Status, FeeCents: row.FeeCents,
			PaymentDueAt: row.PaymentDueAt, CreatedAt: row.CreatedAt,
		}
		return nil
	})
	return out, err
}
