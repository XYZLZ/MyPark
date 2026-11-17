package gate

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/XYZLZ/MyPark/apps/server/internal/domain/authz"
	"github.com/XYZLZ/MyPark/apps/server/internal/domain/errcodes"
)

// qrEntry answers a reservation QR at the entry barrier (design 11.1). The
// token is checked before any lock; then, in the design 8.1 order: the lot,
// the reservation, its slot loads, its stay. The QR skips the public
// availability check (spec 5.2 rule 2), so effective holds are never read.
func (s *Service) qrEntry(ctx context.Context, d authz.Device, token string) (Result, error) {
	ref, err := s.reservations.Resolve(ctx, d.LotID, token)
	if errors.Is(err, ErrNotFound) {
		return deny(errcodes.GateCredentialInvalid), nil
	}
	if err != nil {
		return Result{}, err
	}
	lot, err := s.repo.LockLotOccupancy(ctx, d.LotID)
	if err != nil {
		return Result{}, err
	}
	r, err := s.reservations.LockForEntry(ctx, d.LotID, ref)
	if errors.Is(err, ErrNotFound) {
		return deny(errcodes.GateCredentialInvalid), nil
	}
	if err != nil {
		return Result{}, err
	}
	if lot.Closed {
		return deny(errcodes.GateLotClosed), nil
	}
	// The decision uses the time the locks were won.
	now := s.clock.Now()
	oos, err := s.repo.CountOutOfService(ctx, d.LotID)
	if err != nil {
		return Result{}, err
	}
	pending, err := s.repo.CountFreshPending(ctx, d.LotID, now)
	if err != nil {
		return Result{}, err
	}
	// Read without a row lock: stays come after slot loads in the lock order,
	// and the decision may need the slot loads. lockStay re-validates it.
	stay, err := s.repo.ReservationStay(ctx, d.LotID, r.ID)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return Result{}, err
	}
	stale := r.Status == reservationCheckedIn && stay.Status == StatusEntryPending && !now.Before(stay.AcceptedAt.Add(pendingWindow))
	if stale {
		if r, stay, err = s.voidStale(ctx, r, stay, now); err != nil {
			return Result{}, err
		}
	}
	dec := s.reservations.Decide(QRDecisionInput{
		Reservation: r, Now: now, Occupied: lot.Occupied, Pending: pending,
		EffectiveCapacity: lot.Capacity - oos, StayStatus: stay.Status,
		StayAcceptedAt: stay.AcceptedAt,
	})
	switch dec.Action {
	case QRDeny:
		if stale { // the restored hold is published once; the other outcomes bump
			return deny(dec.Reason), s.bumpVersion(ctx, d.LotID, now)
		}
		return deny(dec.Reason), nil
	case QRPhysicallyFull:
		if err := s.reservations.CancelUnadmitted(ctx, r, now); err != nil {
			return Result{}, err
		}
		return deny(errcodes.GateLotPhysicallyFull), s.bumpVersion(ctx, d.LotID, now)
	case QRCheckIn, QRReopen:
		return s.qrCheckIn(ctx, r, stay, now)
	case QRRefresh:
		if ok, err := s.lockStay(ctx, r, stay); err != nil || !ok {
			return deny(errcodes.GateCredentialInvalid), err
		}
		if err := s.repo.RefreshAccepted(ctx, d.LotID, stay.ID, now); err != nil {
			return Result{}, err
		}
		return Result{Result: OutcomeOpen, StayID: &stay.PublicID}, nil
	}
	return Result{}, fmt.Errorf("gate: qr decision %d", dec.Action)
}

// qrCheckIn checks the confirmed reservation in: a new stay, or its voided
// stay reused (M3B-24, M3B-60). The slot loads are locked before the stay
// and nothing is written until the stay is re-validated.
func (s *Service) qrCheckIn(ctx context.Context, r ReservationEntry, stay Stay, now time.Time) (Result, error) {
	if err := s.reservations.LockSlots(ctx, r); err != nil {
		return Result{}, err
	}
	if ok, err := s.lockStay(ctx, r, stay); err != nil || !ok {
		return deny(errcodes.GateCredentialInvalid), err
	}
	if err := s.reservations.CheckIn(ctx, r, now); err != nil {
		return Result{}, err
	}
	stayID := stay.PublicID
	if stay.ID == 0 {
		id, err := s.repo.InsertStay(ctx, NewStay{
			LotID: r.LotID, ReservationID: r.ID, TariffVersionID: r.TariffVersionID, AcceptedAt: now,
		})
		if err != nil {
			return Result{}, err
		}
		stayID = id
	} else {
		if !CanTransition(stay.Status, StatusEntryPending) {
			return Result{}, fmt.Errorf("gate: reservation stay %s is %s", stay.PublicID, stay.Status)
		}
		if err := s.repo.ReopenReservationStay(ctx, r.LotID, stay.ID, now); err != nil {
			return Result{}, err
		}
	}
	if err := s.bumpVersion(ctx, r.LotID, now); err != nil {
		return Result{}, err
	}
	return Result{Result: OutcomeOpen, StayID: &stayID}, nil
}

// lockStay locks the reservation's stay and reports whether it is still the
// one read before the decision (none, or the same row in the same status).
// Every writer of a reservation stay takes the lot lock first (the void job
// too, M3B-60), so this is a guard: a mismatch means it changed anyway.
func (s *Service) lockStay(ctx context.Context, r ReservationEntry, read Stay) (bool, error) {
	locked, err := s.repo.ReservationStayForUpdate(ctx, r.LotID, r.ID)
	if errors.Is(err, ErrNotFound) {
		return read.ID == 0, nil
	}
	if err != nil {
		return false, err
	}
	return locked.ID == read.ID && locked.Status == read.Status, nil
}

// bumpVersion publishes a change of effective holds at now (M3B-13): the
// caller holds the lot lock.
func (s *Service) bumpVersion(ctx context.Context, lotID int64, now time.Time) error {
	version, err := s.repo.BumpOccupancy(ctx, lotID, 0, now)
	if err != nil {
		return err
	}
	return s.repo.NotifyLotChanged(ctx, lotID, version)
}

// The reservation statuses the gate reads from ReservationEntry.Status.
const (
	reservationConfirmed = "confirmed"
	reservationCheckedIn = "checked_in"
)

// voidStale does the void job's work inline for a checked-in reservation
// whose stay is past its 60 s (M3B-24, M3B-60): the hold is restored without
// a cap check and the stay voided, so the decision then takes the
// confirmed path (reopen, physically full or window closed). The caller
// bumps the version once, whatever the outcome, and holds the lot and
// reservation locks; slot loads come before the stay.
// Nothing else can change the stay under the lot lock, so a mismatch is an
// error that rolls everything back.
func (s *Service) voidStale(ctx context.Context, r ReservationEntry, stay Stay, now time.Time) (ReservationEntry, Stay, error) {
	restored, err := s.reservations.RestoreAfterVoid(ctx, r.ID, now)
	if err != nil {
		return r, stay, err
	}
	ok, err := s.lockStay(ctx, r, stay)
	if err != nil {
		return r, stay, err
	}
	if !restored || !ok {
		return r, stay, fmt.Errorf("gate: stale reservation stay %s changed under the lot lock", stay.PublicID)
	}
	if err := s.repo.VoidStay(ctx, r.LotID, stay.ID, now); err != nil {
		return r, stay, err
	}
	r.Status, stay.Status = reservationConfirmed, StatusVoided
	return r, stay, nil
}
