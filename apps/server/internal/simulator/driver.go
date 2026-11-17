package simulator

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/XYZLZ/MyPark/apps/server/internal/devicefleet"
	"github.com/XYZLZ/MyPark/apps/server/internal/domain/secret"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/clock"
)

// simCardNumber is the gateway's always-succeeding test card.
const simCardNumber = "4242424242424242"

// expirySkew refreshes an access token slightly before it expires.
const expirySkew = 10 * time.Second

// DriverAPIError is a non-2xx answer of the public driver API. It carries the
// status and the error code only, never a body.
type DriverAPIError struct {
	Status int
	Code   string
}

func (e *DriverAPIError) Error() string {
	return fmt.Sprintf("simulator: driver API answered %d %s", e.Status, e.Code)
}

// DriverClientConfig configures a DriverClient.
type DriverClientConfig struct {
	// BaseURL is the API origin.
	BaseURL string
	// HTTP defaults to a client with a timeout that never follows redirects.
	HTTP *http.Client
	// Fleet derives every driver password (devicefleet.SimDriverPassword).
	Fleet secret.Bytes
	// Clock decides when an access token has expired and dates the card.
	Clock clock.Clock
	// LotID is the lot quotes and reservations are made for.
	LotID uuid.UUID
	// Log receives one debug line per request (driver, method, path, status).
	// Bodies and tokens are never logged. Nil discards.
	Log *slog.Logger
}

// Quote is the priced hold window of an arrival.
type Quote struct {
	ArrivalAt          time.Time
	HoldStart, HoldEnd time.Time
	FeeCents           int64
}

// Reservation is the part of a reservation the simulator drives on.
type Reservation struct {
	ID        uuid.UUID
	Status    string
	FeeCents  int64
	ArrivalAt time.Time
}

type driverSession struct {
	mu        sync.Mutex
	access    secret.String
	refresh   secret.String
	expiresAt time.Time
	// gen counts token renewals, so concurrent 401s renew only once.
	gen int
}

// DriverClient drives the public driver API as the seeded simulated drivers
// (M3B-44). Tokens live in memory only. It is safe for concurrent use.
type DriverClient struct {
	cfg DriverClientConfig
	hc  *http.Client
	log *slog.Logger

	mu       sync.Mutex
	sessions map[int]*driverSession
}

// NewDriverClient returns a client over c.
func NewDriverClient(c DriverClientConfig) *DriverClient {
	hc := c.HTTP
	if hc == nil {
		hc = &http.Client{
			Timeout:       requestTimeout,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		}
	}
	log := c.Log
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &DriverClient{cfg: c, hc: hc, log: log, sessions: map[int]*driverSession{}}
}

// String, GoString and LogValue keep the fleet secret and the tokens out of
// every formatting path.
func (*DriverClient) String() string       { return "DriverClient{[redacted]}" }
func (c *DriverClient) GoString() string   { return c.String() }
func (*DriverClient) LogValue() slog.Value { return slog.StringValue("[redacted]") }

// String, GoString and LogValue redact the config for the same reason.
func (DriverClientConfig) String() string       { return "DriverClientConfig{[redacted]}" }
func (c DriverClientConfig) GoString() string   { return c.String() }
func (DriverClientConfig) LogValue() slog.Value { return slog.StringValue("[redacted]") }

func (c *DriverClient) session(n int) *driverSession {
	c.mu.Lock()
	defer c.mu.Unlock()
	s := c.sessions[n]
	if s == nil {
		s = &driverSession{}
		c.sessions[n] = s
	}
	return s
}

type tokenReply struct {
	AccessToken      secret.String `json:"access_token"`
	RefreshToken     secret.String `json:"refresh_token"`
	ExpiresInSeconds int64         `json:"expires_in"`
}

// Login signs driver n in with its derived password.
func (c *DriverClient) Login(ctx context.Context, n int) error {
	s := c.session(n)
	s.mu.Lock()
	defer s.mu.Unlock()
	return c.login(ctx, n, s)
}

// login runs with s.mu held.
func (c *DriverClient) login(ctx context.Context, n int, s *driverSession) error {
	if len(c.cfg.Fleet) < MinFleetSecretLen {
		return ErrFleetSecret
	}
	email := devicefleet.SimDriverEmail(n)
	body := map[string]string{
		"email": email, "client_kind": "mobile",
		"password": devicefleet.SimDriverPassword(c.cfg.Fleet, email).Reveal(),
	}
	var out struct {
		Data tokenReply `json:"data"`
	}
	if _, err := c.call(ctx, n, http.MethodPost, "/v1/auth/login", "", nil, body, &out); err != nil {
		return err
	}
	c.store(s, out.Data)
	return nil
}

// renew runs with s.mu held: it rotates the refresh token and falls back to a
// full login when the session is gone.
func (c *DriverClient) renew(ctx context.Context, n int, s *driverSession) error {
	if s.refresh == "" {
		return c.login(ctx, n, s)
	}
	var out struct {
		Data tokenReply `json:"data"`
	}
	_, err := c.call(ctx, n, http.MethodPost, "/v1/auth/refresh", "", nil, map[string]string{"refresh_token": s.refresh.Reveal()}, &out)
	if apiErr, ok := errors.AsType[*DriverAPIError](err); ok && apiErr.Status == http.StatusUnauthorized {
		return c.login(ctx, n, s)
	}
	if err != nil {
		return err
	}
	c.store(s, out.Data)
	return nil
}

func (c *DriverClient) store(s *driverSession, t tokenReply) {
	s.access, s.refresh = t.AccessToken, t.RefreshToken
	s.expiresAt = c.cfg.Clock.Now().Add(time.Duration(t.ExpiresInSeconds)*time.Second - expirySkew)
	s.gen++
}

// token returns a usable access token, logging in or renewing as needed.
func (c *DriverClient) token(ctx context.Context, n int) (secret.String, int, error) {
	s := c.session(n)
	s.mu.Lock()
	defer s.mu.Unlock()
	var err error
	switch {
	case s.access == "":
		err = c.login(ctx, n, s)
	case !c.cfg.Clock.Now().Before(s.expiresAt):
		err = c.renew(ctx, n, s)
	}
	return s.access, s.gen, err
}

// authed sends an authenticated request. A 401 renews the session once, no
// matter how many calls hit it together, and retries: the API rejected the
// request before running it, so the retry is safe for a POST too.
func (c *DriverClient) authed(ctx context.Context, n int, method, path string, hdr map[string]string, in, out any) (int, error) {
	tok, gen, err := c.token(ctx, n)
	if err != nil {
		return 0, err
	}
	status, err := c.call(ctx, n, method, path, tok.Reveal(), hdr, in, out)
	if apiErr, ok := errors.AsType[*DriverAPIError](err); !ok || apiErr.Status != http.StatusUnauthorized {
		return status, err
	}
	s := c.session(n)
	s.mu.Lock()
	if s.gen == gen { // nobody renewed since our token was read
		if err := c.renew(ctx, n, s); err != nil {
			s.mu.Unlock()
			return 0, err
		}
	}
	tok = s.access
	s.mu.Unlock()
	return c.call(ctx, n, method, path, tok.Reveal(), hdr, in, out)
}

// call performs one request. It never logs or returns a body.
func (c *DriverClient) call(ctx context.Context, n int, method, path, token string, hdr map[string]string, in, out any) (int, error) {
	var rd io.Reader
	if in != nil {
		raw, err := json.Marshal(in)
		if err != nil {
			return 0, fmt.Errorf("simulator: encode %s: %w", path, err)
		}
		rd = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.cfg.BaseURL, "/")+path, rd)
	if err != nil {
		return 0, fmt.Errorf("simulator: build %s: %w", path, err)
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return 0, fmt.Errorf("simulator: driver %d %s: %w", n, path, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxReplyBytes))
	if err != nil {
		return resp.StatusCode, fmt.Errorf("simulator: driver %d %s: read: %w", n, path, err)
	}
	c.log.Debug("driver request", "driver", n, "method", method, "path", path, "status", resp.StatusCode)
	if resp.StatusCode/100 != 2 {
		var env struct {
			Error struct {
				Code string `json:"code"`
			} `json:"error"`
		}
		_ = json.Unmarshal(raw, &env)
		return resp.StatusCode, &DriverAPIError{Status: resp.StatusCode, Code: env.Error.Code}
	}
	if out != nil {
		if err := json.Unmarshal(raw, out); err != nil {
			return resp.StatusCode, fmt.Errorf("simulator: driver %d %s: decode reply", n, path)
		}
	}
	return resp.StatusCode, nil
}

type wireHold struct {
	StartsAt time.Time `json:"starts_at"`
	EndsAt   time.Time `json:"ends_at"`
}

type wireReservation struct {
	ID        uuid.UUID `json:"id"`
	Status    string    `json:"status"`
	FeeCents  int64     `json:"fee_cents"`
	ArrivalAt time.Time `json:"arrival_at"`
}

func (w wireReservation) public() Reservation { return Reservation(w) }

// Quote prices the hold window of an arrival for driver n.
func (c *DriverClient) Quote(ctx context.Context, n int, arrival time.Time) (Quote, error) {
	var out struct {
		Data struct {
			ArrivalAt  time.Time `json:"arrival_at"`
			HoldWindow wireHold  `json:"hold_window"`
			FeeCents   int64     `json:"fee_cents"`
		} `json:"data"`
	}
	body := map[string]any{"lot_id": c.cfg.LotID, "arrival_at": arrival.UTC().Format(time.RFC3339)}
	if _, err := c.authed(ctx, n, http.MethodPost, "/v1/reservations/quote", nil, body, &out); err != nil {
		return Quote{}, err
	}
	d := out.Data
	return Quote{ArrivalAt: d.ArrivalAt, HoldStart: d.HoldWindow.StartsAt, HoldEnd: d.HoldWindow.EndsAt, FeeCents: d.FeeCents}, nil
}

// Create reserves a space for driver n's vehicle.
func (c *DriverClient) Create(ctx context.Context, n int, arrival time.Time, vehicleID uuid.UUID) (Reservation, error) {
	var out struct {
		Data wireReservation `json:"data"`
	}
	body := map[string]any{"lot_id": c.cfg.LotID, "vehicle_id": vehicleID, "arrival_at": arrival.UTC().Format(time.RFC3339)}
	if _, err := c.authed(ctx, n, http.MethodPost, "/v1/reservations", nil, body, &out); err != nil {
		return Reservation{}, err
	}
	return out.Data.public(), nil
}

// Pay charges the reservation fee on the always-succeeding test card. It
// returns the reservation status after a 200 ("confirmed") and "pending" for
// a 202, where the gateway answered late and the API confirms shortly after.
func (c *DriverClient) Pay(ctx context.Context, n int, reservationID uuid.UUID, key string) (string, error) {
	var out struct {
		Data struct {
			Status      string `json:"status"`
			Reservation *struct {
				Status string `json:"status"`
			} `json:"reservation"`
			Payment *struct {
				Status string `json:"status"`
			} `json:"payment"`
		} `json:"data"`
	}
	now := c.cfg.Clock.Now()
	card := map[string]any{"card_number": simCardNumber, "exp_month": 12, "exp_year": now.Year() + 3, "cvc": "123"}
	status, err := c.authed(ctx, n, http.MethodPost, "/v1/reservations/"+reservationID.String()+"/pay",
		map[string]string{"Idempotency-Key": key}, card, &out)
	if err != nil {
		return "", err
	}
	if status == http.StatusAccepted && out.Data.Payment != nil {
		return out.Data.Payment.Status, nil
	}
	return out.Data.Status, nil
}

// QR fetches the entry token of a confirmed reservation. The token is a
// secret and a plain string, because the gate payload must carry it verbatim:
// callers must never log it.
func (c *DriverClient) QR(ctx context.Context, n int, reservationID uuid.UUID) (string, error) {
	var out struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	if _, err := c.authed(ctx, n, http.MethodGet, "/v1/reservations/"+reservationID.String()+"/qr", nil, nil, &out); err != nil {
		return "", err
	}
	return out.Data.Token, nil
}

// Vehicle returns the id of driver n's first vehicle.
func (c *DriverClient) Vehicle(ctx context.Context, n int) (uuid.UUID, error) {
	var out struct {
		Data []struct {
			ID uuid.UUID `json:"id"`
		} `json:"data"`
	}
	if _, err := c.authed(ctx, n, http.MethodGet, "/v1/vehicles", nil, nil, &out); err != nil {
		return uuid.Nil, err
	}
	if len(out.Data) == 0 {
		return uuid.Nil, fmt.Errorf("simulator: driver %d has no vehicle", n)
	}
	return out.Data[0].ID, nil
}
