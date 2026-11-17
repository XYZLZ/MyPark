package reservations

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"time"

	"github.com/XYZLZ/MyPark/apps/server/internal/features/alerts"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/gate"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/payments"
)

// cancelledBySystem is the cancelled_by value of the physically-full cancel.
const cancelledBySystem = "system"

// GateAccess implements gate.Reservations over the service's ports (design
// 5.1). Every method but Resolve and Decide runs on the device transaction in
// ctx, under the lot lock the gate took first (design 8.1, QR entry); none
// opens a transaction of its own.
type GateAccess struct{ s *Service }

var _ gate.Reservations = GateAccess{}

// NewGateAccess returns the gate adapter of s.
func NewGateAccess(s *Service) GateAccess { return GateAccess{s: s} }

// Resolve implements gate.Reservations: the token is verified (constant-time
// MAC) and its nonce compared in constant time. The token is never logged
// nor put in an error.
func (g GateAccess) Resolve(ctx context.Context, lotID int64, token string) (gate.ReservationRef, error) {
	id, nonce, ok := DecodeQR(g.s.qrKey, token)
	if !ok {
		return gate.ReservationRef{}, gate.ErrNotFound
	}
	row, err := g.s.repo.ForQR(ctx, lotID, id)
	switch {
	case errors.Is(err, ErrNotFound):
		return gate.ReservationRef{}, gate.ErrNotFound
	case err != nil:
		return gate.ReservationRef{}, err
	case !sameNonce(row.Nonce, nonce):
		return gate.ReservationRef{}, gate.ErrNotFound
	}
	return gate.ReservationRef{ID: row.ID, PublicID: row.PublicID, Nonce: nonce}, nil
}

// sameNonce compares a stored nonce (nil before confirmation) to a token's.
func sameNonce(stored []byte, presented [16]byte) bool {
	return len(stored) == len(presented) && subtle.ConstantTimeCompare(stored, presented[:]) == 1
}

// LockForEntry implements gate.Reservations.
func (g GateAccess) LockForEntry(ctx context.Context, lotID int64, ref gate.ReservationRef) (gate.ReservationEntry, error) {
	row, err := g.s.repo.LockForEntry(ctx, lotID, ref.ID)
	switch {
	case errors.Is(err, ErrNotFound):
		return gate.ReservationEntry{}, gate.ErrNotFound
	case err != nil:
		return gate.ReservationEntry{}, err
	case !sameNonce(row.Nonce, ref.Nonce):
		// The nonce rotated between the lookup and the lock.
		return gate.ReservationEntry{}, gate.ErrNotFound
	}
	return gate.ReservationEntry{
		ID: row.ID, UserID: row.UserID, LotID: row.LotID, TariffVersionID: row.TariffVersionID, PublicID: row.PublicID,
		Status: string(row.Status), WindowStart: row.Window.Start, WindowEnd: row.Window.End,
	}, nil
}

// gateActions maps the decision to the gate's mirror of it.
var gateActions = map[QRAction]gate.QRAction{
	QRDeny: gate.QRDeny, QRCheckIn: gate.QRCheckIn, QRReopen: gate.QRReopen,
	QRRefresh: gate.QRRefresh, QRPhysicallyFull: gate.QRPhysicallyFull,
}

// Decide implements gate.Reservations with DecideQR.
func (GateAccess) Decide(in gate.QRDecisionInput) gate.QRDecision {
	r := in.Reservation
	out := DecideQR(QRInput{
		Status: Status(r.Status), Window: Window{Start: r.WindowStart, End: r.WindowEnd}, Now: in.Now,
		Occupied: in.Occupied, Pending: in.Pending, EffectiveCapacity: in.EffectiveCapacity, StayStatus: string(in.StayStatus),
	})
	return gate.QRDecision{Action: gateActions[out.Action], Reason: out.Reason}
}

func window(r gate.ReservationEntry) Window { return Window{Start: r.WindowStart, End: r.WindowEnd} }

// LockSlots implements gate.Reservations.
func (g GateAccess) LockSlots(ctx context.Context, r gate.ReservationEntry) error {
	_, err := g.s.repo.EnsureAndLockSlots(ctx, r.LotID, window(r))
	return err
}

// CheckIn implements gate.Reservations: confirmed → checked_in and its slot
// loads released, in the device transaction (design 10.1).
func (g GateAccess) CheckIn(ctx context.Context, r gate.ReservationEntry, now time.Time) error {
	if _, err := Transition(Status(r.Status), EvCheckIn); err != nil {
		return err
	}
	if err := g.s.repo.CheckIn(ctx, r.ID, now); err != nil {
		return err
	}
	return g.release(ctx, r)
}

func (g GateAccess) release(ctx context.Context, r gate.ReservationEntry) error {
	if _, err := g.s.repo.EnsureAndLockSlots(ctx, r.LotID, window(r)); err != nil {
		return err
	}
	return g.s.repo.DecSlots(ctx, r.LotID, window(r))
}

// CancelUnadmitted implements gate.Reservations: design 11.5 steps 1 to 5,
// in the lock order reservation, slot loads, payment, refund, alert, outbox.
// The gate bumps the version (step 6).
func (g GateAccess) CancelUnadmitted(ctx context.Context, r gate.ReservationEntry, now time.Time) error {
	if _, err := Transition(Status(r.Status), EvUnadmitted); err != nil {
		return err
	}
	if g.s.alerts == nil {
		return errors.New("reservations: physically-full cancel needs the alerts recorder")
	}
	if err := g.s.repo.Cancel(ctx, r.ID, cancelledBySystem, payments.RefundFacilityUnavailable, now); err != nil {
		return err
	}
	if err := g.release(ctx, r); err != nil {
		return err
	}
	fee, live, err := g.s.fees.LiveFee(ctx, r.ID)
	if err != nil {
		return err
	}
	if !live || fee.Status != payments.StatusSucceeded {
		return fmt.Errorf("reservations: confirmed reservation %s has no succeeded fee", r.PublicID)
	}
	refund, err := g.s.fees.QueueRefund(ctx, fee.ID, fee.AmountCents, payments.RefundFacilityUnavailable, now)
	if err != nil {
		return err
	}
	if err := g.s.alerts.Open(ctx, r.LotID, alerts.KindReservationUnadmitted, map[string]string{
		"reservation_id": r.PublicID.String(), "refund_id": refund.PublicID.String(),
	}, now); err != nil {
		return err
	}
	rsv := ApplyRow{ID: r.ID, PublicID: r.PublicID, UserID: r.UserID, LotID: r.LotID, FeeCents: fee.AmountCents}
	return g.s.enqueueCancelled(ctx, rsv, refundPending, payments.RefundFacilityUnavailable)
}

// RestoreAfterVoid implements gate.Reservations: the void of its stay moves
// a checked_in reservation back to confirmed and restores its hold WITHOUT a
// cap check (M3B-60: a confirmed reservation is never cancelled for cap).
// Locks the reservation, then its slot loads (design 8.1, void job). Any
// other status is left alone: (false, nil).
func (g GateAccess) RestoreAfterVoid(ctx context.Context, reservationID int64, now time.Time) (bool, error) {
	r, err := g.s.repo.LockForApply(ctx, reservationID)
	if err != nil {
		return false, err
	}
	if _, err := Transition(r.Status, EvEntryVoided); err != nil { // not checked_in
		return false, nil
	}
	if _, err := g.s.repo.EnsureAndLockSlots(ctx, r.LotID, r.Window); err != nil {
		return false, err
	}
	if err := g.s.repo.IncSlots(ctx, r.LotID, r.Window); err != nil {
		return false, err
	}
	return true, g.s.repo.UndoCheckIn(ctx, r.ID, now)
}

// LateEntryPass implements gate.Reservations: the car of a voided stay
// passed after all, so a confirmed reservation is checked in at now and its
// hold released (design 12.7). Called under the lot and stay locks. Any
// other status (cancelled or expired meanwhile) is left alone: (false, nil).
func (g GateAccess) LateEntryPass(ctx context.Context, reservationID int64, now time.Time) (bool, error) {
	r, err := g.s.repo.LockForApply(ctx, reservationID)
	if err != nil {
		return false, err
	}
	if _, err := Transition(r.Status, EvLateEntryPass); err != nil { // not confirmed
		return false, nil
	}
	if err := g.s.repo.CheckIn(ctx, r.ID, now); err != nil {
		return false, err
	}
	if _, err := g.s.repo.EnsureAndLockSlots(ctx, r.LotID, r.Window); err != nil {
		return false, err
	}
	return true, g.s.repo.DecSlots(ctx, r.LotID, r.Window)
}

// Complete implements gate.Reservations (S6.3).
func (GateAccess) Complete(context.Context, int64, time.Time) error { return errors.ErrUnsupported }
