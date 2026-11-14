package ratelimit

import "context"

// Composite routes each rule to the shared or the local Limiter by Rule.Scope.
type Composite struct {
	shared, local Limiter
}

// NewComposite returns a Limiter that sends ScopeLocal rules to local and
// every other rule to shared.
func NewComposite(shared, local Limiter) Composite {
	return Composite{shared: shared, local: local}
}

// Allow implements Limiter.
func (c Composite) Allow(ctx context.Context, rule Rule, key string) (Decision, error) {
	if rule.Scope == ScopeLocal {
		return c.local.Allow(ctx, rule, key)
	}
	return c.shared.Allow(ctx, rule, key)
}
