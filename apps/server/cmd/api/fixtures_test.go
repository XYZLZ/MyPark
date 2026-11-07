package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/XYZLZ/MyPark/apps/server/internal/config"
	"github.com/XYZLZ/MyPark/apps/server/internal/domain/authz"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/legal"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/hashing"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/jwt"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/ratelimit"
	"github.com/XYZLZ/MyPark/apps/server/internal/testsupport"
	"github.com/XYZLZ/MyPark/apps/server/internal/transport/http/router"
)

// fixturePassword is the password of every fixture user.
const fixturePassword = "Zx9!kelp-orbit"

// fastHashParams keep argon2id cheap in tests.
var fastHashParams = hashing.Params{Memory: 1024, Time: 1, Threads: 1, SaltLen: 16, KeyLen: 32}

// apiEnv is the production handler over a fresh database.
type apiEnv struct {
	t      *testing.T
	db     *testsupport.TestDB
	d      deps
	h      http.Handler
	routes []router.Route
	clk    *testsupport.FakeClock
	cfg    config.Config
	signer *jwt.Signer
	hasher hashing.PasswordHasher
	hash   string
	seq    atomic.Int64
}

// newAPIEnv builds the handler exactly as run does, with a fast hasher and
// the legal documents seeded. mods adjust the dependencies (limiter, log).
func newAPIEnv(t *testing.T, mods ...func(*deps)) *apiEnv {
	t.Helper()
	db := testsupport.NewTestDB(t)
	if err := legal.SeedDocuments(t.Context(), db.Super); err != nil {
		t.Fatalf("seed legal: %v", err)
	}
	env := map[string]string{
		"JWT_SIGNING_KEY":  config.DevJWTSigningKey,
		"JWT_KEY_ID":       "dev-1",
		"AUTH_CODE_PEPPER": config.DevAuthCodePepper,
		"DATABASE_URL":     db.AppURL,
	}
	cfg, err := config.Load(func(k string) string { return env[k] })
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	clk := testsupport.NewFakeClock(time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC))
	signer, err := jwt.NewSigner(cfg.JWTKeyID(), cfg.JWTSigningSeed(), clk, jwt.AccessTTL)
	if err != nil {
		t.Fatalf("signer: %v", err)
	}
	codes, err := hashing.NewCodeHasher(cfg.AuthCodePepper())
	if err != nil {
		t.Fatalf("code hasher: %v", err)
	}
	hasher := hashing.NewArgon2id(fastHashParams, 2)
	hash, err := hasher.Hash(t.Context(), fixturePassword)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	d := deps{
		log:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		cfg:     cfg,
		pool:    db.App,
		clock:   clk,
		health:  readyHealth(),
		signer:  signer,
		hasher:  hasher,
		codes:   codes,
		limiter: ratelimit.Noop{},
	}
	for _, m := range mods {
		m(&d)
	}
	h, routes := newHandlerWithRoutes(d)
	return &apiEnv{t: t, db: db, d: d, h: h, routes: routes, clk: clk, cfg: cfg, signer: signer, hasher: hasher, hash: hash}
}

// serve sends req through the full handler with an optional bearer token.
func (e *apiEnv) serve(req *http.Request, token string) *httptest.ResponseRecorder {
	e.t.Helper()
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if req.RemoteAddr == "" || req.RemoteAddr == "192.0.2.1:1234" {
		req.RemoteAddr = "203.0.113.7:5555"
	}
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	return rec
}

// jsonReq builds a request with a JSON body (nil for none).
func jsonReq(method, path string, body any) *http.Request {
	var rd io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			panic(err)
		}
		rd = bytes.NewReader(raw)
	}
	req := httptest.NewRequest(method, path, rd)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req
}

// decodeBody decodes a JSON object body; nil for an empty body.
func decodeBody(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	if rec.Body.Len() == 0 {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatalf("body %q is not a JSON object: %v", rec.Body.String(), err)
	}
	return m
}

// errCode returns error.code of an error envelope, "" otherwise.
func errCode(body map[string]any) string {
	e, _ := body["error"].(map[string]any)
	c, _ := e["code"].(string)
	return c
}

// principal is a fixture user with a live session and its access token.
type principal struct {
	userID  int64
	public  uuid.UUID
	session uuid.UUID
	email   string
	token   string
}

type userSpec struct {
	role    authz.Role
	status  string
	legalOK bool // accepted the current legal versions
	revoked bool // the session is revoked
}

// mint inserts a user and a session straight into the database and signs an
// access token for it. No login runs, so a fixture costs two inserts.
func (e *apiEnv) mint(s userSpec) principal {
	e.t.Helper()
	n := e.seq.Add(1)
	p := principal{email: fmt.Sprintf("fixture%d@example.test", n)}
	var terms, privacy any
	var at any
	if s.legalOK {
		terms, privacy, at = legal.InitialVersion, legal.InitialVersion, e.clk.Now()
	}
	var verified any
	if s.status == "active" || s.status == "suspended" {
		verified = e.clk.Now()
	}
	err := e.db.Super.QueryRow(e.t.Context(), `
		insert into users (email, password_hash, first_name, last_name, role, status, locale, email_verified_at,
		                   terms_version, terms_accepted_at, privacy_version, privacy_accepted_at)
		values ($1, $2, 'Fixture', 'User', $3, $4, 'en', $5, $6, $7, $8, $7)
		returning id, public_id`,
		p.email, e.hash, string(s.role), s.status, verified, terms, at, privacy).Scan(&p.userID, &p.public)
	if err != nil {
		e.t.Fatalf("insert user: %v", err)
	}
	var revokedAt, reason any
	if s.revoked {
		revokedAt, reason = e.clk.Now(), "logout"
	}
	err = e.db.Super.QueryRow(e.t.Context(), `
		insert into auth_sessions (user_id, client_kind, absolute_expires_at, revoked_at, revoked_reason)
		values ($1, 'web', $2, $3, $4) returning public_id`,
		p.userID, e.clk.Now().Add(24*time.Hour), revokedAt, reason).Scan(&p.session)
	if err != nil {
		e.t.Fatalf("insert session: %v", err)
	}
	p.token, _, err = e.signer.Sign(p.public, p.session, s.role)
	if err != nil {
		e.t.Fatalf("sign: %v", err)
	}
	return p
}

// fixture holds the principals of one authorization-matrix run, plus an
// owned vehicle and token variants. Every route gets a fresh one, so a
// destructive request (logout, delete) never disturbs the next.
type fixture struct {
	env       *apiEnv
	owner     principal // active driver, owns vehicleID
	other     principal // active driver who owns nothing here
	admin     principal
	pending   principal // active driver who owes a legal re-acceptance
	suspended principal
	revoked   principal // driver whose session is revoked
	vehicleID uuid.UUID
	tampered  string // owner token with a corrupted signature
	expired   string // owner token past its expiry
	algNone   string // owner claims, "alg":"none", no signature
	uniq      int64
}

func (e *apiEnv) newFixture() *fixture {
	e.t.Helper()
	f := &fixture{env: e, uniq: e.seq.Add(1)}
	active := userSpec{role: authz.RoleDriver, status: "active", legalOK: true}
	f.owner = e.mint(active)
	f.other = e.mint(active)
	f.admin = e.mint(userSpec{role: authz.RoleAdmin, status: "active"})
	f.pending = e.mint(userSpec{role: authz.RoleDriver, status: "active"})
	f.suspended = e.mint(userSpec{role: authz.RoleDriver, status: "suspended", legalOK: true})
	f.revoked = e.mint(userSpec{role: authz.RoleDriver, status: "active", legalOK: true, revoked: true})

	rec := e.serve(jsonReq(http.MethodPost, "/v1/vehicles", map[string]any{
		"plate": fmt.Sprintf("FX%d-%d", f.uniq, f.uniq%97), "make": "Toyota", "model": "Corolla", "color": "Blue",
	}), f.owner.token)
	if rec.Code != http.StatusCreated {
		e.t.Fatalf("create fixture vehicle: %d %s", rec.Code, rec.Body)
	}
	data, _ := decodeBody(e.t, rec)["data"].(map[string]any)
	id, _ := data["id"].(string)
	f.vehicleID = uuid.MustParse(id)

	f.tampered = tamperSignature(f.owner.token)
	f.expired = e.expiredToken(f.owner)
	f.algNone = algNoneToken(f.owner.token)
	return f
}

// tamperSignature flips the first character of the signature segment.
func tamperSignature(tok string) string {
	i := strings.LastIndexByte(tok, '.')
	c := tok[i+1]
	repl := byte('A')
	if c == 'A' {
		repl = 'B'
	}
	return tok[:i+1] + string(repl) + tok[i+2:]
}

// expiredToken signs the owner's claims with a clock far in the past.
func (e *apiEnv) expiredToken(p principal) string {
	e.t.Helper()
	past := testsupport.NewFakeClock(e.clk.Now().Add(-3 * time.Hour))
	s, err := jwt.NewSigner(e.cfg.JWTKeyID(), e.cfg.JWTSigningSeed(), past, jwt.AccessTTL)
	if err != nil {
		e.t.Fatal(err)
	}
	tok, _, err := s.Sign(p.public, p.session, authz.RoleDriver)
	if err != nil {
		e.t.Fatal(err)
	}
	return tok
}

// algNoneToken keeps the payload and replaces the header and signature.
func algNoneToken(tok string) string {
	parts := strings.Split(tok, ".")
	enc := base64.RawURLEncoding
	header := enc.EncodeToString([]byte(`{"alg":"none","typ":"JWT","kid":"dev-1"}`))
	return header + "." + parts[1] + "."
}

// email returns an address nobody used yet.
func (f *fixture) email() string {
	return fmt.Sprintf("new%d-%d@example.test", f.uniq, f.env.seq.Add(1))
}
