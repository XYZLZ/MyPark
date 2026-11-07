package vehicles_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/XYZLZ/MyPark/apps/server/internal/domain/apperr"
	"github.com/XYZLZ/MyPark/apps/server/internal/domain/authz"
	"github.com/XYZLZ/MyPark/apps/server/internal/domain/errcodes"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/vehicles"
	vehiclespg "github.com/XYZLZ/MyPark/apps/server/internal/features/vehicles/postgres"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/database"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/validation"
	"github.com/XYZLZ/MyPark/apps/server/internal/testsupport"
	"github.com/XYZLZ/MyPark/apps/server/internal/transport/http/authn"
	"github.com/XYZLZ/MyPark/apps/server/internal/transport/http/httpserver"
	"github.com/XYZLZ/MyPark/apps/server/internal/transport/http/middleware"
	"github.com/XYZLZ/MyPark/apps/server/internal/transport/http/router"
)

// tokenAuth resolves "Bearer <token>" to a registered principal.
type tokenAuth struct {
	mu sync.Mutex
	m  map[string]authz.Principal
}

func (a *tokenAuth) Authenticate(_ context.Context, bearer string) (authz.Principal, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	p, ok := a.m[bearer]
	if !ok {
		return authz.Principal{}, apperr.Unauthenticated(errcodes.AuthUnauthenticated)
	}
	return p, nil
}

// fakeChecker is a ReservationChecker with a switchable answer.
type fakeChecker struct {
	mu     sync.Mutex
	active bool
}

func (f *fakeChecker) HasActive(context.Context, int64) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.active, nil
}

func (f *fakeChecker) set(v bool) {
	f.mu.Lock()
	f.active = v
	f.mu.Unlock()
}

type env struct {
	t       *testing.T
	db      *testsupport.TestDB
	clock   *testsupport.FakeClock
	svc     *vehicles.Service
	checker *fakeChecker
	auth    *tokenAuth
	h       http.Handler
	max     int
}

const defaultMax = 5

func newEnv(t *testing.T) *env {
	t.Helper()
	db := testsupport.NewTestDB(t)
	clk := testsupport.NewFakeClock(time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC))
	chk := &fakeChecker{}
	svc := vehicles.NewService(database.NewTxRunner(db.App), vehiclespg.NewRepo(db.App), chk, defaultMax, clk)
	ta := &tokenAuth{m: map[string]authz.Principal{}}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	mux := http.NewServeMux()
	g := authn.New(ta, log)
	r := router.New(mux, router.Middlewares{
		ClientIP:  middleware.ClientIP(nil),
		RateLimit: router.PassthroughRateLimit,
		Authn:     g.Authn, Roles: g.Roles, Legal: g.Legal,
	})
	vehicles.NewHandler(svc, validation.New(), defaultMax, log).Register(r)
	return &env{t: t, db: db, clock: clk, svc: svc, checker: chk, auth: ta, max: defaultMax,
		h: httpserver.NewHandler(log, mux, nil)}
}

type user struct {
	ID       int64
	PublicID uuid.UUID
	P        authz.Principal
	Token    string
}

// newUser inserts a user and registers a bearer token for it.
func (e *env) newUser(name string, role authz.Role) user {
	e.t.Helper()
	u := user{Token: "tok-" + name}
	err := e.db.Super.QueryRow(e.t.Context(), `
		insert into users (email, first_name, last_name, role, status, email_verified_at)
		values ($1, 'Ana', 'P', $2, 'active', now()) returning id, public_id`,
		name+"@x.do", string(role)).Scan(&u.ID, &u.PublicID)
	if err != nil {
		e.t.Fatalf("insert user: %v", err)
	}
	u.P = authz.Principal{UserID: u.ID, UserPublicID: u.PublicID, Role: role}
	e.auth.mu.Lock()
	e.auth.m[u.Token] = u.P
	e.auth.mu.Unlock()
	return u
}

type resp struct{ *httptest.ResponseRecorder }

func (e *env) do(u *user, method, path string, body any) resp {
	e.t.Helper()
	var rd io.Reader
	switch b := body.(type) {
	case nil:
	case string:
		rd = strings.NewReader(b)
	default:
		raw, err := json.Marshal(b)
		if err != nil {
			e.t.Fatal(err)
		}
		rd = bytes.NewReader(raw)
	}
	req := httptest.NewRequest(method, path, rd)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if u != nil {
		req.Header.Set("Authorization", "Bearer "+u.Token)
	}
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	return resp{rec}
}

func (r resp) json() map[string]any {
	if r.Body.Len() == 0 {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal(r.Body.Bytes(), &m); err != nil {
		panic("body is not JSON: " + r.Body.String())
	}
	return m
}

// normalized returns the body with meta.request_id removed.
func (r resp) normalized() map[string]any {
	m := r.json()
	if meta, ok := m["meta"].(map[string]any); ok {
		delete(meta, "request_id")
	}
	return m
}

func (r resp) errCode() string {
	e, _ := r.json()["error"].(map[string]any)
	s, _ := e["code"].(string)
	return s
}

func (r resp) data() map[string]any {
	d, _ := r.json()["data"].(map[string]any)
	return d
}

func (r resp) list() []any {
	d, _ := r.json()["data"].([]any)
	return d
}

func vehicleBody(plate string) map[string]any {
	return map[string]any{"plate": plate, "make": "Toyota", "model": "Corolla", "color": "Gris"}
}

// create posts a vehicle and fails the test unless it is created.
func (e *env) create(u *user, plate string) string {
	e.t.Helper()
	r := e.do(u, http.MethodPost, "/v1/vehicles", vehicleBody(plate))
	if r.Code != http.StatusCreated {
		e.t.Fatalf("create %q: status %d body %s", plate, r.Code, r.Body.String())
	}
	return r.data()["id"].(string)
}

func (e *env) count(query string, args ...any) int64 {
	e.t.Helper()
	var n int64
	if err := e.db.Super.QueryRow(e.t.Context(), query, args...).Scan(&n); err != nil {
		e.t.Fatalf("query %q: %v", query, err)
	}
	return n
}

func plateN(i int) string { return fmt.Sprintf("PL%03d", i) }
