package paymentsim_test

import (
	"context"
	"errors"
	"io"
	"math/rand/v2"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/XYZLZ/MyPark/apps/server/internal/features/payments"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/paymentsim"
	"github.com/XYZLZ/MyPark/apps/server/internal/testsupport"
	"github.com/XYZLZ/MyPark/apps/server/internal/testsupport/fakeclock"
)

const (
	latency      = 800 * time.Millisecond
	timeoutDelay = 30 * time.Second
)

var t0 = time.Date(2026, 11, 20, 15, 0, 0, 0, time.UTC)

// sleeper records the durations it was asked for. It returns at once, except
// for the timeout delay, which blocks until the context ends like a real wait.
type sleeper struct {
	mu    sync.Mutex
	asked []time.Duration
}

func (s *sleeper) sleep(ctx context.Context, d time.Duration) error {
	s.mu.Lock()
	s.asked = append(s.asked, d)
	s.mu.Unlock()
	if d == timeoutDelay {
		<-ctx.Done()
	}
	return ctx.Err()
}

func (s *sleeper) durations() []time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]time.Duration(nil), s.asked...)
}

type fixture struct {
	gw    *paymentsim.Gateway
	pool  *pgxpool.Pool
	sleep *sleeper
	clk   *fakeclock.Clock
}

// seededRand is a deterministic io.Reader for provider refs.
func seededRand(seed uint64) io.Reader {
	return rand.NewChaCha8([32]byte{byte(seed)})
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	pool := testsupport.NewDB(t)
	s := &sleeper{}
	clk := fakeclock.New(t0)
	return &fixture{
		pool: pool, sleep: s, clk: clk,
		gw: paymentsim.New(pool, paymentsim.Config{
			Clock: clk, Latency: latency, TimeoutDelay: timeoutDelay, Sleep: s.sleep, Rand: seededRand(1),
		}),
	}
}

func card(number string) *payments.Card {
	return &payments.Card{Number: number, ExpMonth: 12, ExpYear: 2030, CVC: "123"}
}

func charge(key, number string) payments.ChargeRequest {
	return payments.ChargeRequest{IdempotencyKey: key, AmountCents: 15000, Card: card(number)}
}

func TestSim_OutcomeTable(t *testing.T) {
	tests := []struct {
		number  string
		outcome payments.ChargeOutcome
		decline string
		brand   string
	}{
		{"4242424242424242", payments.ChargeSucceeded, "", "visa"},
		{"4000000000000002", payments.ChargeDeclined, "card_declined", "visa"},
		{"5555555555554444", payments.ChargeDeclined, "card_not_supported", "mastercard"},
	}
	for _, tc := range tests {
		t.Run(tc.number, func(t *testing.T) {
			f := newFixture(t)
			key := "rsv-fee:" + tc.brand + tc.number[12:]
			res, err := f.gw.Charge(t.Context(), charge(key, tc.number))
			if err != nil {
				t.Fatalf("Charge: %v", err)
			}
			if res.Outcome != tc.outcome || res.DeclineCode != tc.decline {
				t.Errorf("result = %+v, want outcome %v decline %q", res, tc.outcome, tc.decline)
			}
			if !strings.HasPrefix(res.ProviderRef, "sim_ch_") || len(res.ProviderRef) != len("sim_ch_")+24 {
				t.Errorf("ProviderRef = %q", res.ProviderRef)
			}
			if got := f.sleep.durations(); len(got) != 1 || got[0] != latency {
				t.Errorf("sleeps = %v, want [%v]", got, latency)
			}
			var brand, last4, row string
			err = f.pool.QueryRow(t.Context(),
				`select card_brand, card_last4, s::text from sim_gateway_charges s where idempotency_key = $1`, key).
				Scan(&brand, &last4, &row)
			if err != nil {
				t.Fatalf("row: %v", err)
			}
			if brand != tc.brand || last4 != tc.number[12:] {
				t.Errorf("row brand/last4 = %s/%s", brand, last4)
			}
			if strings.Contains(row, tc.number) {
				t.Errorf("row stores card data: %s", row)
			}
		})
	}
}

func TestSim_TimeoutCard(t *testing.T) {
	f := newFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	_, err := f.gw.Charge(ctx, charge("rsv-fee:timeout", "4000000000009995"))
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want context.DeadlineExceeded", err)
	}
	if got := f.sleep.durations(); len(got) != 1 || got[0] != timeoutDelay {
		t.Errorf("sleeps = %v, want [%v]", got, timeoutDelay)
	}
	var outcome string
	var delayed bool
	if err := f.pool.QueryRow(t.Context(),
		`select outcome, delayed from sim_gateway_charges where idempotency_key = 'rsv-fee:timeout'`).
		Scan(&outcome, &delayed); err != nil {
		t.Fatalf("row: %v", err)
	}
	if outcome != "succeeded" || !delayed {
		t.Errorf("row = %s delayed=%v, want succeeded delayed=true", outcome, delayed)
	}
	// The re-query sees the recorded success at once.
	res, err := f.gw.Charge(t.Context(), payments.ChargeRequest{IdempotencyKey: "rsv-fee:timeout"})
	if err != nil || res.Outcome != payments.ChargeSucceeded {
		t.Errorf("re-query = %+v, %v", res, err)
	}
}

func TestSim_RepeatAndRequery(t *testing.T) {
	f := newFixture(t)
	first, err := f.gw.Charge(t.Context(), charge("rsv-fee:a", "4242424242424242"))
	if err != nil {
		t.Fatal(err)
	}
	asked := len(f.sleep.durations())

	again, err := f.gw.Charge(t.Context(), charge("rsv-fee:a", "4000000000000002"))
	if err != nil || again != first {
		t.Errorf("repeat = %+v, %v; want %+v", again, err, first)
	}
	requery, err := f.gw.Charge(t.Context(), payments.ChargeRequest{IdempotencyKey: "rsv-fee:a"})
	if err != nil || requery != first {
		t.Errorf("requery = %+v, %v; want %+v", requery, err, first)
	}
	if got := len(f.sleep.durations()); got != asked {
		t.Errorf("repeat and re-query slept %d more times", got-asked)
	}
	if _, err := f.gw.Charge(t.Context(), payments.ChargeRequest{IdempotencyKey: "rsv-fee:never"}); !errors.Is(err, payments.ErrChargeUnknown) {
		t.Errorf("unknown key err = %v, want ErrChargeUnknown", err)
	}
	var n int
	if err := f.pool.QueryRow(t.Context(), `select count(*) from sim_gateway_charges`).Scan(&n); err != nil || n != 1 {
		t.Errorf("rows = %d, %v; want 1", n, err)
	}
}

func TestSim_RejectsBadRequests(t *testing.T) {
	f := newFixture(t)
	for name, req := range map[string]payments.ChargeRequest{
		"no key":      {AmountCents: 100, Card: card("4242424242424242")},
		"zero amount": {IdempotencyKey: "k1", Card: card("4242424242424242")},
		"short card":  {IdempotencyKey: "k2", AmountCents: 100, Card: card("12")},
	} {
		if _, err := f.gw.Charge(t.Context(), req); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}
