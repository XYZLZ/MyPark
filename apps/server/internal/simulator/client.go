package simulator

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/XYZLZ/MyPark/apps/server/internal/devicefleet"
	"github.com/XYZLZ/MyPark/apps/server/internal/devicesig"
	"github.com/XYZLZ/MyPark/apps/server/internal/domain/secret"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/clock"
)

const (
	eventsPath = "/v1/devices/events"
	// MinFleetSecretLen is the shortest fleet secret the client accepts.
	MinFleetSecretLen = 32
	maxRetries        = 5
	baseBackoff       = 100 * time.Millisecond
	minRetryAfter     = time.Second
	// maxRetryAfter bounds a hostile or misconfigured Retry-After.
	maxRetryAfter  = 30 * time.Second
	maxReplyBytes  = 1 << 20
	requestTimeout = 15 * time.Second
)

// ErrFleetSecret is returned by Send when the fleet secret is shorter than
// MinFleetSecretLen: a weak key would derive guessable device secrets.
var ErrFleetSecret = errors.New("simulator: fleet secret must be at least 32 bytes")

// APIError is a non-2xx answer. Code is the error envelope's code, when it
// could be read.
type APIError struct {
	Status     int
	Code       string
	RetryAfter time.Duration
}

func (e *APIError) Error() string {
	return fmt.Sprintf("simulator: device API answered %d %s", e.Status, e.Code)
}

// ClientConfig configures a DeviceClient.
type ClientConfig struct {
	// BaseURL is the API origin. A path prefix and a query are kept; the
	// query is part of the signed request URI.
	BaseURL string
	// Transport defaults to http.DefaultTransport.
	Transport http.RoundTripper
	// SignClock stamps X-Timestamp: the wall clock in compose, the shared
	// fake clock in CI.
	SignClock clock.Clock
	// FleetSecret derives every device secret (devicefleet.DeviceSecret).
	FleetSecret secret.Bytes
	// Sleep waits between retries; nil sleeps for real.
	Sleep SleepFunc
}

// DeviceClient posts signed device events (design §7.1, §7.6).
type DeviceClient struct {
	cfg ClientConfig
	hc  *http.Client
}

// NewDeviceClient returns a client over c.
func NewDeviceClient(c ClientConfig) *DeviceClient {
	rt := c.Transport
	if rt == nil {
		rt = http.DefaultTransport
	}
	if c.Sleep == nil {
		c.Sleep = sleepCtx
	}
	return &DeviceClient{cfg: c, hc: &http.Client{
		Transport: rt,
		Timeout:   requestTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}}
}

// Outgoing is one device event. A zero OccurredAt is stamped by Send with the
// signing clock.
type Outgoing struct {
	EventID    uuid.UUID
	Type       string
	OccurredAt time.Time
	Payload    any
}

// Reply is the decoded answer to an event. PaidAt and ExitDeadline are zero
// when the answer carries none.
type Reply struct {
	Status                                         int
	Result, Reason, StayID, TicketCode, StayStatus string
	AmountCents                                    int64
	Replayed                                       bool
	PaidAt, ExitDeadline                           time.Time
}

type wireEvent struct {
	EventID    uuid.UUID `json:"event_id"`
	Type       string    `json:"type"`
	OccurredAt string    `json:"occurred_at"`
	Payload    any       `json:"payload"`
}

type wireReply struct {
	Result       string          `json:"result"`
	Reason       string          `json:"reason"`
	StayID       string          `json:"stay_id"`
	TicketCode   string          `json:"ticket_code"`
	Status       string          `json:"status"`
	AmountCents  *int64          `json:"amount_cents"`
	Breakdown    json.RawMessage `json:"breakdown"`
	ReceiptNo    string          `json:"receipt_no"`
	PaidAt       *time.Time      `json:"paid_at"`
	ExitDeadline *time.Time      `json:"exit_deadline"`
}

type wireEnvelope struct {
	Data *wireReply      `json:"data"`
	Meta json.RawMessage `json:"meta"`
}

// Send posts ev as dev. It marshals once and, on every attempt, signs with a
// fresh timestamp. It retries 429 (honoring Retry-After, at least 1 s), 5xx
// and transport errors with the same event id and body, at most five times,
// backing off 100 ms × 2ⁿ. Any other non-2xx answer is an *APIError and is
// not retried; the Reply then carries only its Status.
func (c *DeviceClient) Send(ctx context.Context, dev devicefleet.Device, ev Outgoing) (Reply, error) {
	if len(c.cfg.FleetSecret) < MinFleetSecretLen {
		return Reply{}, ErrFleetSecret
	}
	target, err := c.target()
	if err != nil {
		return Reply{}, err
	}
	if ev.OccurredAt.IsZero() {
		ev.OccurredAt = c.cfg.SignClock.Now()
	}
	body, err := json.Marshal(wireEvent{
		EventID: ev.EventID, Type: ev.Type,
		OccurredAt: ev.OccurredAt.UTC().Format(time.RFC3339Nano), Payload: ev.Payload,
	})
	if err != nil {
		return Reply{}, fmt.Errorf("simulator: encode event: %w", err)
	}
	key := devicefleet.DeviceSecret(c.cfg.FleetSecret, dev.PublicID)

	for attempt := 0; ; attempt++ {
		rep, err := c.attempt(ctx, dev, key, target, body)
		if err == nil {
			return rep, nil
		}
		var apiErr *APIError
		isAPI := errors.As(err, &apiErr)
		retryable := (isAPI && (apiErr.Status == http.StatusTooManyRequests || apiErr.Status >= 500)) ||
			(!isAPI && ctx.Err() == nil && !errors.Is(err, errMalformedReply))
		if !retryable {
			return rep, err
		}
		if attempt == maxRetries {
			return rep, fmt.Errorf("simulator: giving up after %d retries: %w", maxRetries, err)
		}
		wait := baseBackoff << attempt
		if isAPI && apiErr.Status == http.StatusTooManyRequests {
			wait = min(max(apiErr.RetryAfter, minRetryAfter), maxRetryAfter)
		}
		if err := c.cfg.Sleep(ctx, wait); err != nil {
			return rep, err
		}
	}
}

// target returns the events URL: the base path and query are kept as given.
func (c *DeviceClient) target() (*url.URL, error) {
	u, err := url.Parse(c.cfg.BaseURL)
	if err != nil || u.Host == "" {
		return nil, fmt.Errorf("simulator: invalid base URL")
	}
	u.Path = path.Join("/", u.Path, eventsPath)
	u.RawPath = ""
	u.Fragment = ""
	return u, nil
}

// sign returns the X-Signature of one request: the canonical string is
// METHOD, the raw request URI (path and raw query as sent), the X-Timestamp
// value and the body hash.
func sign(key secret.Bytes, method, requestURI, timestamp string, body []byte) string {
	return devicesig.Sign(key.Reveal(), devicesig.Canonical(method, requestURI, timestamp, body))
}

var errMalformedReply = errors.New("simulator: malformed device reply")

func (c *DeviceClient) attempt(ctx context.Context, dev devicefleet.Device, key secret.Bytes, target *url.URL, body []byte) (Reply, error) {
	ts := c.cfg.SignClock.Now().UTC().Format(devicesig.TimestampLayout)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target.String(), bytes.NewReader(body))
	if err != nil {
		return Reply{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Device-Id", dev.PublicID.String())
	req.Header.Set("X-Timestamp", ts)
	// Sign exactly the request URI that goes on the wire.
	req.Header.Set("X-Signature", sign(key, req.Method, req.URL.RequestURI(), ts, body))

	res, err := c.hc.Do(req)
	if err != nil {
		return Reply{}, err
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, maxReplyBytes))
	if err != nil {
		return Reply{Status: res.StatusCode}, err
	}
	if res.StatusCode < 200 || res.StatusCode > 299 {
		return Reply{Status: res.StatusCode}, apiError(res, raw)
	}
	rep, err := decodeReply(raw)
	rep.Status = res.StatusCode
	rep.Replayed = res.Header.Get("Idempotent-Replayed") == "true"
	return rep, err
}

func apiError(res *http.Response, raw []byte) *APIError {
	e := &APIError{Status: res.StatusCode}
	var env struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if json.Unmarshal(raw, &env) == nil {
		e.Code = env.Error.Code
	}
	if secs, err := strconv.Atoi(res.Header.Get("Retry-After")); err == nil && secs > 0 {
		e.RetryAfter = time.Duration(secs) * time.Second
	}
	return e
}

// decodeReply strictly decodes a success envelope: unknown fields, trailing
// data, a missing data object and an unknown result are refused.
func decodeReply(raw []byte) (Reply, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var env wireEnvelope
	if err := dec.Decode(&env); err != nil {
		return Reply{}, fmt.Errorf("%w: %v", errMalformedReply, err)
	}
	if _, err := dec.Token(); err != io.EOF {
		return Reply{}, fmt.Errorf("%w: trailing data", errMalformedReply)
	}
	d := env.Data
	if d == nil {
		return Reply{}, fmt.Errorf("%w: no data", errMalformedReply)
	}
	switch d.Result {
	case "ack", "open", "deny", "rejected":
	default:
		return Reply{}, fmt.Errorf("%w: unknown result %q", errMalformedReply, d.Result)
	}
	rep := Reply{
		Result: d.Result, Reason: d.Reason, StayID: d.StayID,
		TicketCode: d.TicketCode, StayStatus: d.Status,
	}
	if d.AmountCents != nil {
		rep.AmountCents = *d.AmountCents
	}
	if d.PaidAt != nil {
		rep.PaidAt = *d.PaidAt
	}
	if d.ExitDeadline != nil {
		rep.ExitDeadline = *d.ExitDeadline
	}
	return rep, nil
}
