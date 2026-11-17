package gate

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/XYZLZ/MyPark/apps/server/internal/domain/authz"
	"github.com/XYZLZ/MyPark/apps/server/internal/domain/errcodes"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/audit"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/availability"
)

// entry decides entry.credential_presented (design §9). Everything that
// takes no lock (code, tariff, card lookup) runs before the lot lock, so the
// lock is held only for the counts and the insert (M2C-29).
func (s *Service) entry(ctx context.Context, d authz.Device, ev Event) (Result, error) {
	p, ok := ev.Payload.(EntryCredential)
	if !ok {
		return Result{}, fmt.Errorf("gate: entry event carries %T", ev.Payload)
	}
	switch p.Kind {
	case KindTicketRequest:
		return s.issueTicket(ctx, d)
	case KindEmployeeCard:
		return s.cardEntry(ctx, d, p.Value)
	case KindReservationQR:
		return s.qrEntry(ctx, d, p.Value)
	default:
		return Result{}, fmt.Errorf("gate: entry credential kind %q", p.Kind)
	}
}

// issueTicket answers a dispenser's ticket request (design §9.3). The plain
// code goes only into the answer, which the device API seals before storing.
func (s *Service) issueTicket(ctx context.Context, d authz.Device) (Result, error) {
	now := s.clock.Now()
	code, err := NewTicketCode(s.rand)
	if err != nil {
		return Result{}, err
	}
	tariffID, err := s.tariffAt(ctx, d.LotID, now)
	if err != nil {
		return Result{}, err
	}
	lot, err := s.repo.LockLotOccupancy(ctx, d.LotID)
	if err != nil {
		return Result{}, err
	}
	if refusal, err := s.refusal(ctx, d.LotID, lot, now); err != nil || refusal != "" {
		return deny(refusal), err
	}
	stayID, err := s.repo.InsertStay(ctx, NewStay{
		LotID: d.LotID, TicketCodeHash: HashTicketCode(code), TariffVersionID: tariffID, AcceptedAt: now,
	})
	if err != nil {
		return Result{}, err
	}
	return Result{Result: OutcomeOpen, StayID: &stayID, TicketCode: code}, nil
}

// cardEntry answers an employee card at the entry barrier (design §9.5): an
// active card of the lot, at most one open stay per card, and the same
// capacity rule as drive-up.
func (s *Service) cardEntry(ctx context.Context, d authz.Device, uid string) (Result, error) {
	now := s.clock.Now()
	n, ok := NormalizeCardUID(uid)
	if !ok {
		return deny(errcodes.GateCredentialInvalid), nil
	}
	card, err := s.repo.ActiveCardByHash(ctx, d.LotID, HashCardUID(s.cardPepper, n))
	if errors.Is(err, ErrNotFound) {
		return deny(errcodes.GateCredentialInvalid), nil
	}
	if err != nil {
		return Result{}, err
	}
	tariffID, err := s.tariffAt(ctx, d.LotID, now)
	if err != nil {
		return Result{}, err
	}

	// The lot lock comes before any stays read (M2C-29). It also serializes
	// two entries of the same card, so the lookup decides; the
	// stays_card_open_key index is only the backstop.
	lot, err := s.repo.LockLotOccupancy(ctx, d.LotID)
	if err != nil {
		return Result{}, err
	}
	inUse, err := s.repo.CardHasOpenStay(ctx, d.LotID, card.ID)
	if err != nil {
		return Result{}, err
	}
	if inUse {
		return deny(errcodes.GateCredentialInUse), nil
	}
	if refusal, err := s.refusal(ctx, d.LotID, lot, now); err != nil || refusal != "" {
		return deny(refusal), err
	}
	stayID, err := s.repo.InsertStay(ctx, NewStay{
		LotID: d.LotID, EmployeeCardID: card.ID, TariffVersionID: tariffID, AcceptedAt: now,
	})
	if err != nil {
		return Result{}, err
	}
	if err := s.audit.Record(ctx, audit.Entry{
		Action: audit.GateEmployeeEntry, Actor: audit.ActorSystem, EntityType: "stay", EntityID: stayID.String(),
		After: map[string]any{"employee_card_id": card.PublicID.String(), "device_id": d.PublicID.String()},
	}); err != nil {
		return Result{}, fmt.Errorf("gate: audit %s: %w", audit.GateEmployeeEntry, err)
	}
	return Result{Result: OutcomeOpen, StayID: &stayID}, nil
}

// refusal applies availability.CanAdmitDriveUp to the locked lot and returns
// the denial reason, or "" to admit. The counts run after the lock, so under
// READ COMMITTED they see every decision committed before it.
func (s *Service) refusal(ctx context.Context, lotID int64, lot LotOccupancy, now time.Time) (errcodes.Code, error) {
	oos, err := s.repo.CountOutOfService(ctx, lotID)
	if err != nil {
		return "", err
	}
	pending, err := s.repo.CountFreshPending(ctx, lotID, now)
	if err != nil {
		return "", err
	}
	holds, err := s.holds.EffectiveHolds(ctx, lotID, now)
	if err != nil {
		return "", fmt.Errorf("gate: reservation holds: %w", err)
	}
	_, code := availability.CanAdmitDriveUp(availability.AdmitInput{
		LotClosed: lot.Closed, Capacity: lot.Capacity, OutOfService: oos,
		Occupied: lot.Occupied, Pending: pending, Holds: holds,
	})
	return code, nil
}

// tariffAt is the id of the lot's tariff version in force at now, which the
// stay keeps for its whole life (M2C-2).
func (s *Service) tariffAt(ctx context.Context, lotID int64, now time.Time) (int64, error) {
	v, err := s.tariffs.AtTime(ctx, lotID, now)
	if err != nil {
		return 0, fmt.Errorf("gate: tariff at entry: %w", err)
	}
	return v.ID, nil
}

func deny(code errcodes.Code) Result {
	return Result{Result: OutcomeDeny, Reason: string(code)}
}
