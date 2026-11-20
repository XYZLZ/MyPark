package paymentsim_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/XYZLZ/MyPark/apps/server/internal/features/payments"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/paymentsim"
)

func TestSim_RefundOnce(t *testing.T) {
	f := newFixture(t)
	if _, err := f.gw.Charge(t.Context(), charge("rsv-fee:r", "4242424242424242")); err != nil {
		t.Fatal(err)
	}
	req := payments.RefundRequest{IdempotencyKey: "refund:1", ChargeKey: "rsv-fee:r", AmountCents: 15000}
	first, err := f.gw.Refund(t.Context(), req)
	if err != nil {
		t.Fatalf("Refund: %v", err)
	}
	if !strings.HasPrefix(first.ProviderRef, "sim_rf_") || len(first.ProviderRef) != len("sim_rf_")+24 {
		t.Errorf("ProviderRef = %q", first.ProviderRef)
	}
	f.clk.Advance(time.Hour)
	again, err := f.gw.Refund(t.Context(), req)
	if err != nil || again != first {
		t.Errorf("repeat = %+v, %v; want %+v", again, err, first)
	}
	var refundedAt time.Time
	if err := f.pool.QueryRow(t.Context(),
		`select refunded_at from sim_gateway_charges where idempotency_key = 'rsv-fee:r'`).Scan(&refundedAt); err != nil {
		t.Fatal(err)
	}
	if !refundedAt.Equal(t0) {
		t.Errorf("refunded_at = %v, want the injected clock's %v (kept on repeat)", refundedAt, t0)
	}

	other := req
	other.IdempotencyKey = "refund:2"
	if _, err := f.gw.Refund(t.Context(), other); err == nil {
		t.Error("second refund key on the same charge succeeded")
	}
	wrongAmount := req
	wrongAmount.IdempotencyKey, wrongAmount.ChargeKey, wrongAmount.AmountCents = "refund:3", "rsv-fee:r2", 1
	if _, err := f.gw.Charge(t.Context(), charge("rsv-fee:r2", "4242424242424242")); err != nil {
		t.Fatal(err)
	}
	if _, err := f.gw.Refund(t.Context(), wrongAmount); err == nil {
		t.Error("partial refund succeeded")
	}
}

func TestSim_RefundOfDeclinedOrUnknownFails(t *testing.T) {
	f := newFixture(t)
	if _, err := f.gw.Charge(t.Context(), charge("rsv-fee:d", "4000000000000002")); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"rsv-fee:d", "rsv-fee:missing"} {
		_, err := f.gw.Refund(t.Context(), payments.RefundRequest{IdempotencyKey: "refund:x", ChargeKey: key, AmountCents: 15000})
		if !errors.Is(err, paymentsim.ErrRefundRejected) {
			t.Errorf("refund of %s err = %v, want ErrRefundRejected", key, err)
		}
	}
}
