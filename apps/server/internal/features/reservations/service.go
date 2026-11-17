package reservations

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/XYZLZ/MyPark/apps/server/internal/domain/apperr"
	"github.com/XYZLZ/MyPark/apps/server/internal/domain/errcodes"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/notifications"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/payments"
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

	// The payment side. They stay nil in tests that never pay or apply.
	Fees          payments.Fees
	Gateway       payments.Gateway
	Notify        notifications.Notifier
	QRKey         []byte        // signs QR tokens (QR_HMAC_KEY)
	ChargeTimeout time.Duration // PAYMENT_CHARGE_TIMEOUT
	Rand          io.Reader     // QR nonces; nil means crypto/rand
}

// Service holds the reservation use cases.
type Service struct {
	tx      database.TxRunnerPort
	repo    Repo
	tariffs tariffs.Current
	avail   Availability
	clock   clock.Clock
	log     *slog.Logger

	fees          payments.Fees
	gateway       payments.Gateway
	notify        notifications.Notifier
	qrKey         []byte
	chargeTimeout time.Duration
	rand          io.Reader
}

// NewService returns a Service.
func NewService(d Deps) *Service {
	if d.Rand == nil {
		d.Rand = rand.Reader
	}
	return &Service{
		tx: d.Tx, repo: d.Repo, tariffs: d.Tariffs, avail: d.Avail, clock: d.Clock, log: d.Log,
		fees: d.Fees, gateway: d.Gateway, notify: d.Notify, qrKey: d.QRKey, chargeTimeout: d.ChargeTimeout, rand: d.Rand,
	}
}

var _ payments.OutcomeApplier = (*Service)(nil)

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

// View is the reservation representation of design 7.1 without the payment,
// refund and stay parts, which arrive with the payment slices (the API shows
// them as null until then).
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

	ConfirmedAt  *time.Time
	CancelledAt  *time.Time
	CancelledBy  *string
	RefundReason *string
	CheckedInAt  *time.Time
	CompletedAt  *time.Time
	ExpiredAt    *time.Time

	// LatestPayment is the newest fee attempt, nil before the first one. It is
	// filled by the read paths (Get, List, the pay answer), not by the repo.
	LatestPayment *PaymentRef
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

// Quote runs the create checks and prices the reservation. It writes nothing,
// but runs in a transaction with the same timeouts as create (M3B-55) so the
// vehicle share lock cannot wait without limit. Its capacity answer comes
// from an unlocked slot read (M3B-57).
func (s *Service) Quote(ctx context.Context, userID int64, in QuoteInput) (QuoteResult, error) {
	var out QuoteResult
	err := s.tx.InTx(ctx, func(ctx context.Context) error {
		if err := s.repo.SetTxLimits(ctx); err != nil {
			return err
		}
		c, err := s.check(ctx, userID, in.LotID, in.VehicleID, in.ArrivalAt, s.clock.Now())
		if err != nil {
			return err
		}
		held, err := s.repo.SlotLoads(ctx, c.lot.ID, c.window)
		if err != nil {
			return err
		}
		if CapReached(held, c.capacity()) {
			return apperr.Conflict(errcodes.ReservationCapacityReached)
		}
		out = QuoteResult{
			LotID: c.lot.PublicID, ArrivalAt: c.arrival, Window: c.window,
			FeeCents: c.fee, PaymentTimeoutMin: c.policy.PaymentTimeoutMin,
		}
		return nil
	})
	return out, err
}

// PaymentRef is the newest fee attempt of a reservation as clients see it.
type PaymentRef struct {
	ID          uuid.UUID
	Status      string
	FailureCode string // set when failed
}

// withLatestPayment fills v.LatestPayment inside the transaction of ctx. A
// service built without a payments port leaves it nil.
func (s *Service) withLatestPayment(ctx context.Context, reservationRowID int64, v *View) error {
	if s.fees == nil {
		return nil
	}
	fee, found, err := s.fees.LatestFee(ctx, reservationRowID)
	if err != nil || !found {
		return err
	}
	v.LatestPayment = &PaymentRef{ID: fee.PublicID, Status: fee.Status, FailureCode: fee.FailureCode}
	return nil
}
