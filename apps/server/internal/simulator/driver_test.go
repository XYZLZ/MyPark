package simulator_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/XYZLZ/MyPark/apps/server/internal/devicefleet"
	"github.com/XYZLZ/MyPark/apps/server/internal/domain/secret"
	"github.com/XYZLZ/MyPark/apps/server/internal/simulator"
	"github.com/XYZLZ/MyPark/apps/server/internal/testsupport"
	"github.com/XYZLZ/MyPark/apps/server/internal/testsupport/fakeclock"
)

const qrToken = "QRTOKEN-never-log-me-0123456789"

var driverFleet = secret.Bytes(bytes.Repeat([]byte("f"), 32))

// fakeAPI is a minimal driver API: login, refresh (which rotates, so a reused
// refresh token is refused) and an authenticated vehicles list.
type fakeAPI struct {
	mu                sync.Mutex
	access, refresh   string
	seq               int
	logins, refreshes atomic.Int32
	loginBody         string
	failPay           bool
}

func (f *fakeAPI) issue(w http.ResponseWriter) {
	f.seq++
	f.access, f.refresh = fmt.Sprintf("access-token-%d", f.seq), fmt.Sprintf("refresh-token-%d", f.seq)
	_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
		"access_token": f.access, "refresh_token": f.refresh, "expires_in": 900,
	}})
}

func apiErr(w http.ResponseWriter, status int, code string) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": code}})
}

func (f *fakeAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch r.URL.Path {
	case "/v1/auth/login":
		var in map[string]string
		_ = json.NewDecoder(r.Body).Decode(&in)
		f.loginBody = in["client_kind"] + " " + in["email"]
		if in["password"] != devicefleet.SimDriverPassword(driverFleet, in["email"]).Reveal() {
			apiErr(w, 401, "auth.invalid_credentials")
			return
		}
		f.logins.Add(1)
		f.issue(w)
	case "/v1/auth/refresh":
		var in map[string]string
		_ = json.NewDecoder(r.Body).Decode(&in)
		if in["refresh_token"] != f.refresh {
			apiErr(w, 401, "auth.session_revoked")
			return
		}
		f.refreshes.Add(1)
		f.issue(w)
	case "/v1/reservations/" + uuid.Nil.String() + "/qr":
		if r.Header.Get("Authorization") != "Bearer "+f.access {
			apiErr(w, 401, "auth.unauthorized")
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"token": qrToken}})
	case "/v1/vehicles":
		if r.Header.Get("Authorization") != "Bearer "+f.access {
			apiErr(w, 401, "auth.unauthorized")
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]any{{"id": uuid.Nil.String()}}})
	case "/v1/reservations/" + uuid.Nil.String() + "/pay":
		if f.failPay {
			apiErr(w, 500, "internal.unexpected")
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"status": "confirmed"}})
	default:
		http.NotFound(w, r)
	}
}

func newDriverClient(t *testing.T, api *fakeAPI, log *slog.Logger) (*simulator.DriverClient, *fakeclock.Clock) {
	t.Helper()
	srv := httptest.NewServer(api)
	t.Cleanup(srv.Close)
	clk := fakeclock.New(time.Date(2026, 12, 1, 12, 0, 0, 0, time.UTC))
	return simulator.NewDriverClient(simulator.DriverClientConfig{
		BaseURL: srv.URL, Fleet: driverFleet, Clock: clk, LotID: uuid.New(), Log: log,
	}), clk
}

func TestDriverClient_LoginIsMobileWithDerivedPassword(t *testing.T) {
	api := &fakeAPI{}
	c, _ := newDriverClient(t, api, nil)
	if err := c.Login(t.Context(), 7); err != nil {
		t.Fatalf("Login: %v", err)
	}
	if api.loginBody != "mobile sim-driver-07@mypark.test" {
		t.Errorf("login = %q", api.loginBody)
	}
}

func TestDriverClient_ParallelUnauthorizedRefreshesOnce(t *testing.T) {
	api := &fakeAPI{}
	c, _ := newDriverClient(t, api, nil)
	if err := c.Login(t.Context(), 1); err != nil {
		t.Fatal(err)
	}
	// The server forgets the access token: every call now answers 401 once.
	api.mu.Lock()
	api.access = "rotated-elsewhere"
	api.mu.Unlock()
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for range 8 {
		wg.Go(func() {
			_, err := c.Vehicle(t.Context(), 1)
			errs <- err
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Errorf("Vehicle: %v", err)
		}
	}
	if got := api.refreshes.Load(); got != 1 {
		t.Errorf("refreshes = %d, want 1 (single flight)", got)
	}
}

func TestDriverClient_ExpiredTokenRefreshesFromTheClock(t *testing.T) {
	api := &fakeAPI{}
	c, clk := newDriverClient(t, api, nil)
	if err := c.Login(t.Context(), 1); err != nil {
		t.Fatal(err)
	}
	clk.Advance(16 * time.Minute)
	if _, err := c.Vehicle(t.Context(), 1); err != nil {
		t.Fatalf("Vehicle: %v", err)
	}
	if api.refreshes.Load() != 1 || api.logins.Load() != 1 {
		t.Errorf("refreshes=%d logins=%d, want 1 and 1", api.refreshes.Load(), api.logins.Load())
	}
}

func TestDriverClient_NeverLogsSecrets(t *testing.T) {
	api := &fakeAPI{}
	logs := &testsupport.SyncBuffer{}
	c, _ := newDriverClient(t, api, slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	ctx := context.Background()
	if _, err := c.Vehicle(ctx, 2); err != nil {
		t.Fatal(err)
	}
	tok, err := c.QR(ctx, 2, uuid.Nil)
	if err != nil || tok.Reveal() != qrToken {
		t.Fatalf("QR = %v, %v", tok, err)
	}
	if fmt.Sprintf("%v %+v %#v", tok, tok, tok) != "[redacted] [redacted] [redacted]" {
		t.Error("QR token prints itself")
	}
	api.failPay = true
	_, payErr := c.Pay(ctx, 2, uuid.Nil, "key-0001-abcdef")
	if payErr == nil {
		t.Fatal("Pay succeeded against a failing API")
	}
	password := devicefleet.SimDriverPassword(driverFleet, devicefleet.SimDriverEmail(2)).Reveal()
	out := logs.String() + payErr.Error() + fmt.Sprintf("%v %+v %#v %v", c, c, c, simulator.DriverClientConfig{Fleet: driverFleet})
	for _, secretText := range []string{password, "access-token-1", "refresh-token-1", "4242424242424242", string(driverFleet), qrToken} {
		if strings.Contains(out, secretText) {
			t.Errorf("output contains secret %q...", secretText[:6])
		}
	}
	if !strings.Contains(logs.String(), "/v1/auth/login") {
		t.Errorf("expected the request to be logged, got %q", logs.String())
	}
}
