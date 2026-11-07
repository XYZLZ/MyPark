package auth_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/XYZLZ/MyPark/apps/server/internal/features/audit"
	auditpg "github.com/XYZLZ/MyPark/apps/server/internal/features/audit/postgres"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/auth"
	authpg "github.com/XYZLZ/MyPark/apps/server/internal/features/auth/postgres"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/legal"
	legalpg "github.com/XYZLZ/MyPark/apps/server/internal/features/legal/postgres"
	notificationspg "github.com/XYZLZ/MyPark/apps/server/internal/features/notifications/postgres"
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

const pathProbe = "/v1/test/probe"

var fastParams = hashing.Params{Memory: 1024, Time: 1, Threads: 1, SaltLen: 16, KeyLen: 32}

// spyHasher counts Hash calls and records the hash of every Verify call on a
// real, fast argon2id hasher.
type spyHasher struct {
	hashing.PasswordHasher
	hashes atomic.Int64

	mu       sync.Mutex
	verified []string
}

func (s *spyHasher) Verify(ctx context.Context, phc, pw string) (bool, bool, error) {
	s.mu.Lock()
	s.verified = append(s.verified, phc)
	s.mu.Unlock()
	return s.PasswordHasher.Verify(ctx, phc, pw)
}

// resetVerifies forgets the recorded Verify calls.
func (s *spyHasher) resetVerifies() {
	s.mu.Lock()
	s.verified = nil
	s.mu.Unlock()
}

// verifies returns the hashes passed to Verify since the last reset.
func (s *spyHasher) verifies() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.verified)
}

func (s *spyHasher) Hash(ctx context.Context, pw string) (string, error) {
	s.hashes.Add(1)
	return s.PasswordHasher.Hash(ctx, pw)
}

// fakeLimiter allows everything except the rules in deny, and records keys.
type fakeLimiter struct {
	mu         sync.Mutex
	deny       map[string]time.Duration
	keysByRule map[string][]string
}

func (l *fakeLimiter) Allow(_ context.Context, rule ratelimit.Rule, key string) (ratelimit.Decision, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.keysByRule == nil {
		l.keysByRule = map[string][]string{}
	}
	l.keysByRule[rule.Name] = append(l.keysByRule[rule.Name], key)
	if d, ok := l.deny[rule.Name]; ok {
		return ratelimit.Decision{Allowed: false, RetryAfter: d}, nil
	}
	return ratelimit.Decision{Allowed: true}, nil
}

type failingAudit struct{}

func (failingAudit) Record(context.Context, audit.Entry) error { return io.ErrUnexpectedEOF }

type env struct {
	t       *testing.T
	db      *testsupport.TestDB
	clock   *testsupport.FakeClock
	hasher  *spyHasher
	codes   hashing.CodeHasher
	limiter *fakeLimiter
	svc     *auth.Service
	h       http.Handler
	signer  *jwt.Signer
	// verifier trusts the test signer's key. authenticator runs over it.
	verifier      *jwt.Verifier
	legal         legal.Current
	authenticator *auth.Authenticator
}

// jwtSeed is the Ed25519 seed of the test signer.
var jwtSeed = bytes.Repeat([]byte("s"), 32)

func newEnv(t *testing.T, mod ...func(*auth.Deps)) *env {
	t.Helper()
	db := testsupport.NewTestDB(t)
	if err := legal.SeedDocuments(t.Context(), db.Super); err != nil {
		t.Fatalf("seed legal: %v", err)
	}
	clk := testsupport.NewFakeClock(time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC))
	hasher := &spyHasher{PasswordHasher: hashing.NewArgon2id(fastParams, 4)}
	codes, err := hashing.NewCodeHasher(bytes.Repeat([]byte("p"), 32))
	if err != nil {
		t.Fatal(err)
	}
	signer, err := jwt.NewSigner("test", jwtSeed, clk, jwt.AccessTTL)
	if err != nil {
		t.Fatal(err)
	}
	d := auth.Deps{
		Signer: signer,
		Tx:     database.NewTxRunner(db.App),
		Repo:   authpg.NewRepo(db.App),
		Hasher: hasher,
		Codes:  codes,
		Legal:  legal.NewService(legalpg.NewRepo(db.App), clk, time.Minute),
		Audit:  auditpg.NewRecorder(),
		Notify: notificationspg.NewNotifier(clk),
		Clock:  clk,
	}
	for _, m := range mod {
		m(&d)
	}
	svc := auth.NewService(d)
	lim := &fakeLimiter{}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	mux := http.NewServeMux()
	verifier := jwt.NewVerifier(map[string]ed25519.PublicKey{
		"test": ed25519.NewKeyFromSeed(jwtSeed).Public().(ed25519.PublicKey),
	}, clk)
	authenticator := auth.NewAuthenticator(verifier, d.Repo, d.Legal, clk)
	g := authn.New(authenticator, log, authn.WithLegal(d.Legal))
	r := router.New(mux, router.Middlewares{
		ClientIP:  middleware.ClientIP(nil),
		RateLimit: router.PassthroughRateLimit,
		Authn:     g.Authn, Roles: g.Roles, Legal: g.Legal,
	})
	auth.NewHandler(svc, validation.New(), lim, log).Register(r)
	// A gated probe route: 200 for any role that passes the legal gate.
	r.Handle("GET "+pathProbe, router.Policy{Roles: router.AnyRole}, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	return &env{t: t, db: db, clock: clk, hasher: hasher, codes: codes, limiter: lim, svc: svc, signer: signer,
		verifier: verifier, legal: d.Legal, authenticator: authenticator, h: httpserver.NewHandler(log, mux, nil)}
}

type resp struct {
	*httptest.ResponseRecorder
}

func (e *env) post(path string, body any) resp {
	e.t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		e.t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
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

func (r resp) headerNames() []string {
	var names []string
	for k := range r.Header() {
		names = append(names, k)
	}
	slices.Sort(names)
	return names
}

func (r resp) errCode() string {
	e, _ := r.json()["error"].(map[string]any)
	s, _ := e["code"].(string)
	return s
}

func registerBody(email string, mod ...func(map[string]any)) map[string]any {
	b := map[string]any{
		"email": email, "password": "Zx9!kelp-orbit", "first_name": "Ana", "last_name": "Pérez Gómez",
		"accepted_terms_version": legal.InitialVersion, "accepted_privacy_version": legal.InitialVersion,
	}
	for _, m := range mod {
		m(b)
	}
	return b
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

func (e *env) exec(query string, args ...any) {
	e.t.Helper()
	if _, err := e.db.Super.Exec(e.t.Context(), query, args...); err != nil {
		e.t.Fatalf("exec %q: %v", query, err)
	}
}

type userInfo struct {
	ID       int64
	PublicID uuid.UUID
}

func (e *env) user(email string) userInfo {
	e.t.Helper()
	var u userInfo
	err := e.db.Super.QueryRow(e.t.Context(), `select id, public_id from users where email = $1`, email).Scan(&u.ID, &u.PublicID)
	if err != nil {
		e.t.Fatalf("user %s: %v", email, err)
	}
	return u
}

// outboxCode returns the code in the newest verify_email message to email.
func (e *env) outboxCode(email string) string {
	e.t.Helper()
	return e.text(`select payload->>'code' from outbox_messages
		where to_address = $1 and template = 'verify_email' order by id desc limit 1`, email)
}

// userSnapshot is the whole users row as text, to prove a row is untouched.
func (e *env) userSnapshot(email string) string {
	e.t.Helper()
	return e.text(`select u::text from users u where email = $1`, email)
}

func (e *env) insertUser(email, role, status string, withPassword bool) userInfo {
	e.t.Helper()
	var hash any
	if withPassword {
		h, err := e.hasher.PasswordHasher.Hash(e.t.Context(), "Original-pass-77")
		if err != nil {
			e.t.Fatal(err)
		}
		hash = h
	}
	var verified any
	if status == "active" {
		verified = e.clock.Now()
	}
	var u userInfo
	err := e.db.Super.QueryRow(e.t.Context(), `
		insert into users (email, password_hash, first_name, last_name, role, status, locale, email_verified_at)
		values ($1, $2, 'Orig', 'Inal', $3, $4, 'en', $5) returning id, public_id`,
		email, hash, role, status, verified).Scan(&u.ID, &u.PublicID)
	if err != nil {
		e.t.Fatalf("insert user: %v", err)
	}
	return u
}

func (e *env) insertCode(u userInfo, purpose, code string) {
	e.t.Helper()
	e.exec(`insert into auth_codes (user_id, purpose, code_hash, expires_at) values ($1, $2, $3, $4)`,
		u.ID, purpose, e.codes.Hash(purpose, u.PublicID, code), e.clock.Now().Add(48*time.Hour))
}

func wrongCode(code string) string {
	if code == "123456" {
		return "654321"
	}
	return "123456"
}

// loginAccess logs email in with the default test password and returns the
// access token.
func (e *env) loginAccess(email string) string {
	e.t.Helper()
	rec := e.post(pathLogin, loginBody(email, loginPassword))
	if rec.Code != http.StatusOK {
		e.t.Fatalf("login %s: status %d, body %s", email, rec.Code, rec.Body)
	}
	data, _ := rec.json()["data"].(map[string]any)
	tok, _ := data["access_token"].(string)
	return tok
}

// do sends method path with an optional bearer token and no body.
func (e *env) do(method, path, token string) resp {
	e.t.Helper()
	req := httptest.NewRequest(method, path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	return resp{rec}
}

// claims verifies a token the test itself obtained.
func (e *env) claims(token string) jwt.Claims {
	e.t.Helper()
	c, err := e.verifier.Verify(token)
	if err != nil {
		e.t.Fatalf("verify own token: %v", err)
	}
	return c
}
