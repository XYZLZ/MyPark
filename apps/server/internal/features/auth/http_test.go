package auth_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/XYZLZ/MyPark/apps/server/internal/features/auth"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/ratelimit"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/validation"
	"github.com/XYZLZ/MyPark/apps/server/internal/transport/http/middleware"
	"github.com/XYZLZ/MyPark/apps/server/internal/transport/http/router"
)

func TestHandler_RoutesAndPolicies(t *testing.T) {
	mux := http.NewServeMux()
	r := router.New(mux, router.Middlewares{
		ClientIP:  middleware.ClientIP(nil),
		RateLimit: router.PassthroughRateLimit,
	})
	auth.NewHandler(auth.NewService(auth.Deps{}), validation.New(), ratelimit.Noop{}, nil).Register(r)
	want := map[string]string{
		"POST /v1/auth/register":      ratelimit.RegisterIP.Name,
		"POST /v1/auth/verify":        ratelimit.CodeSubmitIP.Name,
		"POST /v1/auth/verify/resend": ratelimit.CodeRequestIP.Name,
		// Login limits per ip+email and per email in the handler, so the
		// route itself carries no rule.
		"POST /v1/auth/login": "",
	}
	got := map[string]string{}
	for _, rt := range r.Routes() {
		if !rt.Policy.Public {
			t.Errorf("%s: policy = %+v, want public", rt.Pattern, rt.Policy)
			continue
		}
		if rt.Policy.Limit != nil {
			got[rt.Pattern] = rt.Policy.Limit.Name
		} else {
			got[rt.Pattern] = ""
		}
	}
	for pattern, rule := range want {
		if got[pattern] != rule {
			t.Errorf("%s limit = %q, want %q", pattern, got[pattern], rule)
		}
	}
}

// Requests that fail before the service is reached need no database.
func TestHandler_RejectsBadInputBeforeService(t *testing.T) {
	mux := http.NewServeMux()
	r := router.New(mux, router.Middlewares{ClientIP: middleware.ClientIP(nil), RateLimit: router.PassthroughRateLimit})
	auth.NewHandler(auth.NewService(auth.Deps{}), validation.New(), ratelimit.Noop{}, nil).Register(r)

	tests := []struct {
		name, path, body, ct string
		status               int
		code                 string
	}{
		{"empty body", "/v1/auth/register", ``, "application/json", 400, "http.invalid_json"},
		{"null body", "/v1/auth/verify", `null`, "application/json", 400, "http.invalid_json"},
		{"wrong content type", "/v1/auth/verify/resend", `{}`, "text/plain", 415, "http.unsupported_media_type"},
		{"verify non-numeric code", "/v1/auth/verify", `{"email":"a@b.co","code":"12ab56"}`, "application/json", 422, "validation.failed"},
		{"verify short code", "/v1/auth/verify", `{"email":"a@b.co","code":"123"}`, "application/json", 422, "validation.failed"},
		{"resend bad email", "/v1/auth/verify/resend", `{"email":"nope"}`, "application/json", 422, "validation.failed"},
		{"register missing fields", "/v1/auth/register", `{}`, "application/json", 422, "validation.failed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, tt.path, strings.NewReader(tt.body))
			req.Header.Set("Content-Type", tt.ct)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)
			if rec.Code != tt.status || !strings.Contains(rec.Body.String(), tt.code) {
				t.Errorf("status = %d body = %s, want %d %s", rec.Code, rec.Body, tt.status, tt.code)
			}
		})
	}
}
