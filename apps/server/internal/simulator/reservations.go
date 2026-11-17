package simulator

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/XYZLZ/MyPark/apps/server/internal/domain/errcodes"
	"github.com/XYZLZ/MyPark/apps/server/internal/domain/secret"
)

// DriverAPI is the part of the public driver API the reserved-car scenarios
// use. *DriverClient implements it.
type DriverAPI interface {
	Login(ctx context.Context, n int) error
	Vehicle(ctx context.Context, n int) (uuid.UUID, error)
	Quote(ctx context.Context, n int, arrival time.Time) (Quote, error)
	Create(ctx context.Context, n int, arrival time.Time, vehicleID uuid.UUID) (Reservation, error)
	Pay(ctx context.Context, n int, reservationID uuid.UUID, key string) (string, error)
	QR(ctx context.Context, n int, reservationID uuid.UUID) (secret.String, error)
}

const (
	// A blocked driver (no-show strikes) is left alone for a while; a driver
	// with an active reservation is busy until its car has gone.
	blockedFor = time.Hour
	busyFor    = 10 * time.Minute
	slotStep   = 5 * time.Minute
)

// errDriverSkipped marks a driver the API refused for a reason that lifts
// with time; Book moves on to the next one.
var errDriverSkipped = errors.New("simulator: driver skipped")

// Reserver books and pays reservations as the simulated drivers (design
// §13.2). It draws no random numbers: the drivers go round-robin and the
// server's answers never reach the engine's generator (FR-SIM-7).
type Reserver struct {
	api DriverAPI
	// now is the server's clock (wall time): windows are server time.
	now func() time.Time
	log *slog.Logger

	arrivals, noShow DriverPool

	mu       sync.Mutex
	vehicles map[int]uuid.UUID
	skipped  map[int]time.Time // driver -> skipped until
}

// NewReserver returns a Reserver over api. A nil log discards.
func NewReserver(api DriverAPI, now func() time.Time, log *slog.Logger) *Reserver {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Reserver{
		api: api, now: now, log: log,
		arrivals: DriverPool{Range: PoolArrivals}, noShow: DriverPool{Range: PoolNoShow},
		vehicles: map[int]uuid.UUID{}, skipped: map[int]time.Time{},
	}
}

// ceil5 is the next 5-minute UTC boundary, or t when already aligned (the
// server's arrival grid).
func ceil5(t time.Time) time.Time {
	t = t.UTC()
	floor := t.Truncate(slotStep)
	if floor.Equal(t) {
		return floor
	}
	return floor.Add(slotStep)
}

func (r *Reserver) isSkipped(n int) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.now().Before(r.skipped[n])
}

func (r *Reserver) skip(n int, d time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.skipped[n] = r.now().Add(d)
}

// Book runs quote, create (arrival = ceil5 of the server's now) and pay for
// the next free driver of the scenario's pool. For reservation_arrivals it
// then fetches the QR and returns the car that will present it; for no_show it
// stops after the payment and returns a car that is never sent. It returns
// false when no reservation was made: every driver is skipped, or the API
// refused a step. It never loops past one pass over the pool.
func (r *Reserver) Book(ctx context.Context, s Scenario) (*Car, bool) {
	pool := &r.arrivals
	if s == ScenarioNoShow {
		pool = &r.noShow
	}
	for {
		n, ok := pool.Next(r.isSkipped)
		if !ok {
			return nil, false
		}
		car, err := r.book(ctx, n, s)
		switch {
		case err == nil:
			return car, true
		case errors.Is(err, errDriverSkipped):
			continue // marked: the next pass skips it, so this ends
		}
		r.log.Debug("reserved car not booked", "driver", n, "scenario", s, "error", err.Error())
		return nil, false
	}
}

func (r *Reserver) book(ctx context.Context, n int, s Scenario) (*Car, error) {
	vehicle, err := r.vehicle(ctx, n)
	if err != nil {
		return nil, err
	}
	arrival := ceil5(r.now())
	if _, err := r.api.Quote(ctx, n, arrival); err != nil {
		return nil, err
	}
	res, err := r.api.Create(ctx, n, arrival, vehicle)
	if err != nil {
		if apiErr, ok := errors.AsType[*DriverAPIError](err); ok {
			switch apiErr.Code {
			case string(errcodes.ReservationBlockedNoShows):
				r.skip(n, blockedFor)
				return nil, errDriverSkipped
			case string(errcodes.ReservationActiveExists):
				r.skip(n, busyFor)
				return nil, errDriverSkipped
			}
		}
		return nil, err
	}
	status, err := r.api.Pay(ctx, n, res.ID, uuid.NewString())
	if err != nil {
		return nil, err
	}
	if s == ScenarioNoShow {
		return &Car{Cred: CredReservationQR}, nil
	}
	if status != "confirmed" {
		return nil, errors.New("simulator: payment not confirmed yet")
	}
	token, err := r.api.QR(ctx, n, res.ID)
	if err != nil {
		return nil, err
	}
	return &Car{Cred: CredReservationQR, QR: token}, nil
}

func (r *Reserver) vehicle(ctx context.Context, n int) (uuid.UUID, error) {
	r.mu.Lock()
	id, ok := r.vehicles[n]
	r.mu.Unlock()
	if ok {
		return id, nil
	}
	id, err := r.api.Vehicle(ctx, n)
	if err != nil {
		return uuid.Nil, err
	}
	r.mu.Lock()
	r.vehicles[n] = id
	r.mu.Unlock()
	return id, nil
}
