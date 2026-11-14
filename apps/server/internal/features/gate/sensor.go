package gate

import (
	"context"
	"fmt"

	"github.com/XYZLZ/MyPark/apps/server/internal/domain/authz"
	"github.com/XYZLZ/MyPark/apps/server/internal/domain/errcodes"
)

// Space states a sensor reports.
const (
	SpaceFree     = "free"
	SpaceOccupied = "occupied"
)

// sensor applies a sensor.space_changed event (design §8.3). Only a real
// change writes and notifies; the lot occupancy is untouched (it counts
// stays, not sensors). An unknown code is a business refusal.
func (s *Service) sensor(ctx context.Context, d authz.Device, ev Event) (Result, error) {
	p, ok := ev.Payload.(SensorChanged)
	if !ok {
		return Result{}, fmt.Errorf("gate: sensor event carries %T", ev.Payload)
	}
	state := SpaceFree
	if p.Occupied {
		state = SpaceOccupied
	}

	changed, err := s.repo.UpdateSpaceFromSensor(ctx, d.LotID, p.SpaceCode, state, s.clock.Now())
	if err != nil {
		return Result{}, err
	}
	if changed {
		if err := s.repo.NotifySpaceChanged(ctx, d.LotID, p.SpaceCode, state); err != nil {
			return Result{}, err
		}
		return Result{Result: OutcomeAck}, nil
	}

	exists, err := s.repo.SpaceExists(ctx, d.LotID, p.SpaceCode)
	if err != nil {
		return Result{}, err
	}
	if !exists {
		return Result{Result: OutcomeRejected, Reason: string(errcodes.LotSpaceUnknown)}, nil
	}
	return Result{Result: OutcomeAck}, nil
}
