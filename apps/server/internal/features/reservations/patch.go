package reservations

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/XYZLZ/MyPark/apps/server/internal/domain/apperr"
	"github.com/XYZLZ/MyPark/apps/server/internal/domain/errcodes"
)

// ChangeVehicle points a confirmed reservation at another of the driver's
// vehicles, before its arrival (FR-RES-16, design 7.7). It locks the
// reservation, then the new vehicle FOR SHARE. The same vehicle is a no-op
// that writes nothing (M3B-57). Fee, window and QR do not change.
func (s *Service) ChangeVehicle(ctx context.Context, userID int64, id, vehicleID uuid.UUID) (View, error) {
	var out View
	err := s.tx.InTx(ctx, func(ctx context.Context) error {
		if err := s.repo.SetTxLimits(ctx); err != nil {
			return err
		}
		rec, err := s.repo.Lock(ctx, userID, id)
		if errors.Is(err, ErrNotFound) {
			return apperr.NotFound(errcodes.ReservationNotFound)
		}
		if err != nil {
			return err
		}
		if rec.Status != StatusConfirmed {
			return apperr.Conflict(errcodes.ReservationInvalidState).
				WithDetails(map[string]any{"status": string(rec.Status)})
		}
		now := s.clock.Now()
		if !now.Before(rec.ArrivalAt) {
			return apperr.Conflict(errcodes.ReservationVehicleChangeClosed)
		}
		v, err := s.repo.VehicleForShare(ctx, userID, vehicleID)
		if errors.Is(err, ErrNotFound) {
			return apperr.NotFound(errcodes.VehicleNotFound)
		}
		if err != nil {
			return err
		}
		out = rec.View
		if v.ID == rec.VehicleID {
			return nil
		}
		if err := s.repo.SetVehicle(ctx, rec.RowID, v.ID, now); err != nil {
			return err
		}
		out.Vehicle = v
		return nil
	})
	return out, err
}
