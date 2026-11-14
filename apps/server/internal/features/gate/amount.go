package gate

import (
	"context"
	"fmt"
	"time"

	"github.com/XYZLZ/MyPark/apps/server/internal/features/tariffs"
)

// ReceiptNo is the receipt number of a payment: "MP-" and its id padded to
// eight digits (M2C-34).
func ReceiptNo(paymentID int64) string { return fmt.Sprintf("MP-%08d", paymentID) }

// amountDue is what the locked stay owes at now under the tariff version it
// entered with (M2C-2). Scan, lost ticket, cash and exit all price through it,
// so their amounts always agree (design §12.2). It also returns the tariff it
// priced with, which sets a payment's exit window.
func (s *Service) amountDue(ctx context.Context, stay Stay, now time.Time) (int64, tariffs.Breakdown, tariffs.Tariff, error) {
	v, err := s.tariffs.ByID(ctx, stay.TariffVersionID)
	if err != nil {
		return 0, tariffs.Breakdown{}, tariffs.Tariff{}, fmt.Errorf("gate: tariff of stay: %w", err)
	}
	paid, paidAt, err := s.repo.PaymentTotals(ctx, stay.LotID, stay.ID)
	if err != nil {
		return 0, tariffs.Breakdown{}, tariffs.Tariff{}, err
	}
	due, bd := tariffs.AmountDue(v.Tariff, tariffs.Stay{
		Kind: tariffs.StayKind(stay.CredentialKind), EnteredAt: stay.EnteredAt, Paid: stay.Status == StatusPaid,
		LostTicket: stay.LostTicket, PaidCents: paid, PaidAt: paidAt, ExitDeadline: stay.ExitDeadlineAt,
	}, now)
	return due, bd, v.Tariff, nil
}

// reactivateIfLapsed applies the lazy paid -> active transition (M2C-35): a
// paid stay whose exit window has closed owes an overstay. It keeps the
// deadline, which keys the overstay pricing, and updates stay in place. The
// query cancels any exit authorization: the stay owes money again.
func (s *Service) reactivateIfLapsed(ctx context.Context, stay *Stay, now time.Time) error {
	if stay.Status != StatusPaid || !now.After(stay.ExitDeadlineAt) {
		return nil
	}
	if !CanTransition(stay.Status, StatusActive) {
		return fmt.Errorf("gate: stay %s cannot go from %s to %s", stay.PublicID, stay.Status, StatusActive)
	}
	if err := s.repo.ReactivateStay(ctx, stay.LotID, stay.ID, now); err != nil {
		return err
	}
	stay.Status, stay.ExitAuthorizedAt = StatusActive, time.Time{}
	return nil
}
