package users_test

import (
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	auditpg "github.com/XYZLZ/MyPark/apps/server/internal/features/audit/postgres"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/auth"
	authpg "github.com/XYZLZ/MyPark/apps/server/internal/features/auth/postgres"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/legal"
	legalpg "github.com/XYZLZ/MyPark/apps/server/internal/features/legal/postgres"
	notificationspg "github.com/XYZLZ/MyPark/apps/server/internal/features/notifications/postgres"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/users"
	userspg "github.com/XYZLZ/MyPark/apps/server/internal/features/users/postgres"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/vehicles"
	vehiclespg "github.com/XYZLZ/MyPark/apps/server/internal/features/vehicles/postgres"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/database"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/hashing"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/jwt"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/ratelimit"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/validation"
	"github.com/XYZLZ/MyPark/apps/server/internal/testsupport"
	"github.com/XYZLZ/MyPark/apps/server/internal/transport/http/authn"
	"github.com/XYZLZ/MyPark/apps/server/internal/transport/http/httpserver"
	"github.com/XYZLZ/MyPark/apps/server/internal/transport/http/middleware"
	"github.com/XYZLZ/MyPark/apps/server/internal/transport/http/router"
)

const password = "Original-pass-77"

var (
	fastParams = hashing.Params{Memory: 1024, Time: 1, Threads: 1, SaltLen: 16, KeyLen: 32}
	jwtSeed    = bytes.Repeat([]byte("s"), 32)
)

type env struct {
	t      *testing.T
	db     *testsupport.TestDB
	clock  *testsupport.FakeClock
	hasher hashing.PasswordHasher
	codes  hashing.CodeHasher
	h      http.Handler
}

// newEnv wires the real auth, vehicles and users features over a real
// database and the real authenticator, so tokens, sessions and the legal gate
// behave as in production.
func newEnv(t *testing.T) *env {
	t.Helper()
	db := testsupport.NewTestDB(t)
	if err := legal.SeedDocuments(t.Context(), db.Super); err != nil {
		t.Fatalf("seed legal: %v", err)
	}
	clk := testsupport.NewFakeClock(time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC))
	hasher := hashing.NewArgon2id(fastParams, 4)
	codes, err := hashing.NewCodeHasher(bytes.Repeat([]byte("p"), 32))
	if err != nil {
		t.Fatal(err)
	}
	signer, err := jwt.NewSigner("test", jwtSeed, clk, jwt.AccessTTL)
	if err != nil {
		t.Fatal(err)
	}
	tx := database.NewTxRunner(db.App)
	legalSvc := legal.NewService(legalpg.NewRepo(db.App), clk, time.Minute)
	authRepo := authpg.NewRepo(db.App)
	rec := auditpg.NewRecorder()
	notifier := notificationspg.NewNotifier(clk)
	authSvc := auth.NewService(auth.Deps{
		Signer: signer, Tx: tx, Repo: authRepo, Hasher: hasher, Codes: codes,
		Legal: legalSvc, Audit: rec, Notify: notifier, Clock: clk,
	})
	vehicleSvc := vehicles.NewService(tx, vehiclespg.NewRepo(db.App), vehicles.NoReservations{}, 5, clk)
	usersSvc := users.NewService(users.Deps{
		Tx: tx, Repo: userspg.NewRepo(db.App), Accounts: auth.NewAccounts(authSvc),
		Vehicles: vehicleSvc, Notify: notifier, Legal: legalSvc, Audit: rec, Clock: clk,
	})

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	verifier := jwt.NewVerifier(map[string]ed25519.PublicKey{
		"test": ed25519.NewKeyFromSeed(jwtSeed).Public().(ed25519.PublicKey),
	}, clk)
	g := authn.New(auth.NewAuthenticator(verifier, authRepo, legalSvc, clk), log, authn.WithLegal(legalSvc))
	mux := http.NewServeMux()
	r := router.New(mux, router.Middlewares{
		ClientIP:  middleware.ClientIP(nil),
		RateLimit: router.PassthroughRateLimit,
		Authn:     g.Authn, Roles: g.Roles, Legal: g.Legal,
	})
	v := validation.New()
	auth.NewHandler(authSvc, v, ratelimit.Noop{}, log).Register(r)
	vehicles.NewHandler(vehicleSvc, v, 5, log).Register(r)
	users.NewHandler(usersSvc, v, log).Register(r)
	return &env{t: t, db: db, clock: clk, hasher: hasher, codes: codes, h: httpserver.NewHandler(log, mux, nil)}
}

type user struct {
	ID       int64
	PublicID uuid.UUID
	Email    string
	Token    string
}

// newUser inserts an active user with a password and signs it in. terms is
// the accepted legal version, or "" for none (an admin).
func (e *env) newUser(email, role, terms string) user {
	e.t.Helper()
	h, err := e.hasher.Hash(e.t.Context(), password)
	if err != nil {
		e.t.Fatal(err)
	}
	var tv, tat any
	if terms != "" {
		tv, tat = terms, e.clock.Now()
	}
	u := user{Email: email}
	err = e.db.Super.QueryRow(e.t.Context(), `
		insert into users (email, password_hash, first_name, last_name, phone, role, status, locale, email_verified_at,
		                   terms_version, terms_accepted_at, privacy_version, privacy_accepted_at)
		values ($1, $2, 'Ana', 'Pérez', '+18095551234', $3, 'active', 'es-DO', $4, $5::text, $6::timestamptz, $5::text, $6::timestamptz)
		returning id, public_id`, email, h, role, e.clock.Now(), tv, tat).Scan(&u.ID, &u.PublicID)
	if err != nil {
		e.t.Fatalf("insert user: %v", err)
	}
	u.Token = e.login(email)
	return u
}

func (e *env) driver(email string) user { return e.newUser(email, "driver", legal.InitialVersion) }

// login signs in through the API and returns the access token.
func (e *env) login(email string) string {
	e.t.Helper()
	r := e.do("", http.MethodPost, "/v1/auth/login",
		map[string]any{"email": email, "password": password, "client_kind": "web"})
	if r.Code != http.StatusOK {
		e.t.Fatalf("login %s: %d %s", email, r.Code, r.Body)
	}
	return r.data()["access_token"].(string)
}

type resp struct{ *httptest.ResponseRecorder }

func (e *env) do(token, method, path string, body any) resp {
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
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
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

func (r resp) data() map[string]any {
	d, _ := r.json()["data"].(map[string]any)
	return d
}

func (r resp) errCode() string {
	e, _ := r.json()["error"].(map[string]any)
	s, _ := e["code"].(string)
	return s
}

func (e *env) exec(query string, args ...any) {
	e.t.Helper()
	if _, err := e.db.Super.Exec(e.t.Context(), query, args...); err != nil {
		e.t.Fatalf("exec %q: %v", query, err)
	}
}

func (e *env) count(query string, args ...any) int64 {
	e.t.Helper()
	var n int64
	if err := e.db.Super.QueryRow(e.t.Context(), query, args...).Scan(&n); err != nil {
		e.t.Fatalf("query %q: %v", query, err)
	}
	return n
}

func (e *env) text(query string, args ...any) string {
	e.t.Helper()
	var s *string
	if err := e.db.Super.QueryRow(e.t.Context(), query, args...).Scan(&s); err != nil {
		e.t.Fatalf("query %q: %v", query, err)
	}
	if s == nil {
		return "<null>"
	}
	return *s
}

// lastAudit returns the newest audit row of action.
func (e *env) lastAudit(action string) (after string, actor int64, entity string) {
	e.t.Helper()
	var a, en *string
	var ac *int64
	err := e.db.Super.QueryRow(e.t.Context(), `
		select after::text, actor_user_id, entity_id from audit_log where action = $1 order by id desc limit 1`,
		action).Scan(&a, &ac, &en)
	if err != nil {
		e.t.Fatalf("audit %s: %v", action, err)
	}
	if a != nil {
		after = *a
	}
	if ac != nil {
		actor = *ac
	}
	if en != nil {
		entity = *en
	}
	return after, actor, entity
}
