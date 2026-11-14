package router_test

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

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
		ClientIP:    r.mw("clientip"),
		Authn:       r.mw("authn"),
		Legal:       r.mw("legal"),
		Roles:       func([]authz.Role) func(http.Handler) http.Handler { return r.mw("roles") },
		DeviceAuth:  r.mw("deviceauth"),
		DeviceLimit: ratelimit.DeviceRule(100),
		RateLimit: func(rule ratelimit.Rule) func(http.Handler) http.Handler {
			switch {
			case rule.Key == ratelimit.KeyUser:
				return r.mw("rl:user")
			case rule.Key == ratelimit.KeyDevice || rule == ratelimit.DeviceIP:
				// The device rules are named so the chain test pins which rule runs where.
				return r.mw("rl:" + rule.Name)
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

func TestHandle_PanicsOnHandlerKeyedOrUnknownLimitKey(t *testing.T) {
	rt := router.New(http.NewServeMux(), (&recorder{}).middlewares())
	mustPanic(t, "POST /v1/custom", func() {
		rt.Handle("POST /v1/custom", router.Policy{Public: true, Limit: &ratelimit.LoginIPEmail}, noop)
	})
	unknown := ratelimit.Rule{Name: "x", Limit: 1, Window: time.Minute, Key: ratelimit.KeyKind(99)}
	mustPanic(t, "POST /v1/unknown", func() {
		rt.Handle("POST /v1/unknown", router.Policy{Public: true, Limit: &unknown}, noop)
	})
}

func TestHandle_DevicePolicyChainOrder(t *testing.T) {
	rec := &recorder{}
	mux := http.NewServeMux()
	rt := router.New(mux, rec.middlewares())
	rt.Handle("POST /v1/devices/events", router.Policy{Device: true}, func(http.ResponseWriter, *http.Request) {
		rec.calls = append(rec.calls, "handler")
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/devices/events", nil)
	// A user bearer token never reaches the user gates on a device route.
	req.Header.Set("Authorization", "Bearer user-token")
	mux.ServeHTTP(httptest.NewRecorder(), req)
	want := []string{"clientip", "rl:device_ip", "deviceauth", "rl:device", "handler"}
	if !slices.Equal(rec.calls, want) {
		t.Errorf("order = %v, want %v", rec.calls, want)
	}
}

func TestHandle_PanicsOnBadDevicePolicy(t *testing.T) {
	const devPath = "POST /v1/devices/events"
	tests := []struct {
		name    string
		pattern string
		policy  router.Policy
		mod     func(*router.Middlewares)
		want    string
	}{
		{"device+public", devPath, router.Policy{Device: true, Public: true}, nil, "exactly one"},
		{"device+roles", devPath, router.Policy{Device: true, Roles: router.AnyRole}, nil, "exactly one"},
		{"device+legal exempt", devPath, router.Policy{Device: true, LegalExempt: true}, nil, "LegalExempt"},
		{"device+limit", devPath, router.Policy{Device: true, Limit: &ratelimit.DeviceIP}, nil, "Limit"},
		{"device off /v1/devices/", "POST /v1/lots", router.Policy{Device: true}, nil, "/v1/devices/"},
		{"device prefix lookalike", "POST /v1/devicesx", router.Policy{Device: true}, nil, "/v1/devices/"},
		{"nil DeviceAuth", devPath, router.Policy{Device: true}, func(m *router.Middlewares) { m.DeviceAuth = nil }, "DeviceAuth"},
		{"nil RateLimit", devPath, router.Policy{Device: true}, func(m *router.Middlewares) { m.RateLimit = nil }, "RateLimit"},
		{"zero DeviceLimit", devPath, router.Policy{Device: true}, func(m *router.Middlewares) { m.DeviceLimit = ratelimit.Rule{} }, "DeviceLimit"},
		{"non-device path keeps the M1 rule", "GET /v1/x", router.Policy{}, nil, "exactly one"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mw := (&recorder{}).middlewares()
			if tc.mod != nil {
				tc.mod(&mw)
			}
			rt := router.New(http.NewServeMux(), mw)
			mustPanic(t, tc.want, func() { rt.Handle(tc.pattern, tc.policy, noop) })
		})
	}
}

func TestHandle_DeviceRouteNeedsNoUserGates(t *testing.T) {
	mw := (&recorder{}).middlewares()
	mw.Authn, mw.Roles, mw.Legal = nil, nil, nil
	rt := router.New(http.NewServeMux(), mw)
	rt.Handle("POST /v1/devices/events", router.Policy{Device: true}, noop)
	if got := rt.Routes(); len(got) != 1 || !got[0].Policy.Device {
		t.Errorf("routes = %+v", got)
	}
}

func TestHandle_PanicsOnDevicePathWithoutDevicePolicy(t *testing.T) {
	rt := router.New(http.NewServeMux(), (&recorder{}).middlewares())
	mustPanic(t, "Device policy", func() {
		rt.Handle("POST /v1/devices/events", router.Policy{Public: true}, noop)
	})
}
