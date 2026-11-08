package vehicles

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/XYZLZ/MyPark/apps/server/internal/domain/apperr"
	"github.com/XYZLZ/MyPark/apps/server/internal/domain/authz"
	"github.com/XYZLZ/MyPark/apps/server/internal/domain/errcodes"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/clock"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/database"
)

// CreateInput is a new vehicle. Strings must already be trimmed.
type CreateInput struct {
	Plate, Make, Model, Color string
}

// UpdateInput is a partial update: a nil field is left unchanged.
type UpdateInput struct {
	Plate, Make, Model, Color *string
}

// Service holds the vehicle use cases. Every method takes the caller's
// Principal, re-asserts the driver role and scopes storage access to the
// caller's user id.
type Service struct {
	tx    database.TxRunnerPort
	repo  Repo
	rc    ReservationChecker
	max   int
	clock clock.Clock
}

// NewService returns a Service capping each driver at max active vehicles.
func NewService(tx database.TxRunnerPort, repo Repo, rc ReservationChecker, max int, clk clock.Clock) *Service {
	return &Service{tx: tx, repo: repo, rc: rc, max: max, clock: clk}
}

func requireDriver(p authz.Principal) error {
	if p.Role != authz.RoleDriver {
		return apperr.Forbidden(errcodes.AuthForbidden)
	}
	return nil
}

func notFound() error { return apperr.NotFound(errcodes.VehicleNotFound) }

func toVehicle(r Record) Vehicle { return r.Vehicle }

// mapRepoErr converts storage sentinels into API errors.
func mapRepoErr(err error) error {
	switch {
	case errors.Is(err, ErrNotFound):
		return notFound()
	case errors.Is(err, ErrPlateTaken):
		return apperr.Conflict(errcodes.VehiclePlateTaken)
	default:
		return err
	}
}

// List returns the caller's live vehicles, oldest first.
func (s *Service) List(ctx context.Context, p authz.Principal) ([]Vehicle, error) {
	if err := requireDriver(p); err != nil {
		return nil, err
	}
	rows, err := s.repo.ListOwned(ctx, p.UserID)
	if err != nil {
		return nil, err
	}
	out := make([]Vehicle, len(rows))
	for i, r := range rows {
		out[i] = toVehicle(r)
	}
	return out, nil
}

// Create registers a vehicle. The owner row is locked before counting, so
// concurrent creates cannot pass the cap.
func (s *Service) Create(ctx context.Context, p authz.Principal, in CreateInput) (Vehicle, error) {
	if err := requireDriver(p); err != nil {
		return Vehicle{}, err
	}
	norm, display, err := NormalizePlate(in.Plate)
	if err != nil {
		return Vehicle{}, err
	}
	f := Fields{PlateNormalized: norm, PlateDisplay: display, Make: in.Make, Model: in.Model, Color: in.Color}

	var out Vehicle
	err = s.tx.InTx(ctx, func(ctx context.Context) error {
		if err := s.repo.LockOwner(ctx, p.UserID); err != nil {
			return mapRepoErr(err)
		}
		n, err := s.repo.CountActiveOwned(ctx, p.UserID)
		if err != nil {
			return err
		}
		if LimitReached(n, s.max) {
			return apperr.Conflict(errcodes.VehicleLimitReached).WithDetails(map[string]any{"limit": s.max})
		}
		rec, err := s.repo.Insert(ctx, p.UserID, f, s.clock.Now())
		if err != nil {
			return mapRepoErr(err)
		}
		out = toVehicle(rec)
		return nil
	})
	return out, err
}

// Get returns one of the caller's vehicles.
func (s *Service) Get(ctx context.Context, p authz.Principal, id uuid.UUID) (Vehicle, error) {
	if err := requireDriver(p); err != nil {
		return Vehicle{}, err
	}
	rec, err := s.repo.GetOwned(ctx, p.UserID, id)
	if err != nil {
		return Vehicle{}, mapRepoErr(err)
	}
	return toVehicle(rec), nil
}

// Update applies a partial update to one of the caller's vehicles. A plate
// change is re-normalized and may fail with vehicle.plate_taken. The row is
// read locked, so a concurrent update of other fields is never overwritten.
func (s *Service) Update(ctx context.Context, p authz.Principal, id uuid.UUID, in UpdateInput) (Vehicle, error) {
	if err := requireDriver(p); err != nil {
		return Vehicle{}, err
	}
	var out Vehicle
	err := s.tx.InTx(ctx, func(ctx context.Context) error {
		cur, err := s.repo.GetOwnedForUpdate(ctx, p.UserID, id)
		if err != nil {
			return mapRepoErr(err)
		}
		f := Fields{
			PlateNormalized: cur.PlateNormalized, PlateDisplay: cur.Plate,
			Make: cur.Make, Model: cur.Model, Color: cur.Color,
		}
		if in.Plate != nil {
			if f.PlateNormalized, f.PlateDisplay, err = NormalizePlate(*in.Plate); err != nil {
				return err
			}
		}
		if in.Make != nil {
			f.Make = *in.Make
		}
		if in.Model != nil {
			f.Model = *in.Model
		}
		if in.Color != nil {
			f.Color = *in.Color
		}
		if in == (UpdateInput{}) {
			out = toVehicle(cur)
			return nil
		}
		rec, err := s.repo.UpdateOwned(ctx, p.UserID, id, f, s.clock.Now())
		if err != nil {
			return mapRepoErr(err)
		}
		out = toVehicle(rec)
		return nil
	})
	return out, err
}

// Delete soft-deletes one of the caller's vehicles, which frees its plate.
func (s *Service) Delete(ctx context.Context, p authz.Principal, id uuid.UUID) error {
	if err := requireDriver(p); err != nil {
		return err
	}
	return s.tx.InTx(ctx, func(ctx context.Context) error {
		rec, err := s.repo.GetOwnedForUpdate(ctx, p.UserID, id)
		if err != nil {
			return mapRepoErr(err)
		}
		busy, err := s.rc.HasActive(ctx, rec.ID)
		if err != nil {
			return fmt.Errorf("vehicles: check reservations: %w", err)
		}
		if busy {
			return apperr.Conflict(errcodes.VehicleInUse)
		}
		return mapRepoErr(s.repo.SoftDeleteOwned(ctx, p.UserID, id, s.clock.Now()))
	})
}

// EraseOwner soft-deletes and scrubs every live vehicle of ownerID, which
// frees their plates (spec 7.8). Account deletion calls it inside its own
// transaction; it takes no Principal because the caller is the deletion flow
// acting on the authenticated user.
func (s *Service) EraseOwner(ctx context.Context, ownerID int64) error {
	return s.repo.EraseOwner(ctx, ownerID, s.clock.Now())
}
