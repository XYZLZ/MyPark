package ratelimit_test

import (
	"context"
	"errors"
	"testing"

	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/ratelimit"
)

type recorder struct {
	calls []string
	dec   ratelimit.Decision
	err   error
}

func (r *recorder) Allow(_ context.Context, rule ratelimit.Rule, key string) (ratelimit.Decision, error) {
	r.calls = append(r.calls, rule.Name+"/"+key)
	return r.dec, r.err
}

func TestComposite_RoutesByScope(t *testing.T) {
	shared, local := &recorder{}, &recorder{}
	c := ratelimit.NewComposite(shared, local)

	_, _ = c.Allow(t.Context(), ratelimit.LoginEmail, "a")
	if len(shared.calls) != 1 || len(local.calls) != 0 {
		t.Fatalf("shared rule: shared=%v local=%v", shared.calls, local.calls)
	}
	_, _ = c.Allow(t.Context(), ratelimit.DeviceIP, "1.2.3.4")
	if len(shared.calls) != 1 || len(local.calls) != 1 || local.calls[0] != "device_ip/1.2.3.4" {
		t.Fatalf("local rule: shared=%v local=%v", shared.calls, local.calls)
	}
}

func TestComposite_PropagatesDecisionAndError(t *testing.T) {
	boom := errors.New("boom")
	c := ratelimit.NewComposite(&recorder{err: boom}, &recorder{dec: ratelimit.Decision{Allowed: true}})
	if _, err := c.Allow(t.Context(), ratelimit.LoginEmail, "a"); !errors.Is(err, boom) {
		t.Errorf("shared error = %v", err)
	}
	if d, err := c.Allow(t.Context(), ratelimit.SSEConnectIP, "a"); err != nil || !d.Allowed {
		t.Errorf("local = %+v, %v", d, err)
	}
}
