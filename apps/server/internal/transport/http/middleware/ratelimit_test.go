package middleware_test

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/XYZLZ/MyPark/apps/server/internal/domain/authz"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/ratelimit"
	"github.com/XYZLZ/MyPark/apps/server/internal/testsupport"
	"github.com/XYZLZ/MyPark/apps/server/internal/transport/http/middleware"
	"github.com/XYZLZ/MyPark/apps/server/internal/transport/http/reqctx"
)

type stubLimiter struct {
	dec ratelimit.Decision
	err error
	key string
}

func (s *stubLimiter) Allow(_ context.Context, _ ratelimit.Rule, key string) (ratelimit.Decision, error) {
	s.key = key
	return s.dec, s.err
}

// serve runs one request through RateLimit(rule) and reports whether the
// wrapped handler ran.
func serve(l ratelimit.Limiter, rule ratelimit.Rule, logs *testsupport.SyncBuffer, mod func(*http.Request) *http.Request) (*httptest.ResponseRecorder, bool) {
	ran := false
	h := middleware.RateLimit(l, slog.New(slog.NewJSONHandler(logs, nil)))(rule)(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			ran = true
			w.WriteHeader(http.StatusNoContent)
		}))
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req = req.WithContext(reqctx.WithClientIP(req.Context(), netip.MustParseAddr("203.0.113.9")))
	if mod != nil {
		req = mod(req)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec, ran
}

func asUser(id uuid.UUID) func(*http.Request) *http.Request {
	return func(r *http.Request) *http.Request {
		return r.WithContext(authz.WithPrincipal(r.Context(), authz.Principal{UserPublicID: id, Role: authz.RoleDriver}))
	}
}

func TestRateLimit_FailClosedOnAuth(t *testing.T) {
	var logs testsupport.SyncBuffer
	rec, ran := serve(&stubLimiter{err: errors.New("db down")}, ratelimit.CodeSubmitIP, &logs, nil)
	if ran {
		t.Error("handler ran on a limiter error for a fail-closed rule")
	}
	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if e, _ := body["error"].(map[string]any); rec.Code != http.StatusInternalServerError || e["code"] != "internal.unexpected" {
		t.Fatalf("response = %d %s, want 500 internal.unexpected", rec.Code, rec.Body)
	}
}

func TestRateLimit_FailOpenOnVehicles(t *testing.T) {
	var logs testsupport.SyncBuffer
	rec, ran := serve(&stubLimiter{err: errors.New("db down")}, ratelimit.VehicleWriteUser, &logs, asUser(uuid.New()))
	if !ran || rec.Code != http.StatusNoContent {
		t.Fatalf("ran=%v status=%d, want the handler to run on a fail-open error", ran, rec.Code)
	}
	if !strings.Contains(logs.String(), `"level":"ERROR"`) {
		t.Errorf("limiter error not logged at error level: %s", logs.String())
	}
}

func TestRateLimit_429Envelope(t *testing.T) {
	var logs testsupport.SyncBuffer
	l := &stubLimiter{dec: ratelimit.Decision{Allowed: false, RetryAfter: 1500 * time.Millisecond}}
	rec, ran := serve(l, ratelimit.CodeSubmitIP, &logs, nil)
	if ran {
		t.Error("handler ran on a denied request")
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	e, _ := body["error"].(map[string]any)
	details, _ := e["details"].(map[string]any)
	if rec.Code != http.StatusTooManyRequests || e["code"] != "rate_limit.exceeded" {
		t.Fatalf("response = %d %s, want 429 rate_limit.exceeded", rec.Code, rec.Body)
	}
	secs, _ := details["retry_after_seconds"].(float64)
	if got := rec.Header().Get("Retry-After"); got != strconv.Itoa(int(secs)) || got != "2" {
		t.Errorf("Retry-After = %q, details.retry_after_seconds = %v; want both 2", got, secs)
	}
}

func TestRateLimit_KeysAndLogsHaveNoPII(t *testing.T) {
	var logs testsupport.SyncBuffer
	l := &stubLimiter{dec: ratelimit.Decision{Allowed: false, RetryAfter: time.Second}}
	serve(l, ratelimit.CodeSubmitIP, &logs, nil)
	if l.key != "203.0.113.9" {
		t.Errorf("KeyIP key = %q, want the client IP", l.key)
	}
	out := logs.String()
	if strings.Contains(out, "203.0.113.9") {
		t.Errorf("throttle log leaks the IP: %s", out)
	}
	if !strings.Contains(out, `"level":"WARN"`) || !strings.Contains(out, ratelimit.CodeSubmitIP.Name) {
		t.Errorf("throttle log lacks warn level or rule name: %s", out)
	}
	if h := hex.EncodeToString(ratelimit.HashKey(ratelimit.CodeSubmitIP, "203.0.113.9")); !strings.Contains(out, h) {
		t.Errorf("throttle log lacks the hashed key %s: %s", h, out)
	}
}

func TestRateLimit_UserKeyIsPublicID(t *testing.T) {
	var logs testsupport.SyncBuffer
	uid := uuid.New()
	l := &stubLimiter{dec: ratelimit.Decision{Allowed: true}}
	rec, ran := serve(l, ratelimit.VehicleWriteUser, &logs, asUser(uid))
	if !ran || rec.Code != http.StatusNoContent || l.key != uid.String() {
		t.Errorf("ran=%v status=%d key=%q, want the user public id", ran, rec.Code, l.key)
	}
}

func TestRateLimit_MissingPrincipalFollowsFailMode(t *testing.T) {
	var logs testsupport.SyncBuffer
	l := &stubLimiter{dec: ratelimit.Decision{Allowed: true}}
	// DeleteAccountUser fails closed: no principal means no request.
	rec, ran := serve(l, ratelimit.DeleteAccountUser, &logs, nil)
	if ran || rec.Code != http.StatusInternalServerError {
		t.Errorf("fail-closed without principal: ran=%v status=%d, want 500", ran, rec.Code)
	}
}

func TestRateLimit_UnknownIPUsesSharedKey(t *testing.T) {
	var logs testsupport.SyncBuffer
	l := &stubLimiter{dec: ratelimit.Decision{Allowed: true}}
	serve(l, ratelimit.CodeSubmitIP, &logs, func(r *http.Request) *http.Request {
		return r.WithContext(reqctx.WithClientIP(r.Context(), netip.Addr{}))
	})
	if l.key != "ip:unknown" {
		t.Errorf("key = %q, want ip:unknown", l.key)
	}
}
