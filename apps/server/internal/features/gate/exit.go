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

// exitAuthorizationTTL is how long an opened exit credential lets the stay
// pass the exit barrier (design §9.6).
const exitAuthorizationTTL = 60 * time.Second

// exitCredential decides whether the exit barrier opens (design §10.4). It
// locks only the stay (M2C-29) and prices it with the pay station's
// amountDue. Nothing due opens and records the decision (M2C-32); the zero
// payment and the occupancy change wait for the exit pass.
func (s *Service) exitCredential(ctx context.Context, d authz.Device, ev Event) (Result, error) {
	p, ok := ev.Payload.(ExitCredential)
	if !ok {
		return Result{}, fmt.Errorf("gate: exit credential event carries %T", ev.Payload)
	}
	now := s.clock.Now()
	stay, card, err := s.exitStay(ctx, d.LotID, p.Credential)
	if errors.Is(err, ErrNotFound) || (err == nil && !inside(stay.Status)) {
		return deny(errcodes.GateCredentialInvalid), nil
	}
	if err != nil {
		return Result{}, err
	}
	if err := s.reactivateIfLapsed(ctx, &stay, now); err != nil {
		return Result{}, err
	}
	due, bd, _, err := s.amountDue(ctx, stay, now)
	if err != nil {
		return Result{}, err
	}
	if due > 0 { // every overstay lands here: its due is never 0 (O8)
		// An earlier open must not let the car skip this payment.
		if !stay.ExitAuthorizedAt.IsZero() {
			if err := s.repo.ClearExitAuthorization(ctx, d.LotID, stay.ID, now); err != nil {
				return Result{}, err
			}
		}
		res := deny(errcodes.GatePaymentRequired)
		res.StayID, res.AmountCents, res.Breakdown = &stay.PublicID, &due, &bd
		return res, nil
	}
	if err := s.repo.AuthorizeExit(ctx, d.LotID, stay.ID, now); err != nil {
		return Result{}, err
	}
	if card.Revoked {
		if err := s.audit.Record(ctx, audit.Entry{
			Action: audit.GateRevokedCardExit, Actor: audit.ActorSystem, EntityType: "stay", EntityID: stay.PublicID.String(),
			After: map[string]any{"employee_card_id": card.PublicID.String(), "device_id": d.PublicID.String()},
		}); err != nil {
			return Result{}, fmt.Errorf("gate: audit %s: %w", audit.GateRevokedCardExit, err)
		}
	}
	return Result{Result: OutcomeOpen, StayID: &stay.PublicID}, nil
}

// exitStay locks the stay a credential names, as stayByCredential does,
// except that an employee card is found whatever its status: a card revoked
// while its car is inside may still leave through its open stay. A revoked
// card with no open stay is not found, and entry keeps denying it.
func (s *Service) exitStay(ctx context.Context, lotID int64, c Credential) (Stay, Card, error) {
	if c.Kind != KindEmployeeCard {
		stay, err := s.stayByCredential(ctx, lotID, c)
		return stay, Card{}, err
	}
	uid, ok := NormalizeCardUID(c.Value)
	if !ok {
		return Stay{}, Card{}, ErrNotFound
	}
	card, err := s.repo.CardByHash(ctx, lotID, HashCardUID(s.cardPepper, uid))
	if err != nil {
		return Stay{}, Card{}, err
	}
	stay, err := s.repo.OpenCardStayForUpdate(ctx, lotID, card.ID)
	return stay, card, err
}

// exitPass counts a car leaving through the exit barrier (design §9.6,
// §10.5). Locks: the lot's occupancy, then the stay (M2C-29); the status is
// read after both, so a second pass of the same stay sees it exited.
func (s *Service) exitPass(ctx context.Context, d authz.Device, p BarrierPassed) (Result, error) {
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
	case StatusActive, StatusPaid:
		// No fresh authorization (tailgating, or a stale one): nothing
		// changes. The decision is the credential's, never re-priced here.
		if stay.ExitAuthorizedAt.IsZero() || stay.ExitAuthorizedAt.Before(now.Add(-exitAuthorizationTTL)) {
			return rejected(errcodes.GateExitNotAuthorized), nil
		}
		return s.exit(ctx, d.LotID, stay, now)
	case StatusExited:
		return Result{Result: OutcomeAck, Reason: string(errcodes.GateDuplicatePass)}, nil
	case StatusEntryPending, StatusVoided: // never inside
		return rejected(errcodes.GateStayUnknown), nil
	default:
		return Result{}, fmt.Errorf("gate: stay %s has unknown status %q", stay.PublicID, stay.Status)
	}
}

// exit moves the locked, authorized stay to exited at now, uncounts it and
// queues the lot_changed notification. An active stay was authorized with
// nothing due: it gets its RD$0 payment and hops active -> paid -> exited in
// this one transaction (M2C-32). A paid stay keeps its paid_at.
func (s *Service) exit(ctx context.Context, lotID int64, stay Stay, now time.Time) (Result, error) {
	hops := []Status{stay.Status, StatusExited}
	if stay.Status == StatusActive {
		hops = []Status{StatusActive, StatusPaid, StatusExited}
	}
	for i := 1; i < len(hops); i++ {
		if !CanTransition(hops[i-1], hops[i]) {
			return Result{}, fmt.Errorf("gate: stay %s cannot go from %s to %s", stay.PublicID, hops[i-1], hops[i])
		}
	}
	if stay.Status == StatusActive {
		userID, err := s.paymentUser(ctx, stay)
		if err != nil {
			return Result{}, err
		}
		if _, err := s.repo.InsertPayment(ctx, NewPayment{
			StayID: stay.ID, UserID: userID, Method: "none", AmountCents: 0, SucceededAt: now,
			IdempotencyKey: "zero:" + stay.PublicID.String(),
		}); err != nil {
			return Result{}, err
		}
	}
	if err := s.repo.SetStayExited(ctx, lotID, stay.ID, now); err != nil {
		return Result{}, err
	}
	// design 8.1: stays, payments, then the reservation, under the lot lock.
	if stay.ReservationID != 0 {
		if err := s.reservations.Complete(ctx, stay.ReservationID, now); err != nil {
			return Result{}, err
		}
	}
	version, err := s.repo.BumpOccupancy(ctx, lotID, -1, now)
	if err != nil {
		return Result{}, err
	}
	if err := s.repo.NotifyLotChanged(ctx, lotID, version); err != nil {
		return Result{}, err
	}
	return Result{Result: OutcomeAck}, nil
}
