package authn_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/XYZLZ/MyPark/apps/server/internal/domain/apperr"
	"github.com/XYZLZ/MyPark/apps/server/internal/domain/authz"
	"github.com/XYZLZ/MyPark/apps/server/internal/domain/errcodes"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/legal"
	"github.com/XYZLZ/MyPark/apps/server/internal/transport/http/authn"
	"github.com/XYZLZ/MyPark/apps/server/internal/transport/http/httpserver"
	"github.com/XYZLZ/MyPark/apps/server/internal/transport/http/middleware"
	"github.com/XYZLZ/MyPark/apps/server/internal/transport/http/router"
)

// fakeAuth maps a bearer token to a principal or an error and records the
// tokens it was asked about.
type fakeAuth struct {
	principals map[string]authz.Principal
	errs       map[string]error
	seen       []string
}

func (f *fakeAuth) Authenticate(_ context.Context, bearer string) (authz.Principal, error) {
	f.seen = append(f.seen, bearer)
	if err, ok := f.errs[bearer]; ok {
		return authz.Principal{}, err
	}
	if p, ok := f.principals[bearer]; ok {
		return p, nil
	}
	return authz.Principal{}, apperr.Unauthenticated(errcodes.AuthUnauthenticated)
}

type fakeLegal struct{ legal.Current }

func (fakeLegal) Versions(context.Context) (legal.Versions, error) {
	return legal.Versions{
		Terms:   legal.Document{Version: "2026-11-16"},
		Privacy: legal.Document{Version: "2026-11-17"},
	}, nil
}

var (
	driverID = uuid.MustParse("0192f000-0000-7000-8000-000000000001")
	driver   = authz.Principal{UserID: 1, UserPublicID: driverID, Role: authz.RoleDriver, SessionID: 1}
	pending  = authz.Principal{UserID: 2, UserPublicID: uuid.New(), Role: authz.RoleDriver, SessionID: 2, LegalPending: true}
	admin    = authz.Principal{UserID: 3, UserPublicID: uuid.New(), Role: authz.RoleAdmin, SessionID: 3}
)

type harness struct {
	h   http.Handler
	log *bytes.Buffer
	fa  *fakeAuth
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	fa := &fakeAuth{
		principals: map[string]authz.Principal{"driver": driver, "pending": pending, "admin": admin},
		errs: map[string]error{
			"revoked": apperr.Unauthenticated(errcodes.AuthSessionRevoked),
			"boom":    errors.New("db down"),
		},
	}
	logs := &bytes.Buffer{}
	log := slog.New(slog.NewJSONHandler(logs, nil))
	g := authn.New(fa, log, authn.WithLegal(fakeLegal{}))
	mux := http.NewServeMux()
	r := router.New(mux, router.Middlewares{
		ClientIP: middleware.ClientIP(nil), Authn: g.Authn, Roles: g.Roles, Legal: g.Legal,
	})
	ok := func(w http.ResponseWriter, r *http.Request) {
		p, _ := authz.PrincipalFrom(r.Context())
		_, _ = io.WriteString(w, string(p.Role))
	}
	r.Handle("GET /probe", router.Policy{Roles: router.AnyRole}, ok)
	r.Handle("GET /exempt", router.Policy{Roles: router.AnyRole, LegalExempt: true}, ok)
	r.Handle("GET /v1/admin/probe", router.Policy{Roles: router.AdminOnly}, ok)
	return &harness{h: httpserver.NewHandler(log, mux, nil), log: logs, fa: fa}
}

func (h *harness) get(path, authHeader string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}
	rec := httptest.NewRecorder()
	h.h.ServeHTTP(rec, req)
	return rec
}

func errBody(t *testing.T, rec *httptest.ResponseRecorder) (string, map[string]any) {
	t.Helper()
	var body struct {
		Error struct {
			Code    string         `json:"code"`
			Details map[string]any `json:"details"`
		} `json:"error"`
		Meta map[string]any `json:"meta"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body %q is not JSON: %v", rec.Body, err)
	}
	if body.Meta["request_id"] == "" || body.Meta["request_id"] != rec.Header().Get(middleware.HeaderRequestID) {
		t.Errorf("meta.request_id = %v, want the X-Request-Id header", body.Meta["request_id"])
	}
	return body.Error.Code, body.Error.Details
}

func TestGates_MissingOrMalformedBearer(t *testing.T) {
	h := newHarness(t)
	for _, tc := range []struct{ name, header string }{
		{"no header", ""},
		{"basic scheme", "Basic x"},
		{"empty bearer", "Bearer "},
		{"scheme only", "Bearer"},
		{"token with space", "Bearer a b"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := h.get("/probe", tc.header)
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401", rec.Code)
			}
			if code, _ := errBody(t, rec); code != "auth.unauthenticated" {
				t.Errorf("code = %q, want auth.unauthenticated", code)
			}
		})
	}
	if len(h.fa.seen) != 0 {
		t.Errorf("the authenticator saw %v, want it untouched by malformed headers", h.fa.seen)
	}
}

func TestGates_BearerSchemeIsCaseInsensitive(t *testing.T) {
	h := newHarness(t)
	if rec := h.get("/probe", "bearer driver"); rec.Code != http.StatusOK {
		t.Errorf("lowercase scheme: status = %d, want 200", rec.Code)
	}
}

func TestGates_AuthenticatorErrorsReachTheEnvelope(t *testing.T) {
	h := newHarness(t)
	rec := h.get("/probe", "Bearer revoked")
	if code, _ := errBody(t, rec); rec.Code != http.StatusUnauthorized || code != "auth.session_revoked" {
		t.Errorf("revoked: %d %s", rec.Code, code)
	}
	rec = h.get("/probe", "Bearer boom")
	if code, _ := errBody(t, rec); rec.Code != http.StatusInternalServerError || code != "internal.unexpected" {
		t.Errorf("internal: %d %s", rec.Code, code)
	}
	if strings.Contains(rec.Body.String(), "db down") {
		t.Error("the cause of an internal error reached the client")
	}
}

func TestGates_RoleDenied(t *testing.T) {
	h := newHarness(t)
	rec := h.get("/v1/admin/probe", "Bearer driver")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
	if code, _ := errBody(t, rec); code != "auth.forbidden" {
		t.Errorf("code = %q, want auth.forbidden", code)
	}
	if rec := h.get("/v1/admin/probe", "Bearer admin"); rec.Code != http.StatusOK {
		t.Errorf("admin on admin route: status = %d, want 200", rec.Code)
	}
}

func TestGates_LegalBlocksNonExempt(t *testing.T) {
	h := newHarness(t)
	rec := h.get("/probe", "Bearer pending")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
	code, details := errBody(t, rec)
	if code != "legal.acceptance_required" || details["terms_version"] != "2026-11-16" || details["privacy_version"] != "2026-11-17" {
		t.Errorf("code %q details %v", code, details)
	}
	if rec := h.get("/exempt", "Bearer pending"); rec.Code != http.StatusOK {
		t.Errorf("LegalExempt route: status = %d, want 200", rec.Code)
	}
	if rec := h.get("/probe", "Bearer driver"); rec.Code != http.StatusOK {
		t.Errorf("up-to-date driver: status = %d, want 200", rec.Code)
	}
	if rec := h.get("/probe", "Bearer admin"); rec.Code != http.StatusOK {
		t.Errorf("admin: status = %d, want 200", rec.Code)
	}
}

func TestGates_RoleGateRunsBeforeLegalGate(t *testing.T) {
	h := newHarness(t)
	rec := h.get("/v1/admin/probe", "Bearer pending")
	if code, _ := errBody(t, rec); rec.Code != http.StatusForbidden || code != "auth.forbidden" {
		t.Errorf("pending driver on admin route: %d %s, want 403 auth.forbidden", rec.Code, code)
	}
}

func TestGates_AccessLogHasUserID(t *testing.T) {
	h := newHarness(t)
	h.get("/probe", "Bearer driver")
	h.get("/probe", "Bearer nobody")
	var withUser, without int
	for line := range strings.Lines(h.log.String()) {
		var e map[string]any
		if json.Unmarshal([]byte(line), &e) != nil || e["msg"] != "http request" {
			continue
		}
		switch e["status"] {
		case float64(200):
			if e["user_id"] != driverID.String() {
				t.Errorf("user_id = %v, want %s", e["user_id"], driverID)
			}
			withUser++
		case float64(401):
			if _, ok := e["user_id"]; ok {
				t.Errorf("a failed authentication logged user_id %v", e["user_id"])
			}
			without++
		}
	}
	if withUser != 1 || without != 1 {
		t.Errorf("access log lines: %d authenticated, %d rejected, want 1 and 1\n%s", withUser, without, h.log)
	}
	if strings.Contains(h.log.String(), "Bearer") {
		t.Errorf("the access log holds an Authorization value: %s", h.log)
	}
}

func TestGates_LegalWithoutSourceFailsClosed(t *testing.T) {
	discard := slog.New(slog.NewTextHandler(io.Discard, nil))
	fa := &fakeAuth{principals: map[string]authz.Principal{"pending": pending}}
	g := authn.New(fa, discard)
	mux := http.NewServeMux()
	r := router.New(mux, router.Middlewares{ClientIP: middleware.ClientIP(nil), Authn: g.Authn, Roles: g.Roles, Legal: g.Legal})
	r.Handle("GET /probe", router.Policy{Roles: router.AnyRole}, func(http.ResponseWriter, *http.Request) {})
	req := httptest.NewRequest(http.MethodGet, "/probe", nil)
	req.Header.Set("Authorization", "Bearer pending")
	rec := httptest.NewRecorder()
	httpserver.NewHandler(discard, mux, nil).ServeHTTP(rec, req)
	if code, details := errBody(t, rec); rec.Code != http.StatusForbidden || code != "legal.acceptance_required" || len(details) != 0 {
		t.Errorf("%d %s %v, want a bare 403 legal.acceptance_required", rec.Code, code, details)
	}
}
