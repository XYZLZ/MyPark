package reservations

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/XYZLZ/MyPark/apps/server/internal/domain/apperr"
	"github.com/XYZLZ/MyPark/apps/server/internal/domain/errcodes"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/tariffs"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/clock"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/database"
)

// Deps are the Service's dependencies.
type Deps struct {
	Tx      database.TxRunnerPort
	Repo    Repo
	Tariffs tariffs.Current
	Avail   Availability
	Clock   clock.Clock
	Log     *slog.Logger
}

// Service holds the reservation use cases.
type Service struct {
	tx      database.TxRunnerPort
	repo    Repo
	tariffs tariffs.Current
	avail   Availability
	clock   clock.Clock
	log     *slog.Logger
}

// NewService returns a Service.
func NewService(d Deps) *Service {
	return &Service{tx: d.Tx, repo: d.Repo, tariffs: d.Tariffs, avail: d.Avail, clock: d.Clock, log: d.Log}
}

// QuoteInput is a quote request.
type QuoteInput struct {
	LotID     uuid.UUID
	ArrivalAt time.Time
	VehicleID *uuid.UUID
}

// QuoteResult is the quote of a reservation (design 7.2).
type QuoteResult struct {
	LotID             uuid.UUID
	ArrivalAt         time.Time
	Window            Window
	FeeCents          int64
	PaymentTimeoutMin int
}

// CreateInput is a create request.
type CreateInput struct {
	LotID, VehicleID uuid.UUID
	ArrivalAt        time.Time
}

// View is the reservation representation of design 7.1. The vehicle comes
// from the creation; the payment, refund and stay parts arrive with the read
// endpoints and are absent from the API view until then.
type View struct {
	ID           uuid.UUID
	LotID        uuid.UUID
	Vehicle      VehicleRef
	ArrivalAt    time.Time
	Window       Window
	Status       Status
	FeeCents     int64
	PaymentDueAt time.Time
	CreatedAt    time.Time
}

// checked is the outcome of the checks the quote and the create share.
type checked struct {
	lot     Lot
	vehicle VehicleRef
	arrival time.Time
	window  Window
	policy  Policy
	fee     int64
	tariff  int64
}

// capacity is the reservable cap of the lot (design 8.3).
func (c checked) capacity() int {
	return Cap(c.policy.MaxReservablePct, c.lot.Capacity-c.lot.OutOfService)
}

// check runs design 7.3 checks 1 to 8 in order. A nil vehicle skips check 2
// (a quote without vehicle_id). The policy is read before check 5 because the
// booking horizon comes from it; its own failures surface at check 6.
func (s *Service) check(ctx context.Context, userID int64, lotID uuid.UUID, vehicleID *uuid.UUID, rawArrival, now time.Time) (checked, error) {
	var c checked
	lot, err := s.repo.LotForReservation(ctx, lotID)
	if errors.Is(err, ErrNotFound) {
		return c, apperr.NotFound(errcodes.LotNotFound)
	}
	if err != nil {
		return c, err
	}
	c.lot = lot

	if vehicleID != nil {
		v, err := s.repo.VehicleForShare(ctx, userID, *vehicleID)
		if errors.Is(err, ErrNotFound) {
			return c, apperr.NotFound(errcodes.VehicleNotFound)
		}
		if err != nil {
			return c, err
		}
		c.vehicle = v
	}

	until, err := s.repo.BlockedUntil(ctx, userID)
	if err != nil {
		return c, err
	}
	if until.After(now) {
		return c, apperr.Conflict(errcodes.ReservationBlockedNoShows).
			WithDetails(map[string]any{"blocked_until": until.UTC()})
	}
	if lot.Closed {
		return c, apperr.Conflict(errcodes.ReservationLotClosed)
	}

	policy, err := s.repo.PolicyAt(ctx, lot.ID, now)
	if err != nil {
		return c, fmt.Errorf("reservations: policy of lot %d: %w", lot.ID, err)
	}
	c.policy = policy
	arrival, code := NormalizeArrival(rawArrival, now, policy.BookingHorizonDays)
	switch code {
	case errcodes.ReservationArrivalInPast, errcodes.ReservationBeyondHorizon:
		return c, apperr.Invalid(code)
	}
	c.arrival = arrival
	c.window = HoldWindow(arrival, policy)

	if err := CheckPhase1(policy); err != nil {
		s.log.ErrorContext(ctx, "reservations: policy unsupported", "lot_id", lot.ID, "err", err)
		return c, err
	}
	tv, err := s.tariffs.AtTime(ctx, lot.ID, now)
	if err != nil {
		return c, fmt.Errorf("reservations: tariff of lot %d: %w", lot.ID, err)
	}
	if tv.Tariff.ReservationFee <= 0 {
		err := fmt.Errorf("reservations: tariff version %d has reservation fee %d, want > 0 (M3B-49)", tv.ID, tv.Tariff.ReservationFee)
		s.log.ErrorContext(ctx, "reservations: tariff unusable", "lot_id", lot.ID, "err", err)
		return c, err
	}
	c.fee, c.tariff = tv.Tariff.ReservationFee, tv.ID

	active, err := s.repo.HasActive(ctx, userID)
	if err != nil {
		return c, err
	}
	if active {
		return c, apperr.Conflict(errcodes.ReservationActiveExists)
	}

	if !c.window.Start.After(now) {
		snap, err := s.avail.Snapshot(ctx, lot.ID)
		if err != nil {
			return c, err
		}
		if snap.Available < 1 {
			return c, apperr.Conflict(errcodes.ReservationNoSpaceNow)
		}
	}
	return c, nil
}

// Quote runs the create checks read-only and prices the reservation. Its
// capacity answer comes from an unlocked slot read (M3B-57).
func (s *Service) Quote(ctx context.Context, userID int64, in QuoteInput) (QuoteResult, error) {
	c, err := s.check(ctx, userID, in.LotID, in.VehicleID, in.ArrivalAt, s.clock.Now())
	if err != nil {
		return QuoteResult{}, err
	}
	held, err := s.repo.SlotLoads(ctx, c.lot.ID, c.window)
	if err != nil {
		return QuoteResult{}, err
	}
	if CapReached(held, c.capacity()) {
		return QuoteResult{}, apperr.Conflict(errcodes.ReservationCapacityReached)
	}
	return QuoteResult{
		LotID: c.lot.PublicID, ArrivalAt: c.arrival, Window: c.window,
		FeeCents: c.fee, PaymentTimeoutMin: c.policy.PaymentTimeoutMin,
	}, nil
}
