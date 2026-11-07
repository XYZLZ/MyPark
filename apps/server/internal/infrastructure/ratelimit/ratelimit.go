// Package ratelimit defines the rate-limiting port and the MyPark rules.
// Adapters (e.g. Postgres-backed) implement Limiter elsewhere.
package ratelimit

import (
	"context"
	"net/netip"
	"time"
)

// KeyKind says how the bucket key of a Rule is derived.
type KeyKind int

const (
	// KeyIP keys by client IP (built by the router middleware).
	KeyIP KeyKind = iota
	// KeyUser keys by the authenticated user (needs the principal).
	KeyUser
	// KeyCustom keys are built by the handler (e.g. IP+email).
	KeyCustom
)

// Rule is one rate-limit rule.
type Rule struct {
	Name   string
	Limit  int
	Window time.Duration
	Key    KeyKind
	// Prefix namespaces the bucket key.
	Prefix string
	// FailOpen allows the request when the limiter errors; otherwise it fails closed.
	FailOpen bool
}

// Decision is the outcome of a limiter check.
type Decision struct {
	Allowed    bool
	RetryAfter time.Duration
}

// Limiter is the rate-limit port.
type Limiter interface {
	Allow(ctx context.Context, rule Rule, key string) (Decision, error)
}

// Noop allows everything. It is a placeholder until a real adapter is wired.
type Noop struct{}

// Allow always allows.
func (Noop) Allow(context.Context, Rule, string) (Decision, error) {
	return Decision{Allowed: true}, nil
}

// unknownIPKey is the shared bucket for requests without a valid client IP,
// so they never fall into an empty-string bucket.
const unknownIPKey = "ip:unknown"

// IPKey returns the bucket key for addr. An invalid addr (ClientIP middleware
// missing or unparsable peer) maps to a fixed shared key. IPv4-mapped IPv6
// addresses are unmapped so both forms share a bucket.
func IPKey(addr netip.Addr) string {
	if !addr.IsValid() {
		return unknownIPKey
	}
	return addr.Unmap().String()
}
