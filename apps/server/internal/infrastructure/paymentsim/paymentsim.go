// Package paymentsim is the simulated payment gateway (design 9.5). It keeps
// its state in sim_gateway_charges, so a charge survives restarts and both
// calls are idempotent on their key. Only brand and last four are stored.
package paymentsim

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/XYZLZ/MyPark/apps/server/internal/features/payments"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/clock"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/database/dbgen"
)

// The magic cards of the simulator (M3B-11).
const (
	cardSucceeds = "4242424242424242"
	cardDeclined = "4000000000000002"
	cardTimeout  = "4000000000009995"
)

// errTimedOut is the unknown-outcome error when the simulated delay ends
// before the caller's context does.
var errTimedOut = fmt.Errorf("paymentsim: gateway timed out: %w", context.DeadlineExceeded)

// refBytes of randomness encode to 24 base32 characters.
const refBytes = 15

var refEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)

// Config tunes the simulator. The zero value of each field is usable.
type Config struct {
	// Clock supplies business timestamps. Nil means the system clock.
	Clock clock.Clock
	// Latency is the delay before a normal charge answers.
	Latency time.Duration
	// TimeoutDelay is how long the timeout card waits for the context.
	TimeoutDelay time.Duration
	// Sleep waits d or until ctx ends, returning ctx.Err() in that case.
	// Nil means a real timer. Tests inject one that never waits.
	Sleep func(ctx context.Context, d time.Duration) error
	// Rand feeds provider refs. Nil means crypto/rand.
	Rand io.Reader
}

// Gateway implements payments.Gateway over Postgres.
type Gateway struct {
	pool  *pgxpool.Pool
	cfg   Config
	clock clock.Clock
}

// New returns a simulated gateway recording its state in pool.
func New(pool *pgxpool.Pool, cfg Config) *Gateway {
	if cfg.Sleep == nil {
		cfg.Sleep = realSleep
	}
	if cfg.Rand == nil {
		cfg.Rand = rand.Reader
	}
	clk := cfg.Clock
	if clk == nil {
		clk = clock.System{}
	}
	return &Gateway{pool: pool, cfg: cfg, clock: clk}
}

func realSleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// Charge records the charge once per key and answers from the first record,
// so a repeat (any card) gets the first result at once. A nil Card is a
// re-query: the recorded result, or payments.ErrChargeUnknown.
func (g *Gateway) Charge(ctx context.Context, req payments.ChargeRequest) (payments.ChargeResult, error) {
	q := dbgen.New(g.pool)
	if req.IdempotencyKey == "" {
		return payments.ChargeResult{}, errors.New("paymentsim: idempotency key required")
	}
	if req.Card == nil {
		row, err := q.GetSimCharge(ctx, req.IdempotencyKey)
		if errors.Is(err, pgx.ErrNoRows) {
			return payments.ChargeResult{}, payments.ErrChargeUnknown
		}
		if err != nil {
			return payments.ChargeResult{}, fmt.Errorf("paymentsim: read charge: %w", err)
		}
		return resultOf(row.Outcome, row.DeclineCode, row.ProviderRef), nil
	}
	if req.AmountCents <= 0 || len(req.Card.Number) < 4 {
		return payments.ChargeResult{}, errors.New("paymentsim: invalid charge request")
	}

	outcome, decline, delayed := decide(req.Card.Number)
	ref, err := g.ref("sim_ch_")
	if err != nil {
		return payments.ChargeResult{}, err
	}
	row, err := q.InsertSimCharge(ctx, dbgen.InsertSimChargeParams{
		IdempotencyKey: req.IdempotencyKey,
		AmountCents:    req.AmountCents,
		Outcome:        outcome,
		DeclineCode:    pgtype.Text{String: decline, Valid: decline != ""},
		CardBrand:      payments.Brand(req.Card.Number),
		CardLast4:      req.Card.Last4(),
		ProviderRef:    ref,
		Delayed:        delayed,
		Now:            pgtype.Timestamptz{Time: g.clock.Now(), Valid: true},
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows): // the key exists: answer with its first result
		prev, err := q.GetSimCharge(ctx, req.IdempotencyKey)
		if err != nil {
			return payments.ChargeResult{}, fmt.Errorf("paymentsim: read charge: %w", err)
		}
		return resultOf(prev.Outcome, prev.DeclineCode, prev.ProviderRef), nil
	case err != nil:
		return payments.ChargeResult{}, fmt.Errorf("paymentsim: record charge: %w", err)
	}

	if row.Delayed {
		// The charge is recorded but the answer never arrives in time.
		if err := g.cfg.Sleep(ctx, g.cfg.TimeoutDelay); err != nil {
			return payments.ChargeResult{}, err
		}
		return payments.ChargeResult{}, errTimedOut
	}
	if g.cfg.Latency > 0 {
		if err := g.cfg.Sleep(ctx, g.cfg.Latency); err != nil {
			return payments.ChargeResult{}, err
		}
	}
	return resultOf(row.Outcome, row.DeclineCode, row.ProviderRef), nil
}

// decide maps a card to its recorded outcome (design 9.5).
func decide(number string) (outcome, decline string, delayed bool) {
	switch number {
	case cardSucceeds:
		return "succeeded", "", false
	case cardDeclined:
		return "declined", "card_declined", false
	case cardTimeout:
		return "succeeded", "", true
	}
	return "declined", "card_not_supported", false
}

func resultOf(outcome string, decline pgtype.Text, ref string) payments.ChargeResult {
	res := payments.ChargeResult{Outcome: payments.ChargeDeclined, DeclineCode: decline.String, ProviderRef: ref}
	if outcome == "succeeded" {
		res.Outcome = payments.ChargeSucceeded
	}
	return res
}

// ref returns prefix plus 24 random base32 characters.
func (g *Gateway) ref(prefix string) (string, error) {
	b := make([]byte, refBytes)
	if _, err := io.ReadFull(g.cfg.Rand, b); err != nil {
		return "", fmt.Errorf("paymentsim: provider ref: %w", err)
	}
	return prefix + refEncoding.EncodeToString(b), nil
}
