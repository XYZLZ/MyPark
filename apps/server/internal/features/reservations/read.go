package reservations

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/XYZLZ/MyPark/apps/server/internal/domain/apperr"
	"github.com/XYZLZ/MyPark/apps/server/internal/domain/errcodes"
)

// Get returns one of the driver's reservations. An unknown id and another
// driver's id are the same 404 (M3B-33). It runs in a transaction with the
// driver-API limits, like every reservation path (M3B-55).
func (s *Service) Get(ctx context.Context, userID int64, id uuid.UUID) (View, error) {
	var out View
	err := s.tx.InTx(ctx, func(ctx context.Context) error {
		if err := s.repo.SetTxLimits(ctx); err != nil {
			return err
		}
		rec, err := s.repo.Get(ctx, userID, id)
		if errors.Is(err, ErrNotFound) {
			return apperr.NotFound(errcodes.ReservationNotFound)
		}
		out = rec.View
		return err
	})
	return out, err
}
