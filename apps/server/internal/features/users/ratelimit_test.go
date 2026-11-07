package users_test

import (
	"net/http"
	"testing"

	"github.com/XYZLZ/MyPark/apps/server/internal/domain/authz"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/users"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/ratelimit"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/validation"
	"github.com/XYZLZ/MyPark/apps/server/internal/transport/http/middleware"
	"github.com/XYZLZ/MyPark/apps/server/internal/transport/http/router"
)

// DELETE /v1/me verifies a password, so a stolen access token must not be
// able to brute-force it: the route is throttled per user and fails closed.
func TestDeleteMe_PolicyHasFailClosedUserLimit(t *testing.T) {
	pass := func(next http.Handler) http.Handler { return next }
	r := router.New(http.NewServeMux(), router.Middlewares{
		ClientIP: middleware.ClientIP(nil), RateLimit: router.PassthroughRateLimit,
		Authn: pass, Legal: pass,
		Roles: func([]authz.Role) func(http.Handler) http.Handler { return pass },
	})
	users.NewHandler(users.NewService(users.Deps{}), validation.New(), nil).Register(r)
	for _, rt := range r.Routes() {
		if rt.Pattern != "DELETE /v1/me" {
			continue
		}
		l := rt.Policy.Limit
		if l == nil || l.Name != ratelimit.DeleteAccountUser.Name || l.Key != ratelimit.KeyUser || l.FailOpen {
			t.Fatalf("DELETE /v1/me limit = %+v, want the fail-closed user rule", l)
		}
		return
	}
	t.Fatal("DELETE /v1/me not registered")
}
