package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/jwt"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/ratelimit"
	"github.com/XYZLZ/MyPark/apps/server/internal/testsupport"
)

// loginHandler is the production handler with the real Postgres limiter.
func loginHandler(t *testing.T) (http.Handler, *testsupport.TestDB) {
	t.Helper()
	db := testsupport.NewTestDB(t)
	d := testDeps(readyHealth(), db.App)
	d.limiter = ratelimit.NewPostgres(db.App, d.clock)
	signer, err := jwt.NewSigner("test-1", bytes.Repeat([]byte("s"), 32), d.clock, jwt.AccessTTL)
	if err != nil {
		t.Fatalf("signer: %v", err)
	}
	d.signer = signer
	return newHandler(d), db
}

func login(h http.Handler, email string) *httptest.ResponseRecorder {
	body := `{"email":` + jsonString(email) + `,"password":"Zx9!kelp-orbit","client_kind":"web"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/auth/login", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "203.0.113.9:4000"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// Review focus 2: case and whitespace variants of one address share the
// same email-keyed buckets.
func TestLoginEmailRule_SharesBucketAcrossCaseVariants(t *testing.T) {
	h, _ := loginHandler(t)
	variants := []string{"Ana@X.do", " ana@x.do ", "ANA@x.DO", "ana@X.do", "\tAna@x.do"}
	for i, v := range variants {
		if rec := login(h, v); rec.Code != http.StatusUnauthorized {
			t.Fatalf("login %d (%q) = %d %s, want 401 invalid credentials", i+1, v, rec.Code, rec.Body)
		}
	}
	rec := login(h, " aNa@x.do")
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("sixth variant = %d %s, want 429", rec.Code, rec.Body)
	}
}

// Spec 15.2: the sixth login attempt in a minute is throttled, with the
// envelope and Retry-After.
func TestLogin_SixthAttempt429ViaHandler(t *testing.T) {
	h, _ := loginHandler(t)
	for i := range 5 {
		if rec := login(h, "ana@example.com"); rec.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d = %d %s, want 401", i+1, rec.Code, rec.Body)
		}
	}
	rec := login(h, "ana@example.com")
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode %q: %v", rec.Body, err)
	}
	assertEnvelope(t, rec, body, http.StatusTooManyRequests, "rate_limit.exceeded")
	if rec.Header().Get("Retry-After") == "" {
		t.Error("429 without Retry-After")
	}
	// Another address from the same IP is not blocked by the per-IP+email rule.
	if rec := login(h, "bob@example.com"); rec.Code != http.StatusUnauthorized {
		t.Errorf("other email = %d %s, want 401", rec.Code, rec.Body)
	}
}

// Route-level rules go through the router middleware: the fourth register
// from one IP in a minute is throttled before the handler runs.
func TestRegisterIPRule_ThrottlesViaRouterMiddleware(t *testing.T) {
	h, _ := loginHandler(t)
	post := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/v1/auth/register", strings.NewReader(`{}`))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = "198.51.100.4:1"
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}
	for i := range 3 {
		if rec := post(); rec.Code == http.StatusTooManyRequests {
			t.Fatalf("register %d throttled early", i+1)
		}
	}
	if rec := post(); rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") == "" {
		t.Fatalf("fourth register = %d, want 429 with Retry-After", rec.Code)
	}
}
