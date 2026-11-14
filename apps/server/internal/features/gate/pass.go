package gate

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/XYZLZ/MyPark/apps/server/internal/domain/authz"
	"github.com/XYZLZ/MyPark/apps/server/internal/domain/errcodes"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/audit"
)

// pass applies barrier.passed (design §9.6, M2C-31). The barrier is the
// physical truth: a car that passed is counted, once. Exit passes arrive
// with the exit flow.
func (s *Service) pass(ctx context.Context, d authz.Device, ev Event) (Result, error) {
	p, ok := ev.Payload.(BarrierPassed)
	if !ok {
		return Result{}, fmt.Errorf("gate: pass event carries %T", ev.Payload)
	}
	if p.Direction != DirectionEntry {
		return Result{}, fmt.Errorf("%w: %s %s", ErrUnhandledEvent, ev.Type, p.Direction)
	}
	return s.entryPass(ctx, d, p)
}

// entryPass counts a car passing the entry barrier. Locks: the lot's
// occupancy, then the stay, then (late card pass) the card's open stay
// (M2C-29). The status is read after the locks, so a pass racing the void
// job or a second pass of the same stay sees the committed state.
func (s *Service) entryPass(ctx context.Context, d authz.Device, p BarrierPassed) (Result, error) {
	now := s.clock.Now()
	if _, err := s.repo.LockLotOccupancy(ctx, d.LotID); err != nil {
		return Result{}, err
	}
	stay, err := s.repo.StayForUpdate(ctx, d.LotID, p.StayID)
	if errors.Is(err, ErrNotFound) {
		return rejected(errcodes.GateStayUnknown), nil
	}
	if err != nil {
		return Result{}, err
	}

	switch stay.Status {
	case StatusEntryPending:
		return s.activate(ctx, d.LotID, stay, now)
	case StatusVoided:
		return s.lateEntryPass(ctx, d, stay, now)
	case StatusActive, StatusPaid:
		return Result{Result: OutcomeAck, Reason: string(errcodes.GateDuplicatePass)}, nil
	case StatusExited: // the stay is over
		return rejected(errcodes.GateStayUnknown), nil
	default:
		return Result{}, fmt.Errorf("gate: stay %s has unknown status %q", stay.PublicID, stay.Status)
	}
}

// lateEntryPass reactivates a stay voided before its car passed: the car is
// inside, so it is counted, logged at warn and audited. A card stay stays
// voided when the card already has a newer open stay (one per card, M2C-28).
func (s *Service) lateEntryPass(ctx context.Context, d authz.Device, stay Stay, now time.Time) (Result, error) {
	if stay.EmployeeCardID != 0 {
		inUse, err := s.repo.CardHasOpenStay(ctx, d.LotID, stay.EmployeeCardID)
		if err != nil {
			return Result{}, err
		}
		if inUse {
			return rejected(errcodes.GateCredentialInUse), nil
		}
	}
	res, err := s.activate(ctx, d.LotID, stay, now)
	if err != nil {
		return Result{}, err
	}
	if err := s.audit.Record(ctx, audit.Entry{
		Action: audit.GateLateEntryPass, Actor: audit.ActorSystem, EntityType: "stay", EntityID: stay.PublicID.String(),
		Before: map[string]any{"status": string(StatusVoided)},
		After:  map[string]any{"status": string(StatusActive), "device_id": d.PublicID.String()},
	}); err != nil {
		return Result{}, fmt.Errorf("gate: audit %s: %w", audit.GateLateEntryPass, err)
	}
	// Warn only once the audit row is written: the log claims an audited pass.
	s.log.WarnContext(ctx, "gate: late entry pass on a voided stay",
		"lot_id", d.LotID, "stay_id", stay.PublicID.String(), "device_id", d.PublicID.String())
	return res, nil
}

// activate marks the locked stay active at now, counts it and queues the
// lot_changed notification, which is sent only on commit.
func (s *Service) activate(ctx context.Context, lotID int64, stay Stay, now time.Time) (Result, error) {
	if !CanTransition(stay.Status, StatusActive) {
		return Result{}, fmt.Errorf("gate: stay %s cannot go from %s to %s", stay.PublicID, stay.Status, StatusActive)
	}
	if err := s.repo.ActivateStay(ctx, lotID, stay.ID, now); err != nil {
		return Result{}, err
	}
	version, err := s.repo.BumpOccupancy(ctx, lotID, +1, now)
	if err != nil {
		return Result{}, err
	}
	if err := s.repo.NotifyLotChanged(ctx, lotID, version); err != nil {
		return Result{}, err
	}
	return Result{Result: OutcomeAck}, nil
}

func rejected(code errcodes.Code) Result {
	return Result{Result: OutcomeRejected, Reason: string(code)}
}
