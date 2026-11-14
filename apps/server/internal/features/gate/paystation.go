package gate

import (
	"context"
	"errors"
	"fmt"

	"github.com/XYZLZ/MyPark/apps/server/internal/domain/authz"
	"github.com/XYZLZ/MyPark/apps/server/internal/domain/errcodes"
)

// The pay-station flows lock only the stay, then insert payments (M2C-29):
// never the lot, and occupancy does not change, since active and paid stays
// both count as inside.

// payScan prices the stay a credential names (design §10.1). It changes the
// stay only through the lazy paid -> active transition.
func (s *Service) payScan(ctx context.Context, d authz.Device, ev Event) (Result, error) {
	p, ok := ev.Payload.(PayScan)
	if !ok {
		return Result{}, fmt.Errorf("gate: scan event carries %T", ev.Payload)
	}
	now := s.clock.Now()
	stay, err := s.stayByCredential(ctx, d.LotID, p.Credential)
	if errors.Is(err, ErrNotFound) || (err == nil && !inside(stay.Status)) {
		return rejected(errcodes.GateCredentialInvalid), nil
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
	return Result{
		Result: OutcomeAck, StayID: &stay.PublicID, Status: string(stay.Status), AmountCents: &due, Breakdown: &bd,
	}, nil
}

// lostTicket replaces a ticket the driver lost and prices the lost fee
// (design §10.2, M2C-33). The old code stops working; the new one goes only
// into the answer, which the device API seals before storing.
func (s *Service) lostTicket(ctx context.Context, d authz.Device, ev Event) (Result, error) {
	p, ok := ev.Payload.(LostTicket)
	if !ok {
		return Result{}, fmt.Errorf("gate: lost ticket event carries %T", ev.Payload)
	}
	now := s.clock.Now()
	code, err := NewTicketCode(s.rand)
	if err != nil {
		return Result{}, err
	}
	stay, err := s.repo.StayForUpdate(ctx, d.LotID, p.StayID)
	if errors.Is(err, ErrNotFound) || (err == nil && (stay.CredentialKind != KindTicket || !inside(stay.Status))) {
		return rejected(errcodes.GateStayUnknown), nil
	}
	if err != nil {
		return Result{}, err
	}
	// A lapsed window reopens the stay first, exactly as a scan would.
	if err := s.reactivateIfLapsed(ctx, &stay, now); err != nil {
		return Result{}, err
	}
	if !stay.LostTicket && stay.Status == StatusPaid && !CanTransition(StatusPaid, StatusActive) {
		return Result{}, fmt.Errorf("gate: stay %s cannot go from %s to %s", stay.PublicID, stay.Status, StatusActive)
	}
	if err := s.repo.MarkStayLost(ctx, d.LotID, stay.ID, HashTicketCode(code), now); err != nil {
		return Result{}, err
	}
	// Price what was just written. The first mark makes a paid stay owe
	// money again (L7); a second one only rotates the code (§10.2).
	if !stay.LostTicket {
		stay.Status, stay.LostTicket = StatusActive, true
	}
	due, bd, _, err := s.amountDue(ctx, stay, now)
	if err != nil {
		return Result{}, err
	}
	return Result{Result: OutcomeAck, StayID: &stay.PublicID, TicketCode: code, AmountCents: &due, Breakdown: &bd}, nil
}

// cashPaid records a cash payment of exactly the amount due (design §10.3,
// M2C-34). Two payments of one stay serialize on the stay lock, so the
// second finds it paid and nothing due.
func (s *Service) cashPaid(ctx context.Context, d authz.Device, ev Event) (Result, error) {
	p, ok := ev.Payload.(CashPaid)
	if !ok {
		return Result{}, fmt.Errorf("gate: cash event carries %T", ev.Payload)
	}
	now := s.clock.Now()
	stay, err := s.repo.StayForUpdate(ctx, d.LotID, p.StayID)
	if errors.Is(err, ErrNotFound) || (err == nil && !inside(stay.Status)) {
		return rejected(errcodes.GateStayUnknown), nil
	}
	if err != nil {
		return Result{}, err
	}
	if err := s.reactivateIfLapsed(ctx, &stay, now); err != nil {
		return Result{}, err
	}
	due, _, tariff, err := s.amountDue(ctx, stay, now)
	switch {
	case err != nil:
		return Result{}, err
	case due == 0:
		return rejected(errcodes.GateNothingDue), nil
	case p.AmountCents != due:
		res := rejected(errcodes.GateAmountMismatch)
		res.AmountCents = &due
		return res, nil
	case !CanTransition(stay.Status, StatusPaid):
		return Result{}, fmt.Errorf("gate: stay %s cannot go from %s to %s", stay.PublicID, stay.Status, StatusPaid)
	}

	deadline := now.Add(tariff.PostPaymentExit)
	paymentID, err := s.repo.InsertPayment(ctx, NewPayment{
		StayID: stay.ID, Method: "cash", AmountCents: due, DeviceID: d.ID, SucceededAt: now,
		IdempotencyKey: "cash:" + d.PublicID.String() + ":" + ev.ID.String(),
	})
	if err != nil {
		return Result{}, err
	}
	if err := s.repo.SetStayPaid(ctx, d.LotID, stay.ID, now, deadline); err != nil {
		return Result{}, err
	}
	return Result{
		Result: OutcomeAck, ReceiptNo: ReceiptNo(paymentID), AmountCents: &due, PaidAt: &now, ExitDeadline: &deadline,
	}, nil
}

// stayByCredential locks the lot's stay a presented credential names (design
// §10.1), or returns ErrNotFound. Ticket codes are compared by hash only.
func (s *Service) stayByCredential(ctx context.Context, lotID int64, c Credential) (Stay, error) {
	switch c.Kind {
	case KindTicket:
		code, ok := NormalizeTicketCode(c.Value)
		if !ok {
			return Stay{}, ErrNotFound
		}
		return s.repo.StayByTicketHashForUpdate(ctx, lotID, HashTicketCode(code))
	case KindEmployeeCard:
		uid, ok := NormalizeCardUID(c.Value)
		if !ok {
			return Stay{}, ErrNotFound
		}
		card, err := s.repo.ActiveCardByHash(ctx, lotID, HashCardUID(s.cardPepper, uid))
		if err != nil {
			return Stay{}, err
		}
		return s.repo.OpenCardStayForUpdate(ctx, lotID, card.ID)
	default: // reservation_qr: no reservations before M3 (M2C-1)
		return Stay{}, ErrNotFound
	}
}

// inside reports whether a stay's car is in the lot: the only stays the pay
// station serves.
func inside(st Status) bool { return st == StatusActive || st == StatusPaid }
