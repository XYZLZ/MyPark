package router_test

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/XYZLZ/MyPark/apps/server/internal/domain/authz"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/ratelimit"
	"github.com/XYZLZ/MyPark/apps/server/internal/transport/http/httpserver"
	"github.com/XYZLZ/MyPark/apps/server/internal/transport/http/router"
)

type recorder struct{ calls []string }

func (r *recorder) mw(name string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			r.calls = append(r.calls, name)
			next.ServeHTTP(w, req)
		})
	}
}

func (r *recorder) middlewares() router.Middlewares {
	return router.Middlewares{
		ClientIP: r.mw("clientip"),
		Authn:    r.mw("authn"),
		Legal:    r.mw("legal"),
		Roles:    func([]authz.Role) func(http.Handler) http.Handler { return r.mw("roles") },
		RateLimit: func(rule ratelimit.Rule) func(http.Handler) http.Handler {
			if rule.Key == ratelimit.KeyUser {
				return r.mw("rl:user")
			}
			return r.mw("rl:ip")
		},
	}
}

func mustPanic(t *testing.T, contains string, fn func()) {
	t.Helper()
	defer func() {
		v := recover()
		if v == nil {
			t.Fatal("expected panic")
		}
		if s, _ := v.(string); !strings.Contains(s, contains) {
			t.Errorf("panic %q does not contain %q", s, contains)
		}
	}()
	fn()
}

func noop(http.ResponseWriter, *http.Request) {}

func TestHandle_PanicsWithoutPolicy(t *testing.T) {
	rt := router.New(http.NewServeMux(), (&recorder{}).middlewares())
	mustPanic(t, "GET /v1/nopolicy", func() { rt.Handle("GET /v1/nopolicy", router.Policy{}, noop) })
}

func TestHandle_PanicsPublicWithRoles(t *testing.T) {
	rt := router.New(http.NewServeMux(), (&recorder{}).middlewares())
	mustPanic(t, "GET /v1/x", func() {
		rt.Handle("GET /v1/x", router.Policy{Public: true, Roles: router.AnyRole}, noop)
	})
}

func TestHandle_PanicsAdminPathNotAdminOnly(t *testing.T) {
	rt := router.New(http.NewServeMux(), (&recorder{}).middlewares())
	mustPanic(t, "/v1/admin/x", func() {
		rt.Handle("GET /v1/admin/x", router.Policy{Roles: router.AnyRole}, noop)
	})
	rt.Handle("GET /v1/admin/y", router.Policy{Roles: router.AdminOnly}, noop)
}

func TestHandle_PanicsWhenRequiredMiddlewareNil(t *testing.T) {
	mw := (&recorder{}).middlewares()
	mw.Authn = nil
	rt := router.New(http.NewServeMux(), mw)
	mustPanic(t, "Authn", func() {
		rt.Handle("GET /v1/x", router.Policy{Roles: router.DriverOnly}, noop)
	})

	mw = (&recorder{}).middlewares()
	mw.RateLimit = nil
	rt = router.New(http.NewServeMux(), mw)
	mustPanic(t, "RateLimit", func() {
		rt.Handle("POST /v1/y", router.Policy{Public: true, Limit: &ratelimit.RegisterIP}, noop)
	})
}

func TestHandle_PanicsUserRuleOnPublicRoute(t *testing.T) {
	rt := router.New(http.NewServeMux(), (&recorder{}).middlewares())
	mustPanic(t, "POST /v1/x", func() {
		rt.Handle("POST /v1/x", router.Policy{Public: true, Limit: &ratelimit.VehicleWriteUser}, noop)
	})
}

func TestHandle_ChainOrder(t *testing.T) {
	tests := []struct {
		name   string
		policy router.Policy
		want   []string
	}{
		{"ip rule", router.Policy{Roles: router.DriverOnly, Limit: &ratelimit.RefreshIP},
			[]string{"clientip", "rl:ip", "authn", "roles", "legal", "handler"}},
		{"user rule", router.Policy{Roles: router.DriverOnly, Limit: &ratelimit.VehicleWriteUser},
			[]string{"clientip", "authn", "roles", "legal", "rl:user", "handler"}},
		{"legal exempt", router.Policy{Roles: router.AnyRole, LegalExempt: true},
			[]string{"clientip", "authn", "roles", "handler"}},
		{"public", router.Policy{Public: true, Limit: &ratelimit.RegisterIP},
			[]string{"clientip", "rl:ip", "handler"}},
		{"public no limit", router.Policy{Public: true},
			[]string{"clientip", "handler"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := &recorder{}
			mux := http.NewServeMux()
			rt := router.New(mux, rec.middlewares())
			rt.Handle("POST /v1/x", tc.policy, func(http.ResponseWriter, *http.Request) {
				rec.calls = append(rec.calls, "handler")
			})
			mux.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/v1/x", nil))
			if !slices.Equal(rec.calls, tc.want) {
				t.Errorf("order = %v, want %v", rec.calls, tc.want)
			}
		})
	}
}

func TestHandle_PatternVisibleToAccessLog(t *testing.T) {
	var buf strings.Builder
	log := slog.New(slog.NewTextHandler(&buf, nil))
	mux := http.NewServeMux()
	rt := router.New(mux, (&recorder{}).middlewares())
	rt.Handle("POST /v1/x", router.Policy{Public: true}, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	h := httpserver.NewHandler(log, mux, nil)
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/v1/x", nil))
	out := buf.String()
	if !strings.Contains(out, `route="POST /v1/x"`) || strings.Contains(out, "unmatched") {
		t.Errorf("access log = %s", out)
	}
}

func TestRoutes_RegistryIsCompleteAndSorted(t *testing.T) {
	rt := router.New(http.NewServeMux(), (&recorder{}).middlewares())
	rt.Handle("POST /v1/b", router.Policy{Public: true}, noop)
	rt.Handle("GET /v1/a", router.Policy{Roles: router.AnyRole}, noop)
	rt.Handle("GET /v1/admin/c", router.Policy{Roles: router.AdminOnly}, noop)
	got := rt.Routes()
	var pats []string
	for _, r := range got {
		pats = append(pats, r.Pattern)
	}
	want := []string{"GET /v1/a", "GET /v1/admin/c", "POST /v1/b"}
	if !slices.Equal(pats, want) {
		t.Errorf("routes = %v, want %v", pats, want)
	}
	if !got[2].Policy.Public || !slices.Equal(got[1].Policy.Roles, router.AdminOnly) {
		t.Errorf("policies not preserved: %+v", got)
	}
}

func TestPassthroughRateLimit_RegistersAndServes(t *testing.T) {
	mw := (&recorder{}).middlewares()
	mw.RateLimit = router.PassthroughRateLimit
	mux := http.NewServeMux()
	rt := router.New(mux, mw)
	rt.Handle("POST /v1/x", router.Policy{Public: true, Limit: &ratelimit.RegisterIP}, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/x", nil))
	if rec.Code != http.StatusNoContent {
		t.Errorf("status = %d", rec.Code)
	}
}
