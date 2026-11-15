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
	// KeyDevice keys by the authenticated device's public id (DeviceAuth must
	// run first). Appended so the M1 values never move.
	KeyDevice
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
	// Scope picks the backing store. The zero value is ScopeShared, so every
	// rule that does not opt in stays on Postgres.
	Scope Scope
}

// Scope says which Limiter backs a Rule.
type Scope int

const (
	// ScopeShared counts across every API instance (Postgres).
	ScopeShared Scope = iota
	// ScopeLocal counts per API instance in memory (Memory): cheap, but N
	// instances allow up to N times the limit. Only for rules that guard
	// capacity, not abuse that needs an exact global count.
	ScopeLocal
)

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

// ClientNet returns the network that stands for one client: an IPv4 address
// (IPv4-mapped IPv6 is unmapped first) as is, an IPv6 address masked to its
// /64, since one subscriber or host usually holds a whole /64. The zone is
// dropped. The zero Addr stays zero.
func ClientNet(addr netip.Addr) netip.Addr {
	addr = addr.Unmap().WithZone("")
	if !addr.Is6() {
		return addr
	}
	p, err := addr.Prefix(64)
	if err != nil {
		return addr
	}
	return p.Addr()
}

// RuleIPKey returns the bucket key of addr for an IP-keyed rule. In-memory
// rules (ScopeLocal) key an IPv6 client by its /64 (ClientNet), so one /64
// cannot take a whole capacity limit with many addresses. Postgres rules
// (ScopeShared, M1) keep the per-address IPKey.
func RuleIPKey(rule Rule, addr netip.Addr) string {
	if rule.Scope != ScopeLocal || !addr.IsValid() {
		return IPKey(addr)
	}
	return ClientNet(addr).String()
}
