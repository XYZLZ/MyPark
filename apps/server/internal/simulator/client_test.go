package simulator

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/XYZLZ/MyPark/apps/server/internal/devicefleet"
	"github.com/XYZLZ/MyPark/apps/server/internal/devicesig"
	"github.com/XYZLZ/MyPark/apps/server/internal/domain/authz"
	"github.com/XYZLZ/MyPark/apps/server/internal/domain/secret"
	"github.com/XYZLZ/MyPark/apps/server/internal/testsupport/fakeclock"
)

var fleetSecret = secret.Bytes(bytes.Repeat([]byte("k"), 32))

type captured struct {
	method, uri string
	header      http.Header
	body        []byte
}

// scripted answers the n-th request with replies[n] (the last one repeats)
// and records every request.
type scripted struct {
	replies []func() (*http.Response, error)
	got     []captured
}

func (s *scripted) RoundTrip(r *http.Request) (*http.Response, error) {
	body, _ := io.ReadAll(r.Body)
	s.got = append(s.got, captured{r.Method, r.URL.RequestURI(), r.Header.Clone(), body})
	i := min(len(s.got)-1, len(s.replies)-1)
	return s.replies[i]()
}

func reply(status int, header http.Header, body string) func() (*http.Response, error) {
	return func() (*http.Response, error) {
		if header == nil {
			header = http.Header{}
		}
		return &http.Response{
			StatusCode: status, Header: header,
			Body: io.NopCloser(strings.NewReader(body)),
		}, nil
	}
}

const okBody = `{"data":{"result":"ack"}}`

func newTestClient(rt http.RoundTripper, base string) (*DeviceClient, *fakeclock.Clock, *[]time.Duration) {
	sign := fakeclock.New(time.Date(2026, 11, 24, 14, 3, 11, 0, time.UTC))
	var sleeps []time.Duration
	c := NewDeviceClient(ClientConfig{
		BaseURL: base, Transport: rt, SignClock: sign, FleetSecret: fleetSecret,
		Sleep: func(_ context.Context, d time.Duration) error {
			sleeps = append(sleeps, d)
			sign.Advance(d) // the wall clock moves while the client backs off
			return nil
		},
	})
	return c, sign, &sleeps
}

func podium() devicefleet.Device { return devicefleet.ByKind(authz.DevicePodium) }

func heartbeat() Outgoing {
	return Outgoing{
		EventID:    uuid.MustParse("019a6d00-0000-7000-8000-0000000000aa"),
		Type:       "heartbeat",
		OccurredAt: time.Date(2026, 11, 24, 14, 3, 10, 0, time.UTC),
		Payload:    struct{}{},
	}
}

func TestSign_GoldenVector(t *testing.T) {
	raw, err := os.ReadFile("../devicesig/testdata/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var g map[string]string // golden.json keys are snake_case
	if err := json.Unmarshal(raw, &g); err != nil {
		t.Fatal(err)
	}
	key, err := hex.DecodeString(g["secret_hex"])
	if err != nil {
		t.Fatal(err)
	}
	if got := sign(secret.Bytes(key), g["method"], g["uri"], g["timestamp"], []byte(g["body"])); got != g["signature"] {
		t.Errorf("sign = %s, want %s", got, g["signature"])
	}
}

func TestClient_SendIsVerifiable(t *testing.T) {
	rt := &scripted{replies: []func() (*http.Response, error){reply(200, nil, okBody)}}
	c, _, _ := newTestClient(rt, "http://api.test/?trace=1")
	dev := podium()
	if _, err := c.Send(context.Background(), dev, heartbeat()); err != nil {
		t.Fatal(err)
	}
	if len(rt.got) != 1 {
		t.Fatalf("%d requests", len(rt.got))
	}
	g := rt.got[0]
	if g.method != http.MethodPost || g.uri != "/v1/devices/events?trace=1" {
		t.Errorf("request = %s %s", g.method, g.uri)
	}
	if g.header.Get("Content-Type") != "application/json" || g.header.Get("X-Device-Id") != dev.PublicID.String() {
		t.Errorf("headers = %v", g.header)
	}
	ts := g.header.Get("X-Timestamp")
	if ts != "2026-11-24T14:03:11Z" {
		t.Errorf("X-Timestamp = %q", ts)
	}
	wantBody := `{"event_id":"019a6d00-0000-7000-8000-0000000000aa","type":"heartbeat","occurred_at":"2026-11-24T14:03:10Z","payload":{}}`
	if string(g.body) != wantBody {
		t.Errorf("body = %s", g.body)
	}
	key := devicefleet.DeviceSecret(fleetSecret, dev.PublicID).Reveal()
	canon := devicesig.Canonical(g.method, g.uri, ts, g.body)
	if !devicesig.Verify([]byte(key), canon, g.header.Get("X-Signature")) {
		t.Error("X-Signature does not verify over the sent request URI")
	}
}

func TestClient_RetriesSameBody(t *testing.T) {
	rt := &scripted{replies: []func() (*http.Response, error){
		reply(429, http.Header{"Retry-After": {"2"}}, `{"error":{"code":"rate_limit.exceeded"}}`),
		reply(503, nil, ""),
		reply(200, nil, okBody),
	}}
	c, _, sleeps := newTestClient(rt, "http://api.test")
	rep, err := c.Send(context.Background(), podium(), heartbeat())
	if err != nil {
		t.Fatal(err)
	}
	if rep.Status != 200 || rep.Result != "ack" {
		t.Errorf("reply = %+v", rep)
	}
	if len(rt.got) != 3 {
		t.Fatalf("%d attempts, want 3", len(rt.got))
	}
	if want := []time.Duration{2 * time.Second, 200 * time.Millisecond}; len(*sleeps) != 2 || (*sleeps)[0] != want[0] || (*sleeps)[1] != want[1] {
		t.Errorf("sleeps = %v, want %v", *sleeps, want)
	}
	for i, g := range rt.got {
		if !bytes.Equal(g.body, rt.got[0].body) {
			t.Errorf("attempt %d body differs", i)
		}
		if i == 1 && g.header.Get("X-Timestamp") == rt.got[i-1].header.Get("X-Timestamp") {
			t.Errorf("attempt %d reused the previous X-Timestamp", i)
		}
		if i == 1 && g.header.Get("X-Signature") == rt.got[i-1].header.Get("X-Signature") {
			t.Errorf("attempt %d was not re-signed", i)
		}
	}
}

func TestClient_RetryAfterIsAtLeastOneSecond(t *testing.T) {
	rt := &scripted{replies: []func() (*http.Response, error){
		reply(429, http.Header{"Retry-After": {"0"}}, ""), reply(200, nil, okBody),
	}}
	c, _, sleeps := newTestClient(rt, "http://api.test")
	if _, err := c.Send(context.Background(), podium(), heartbeat()); err != nil {
		t.Fatal(err)
	}
	if len(*sleeps) != 1 || (*sleeps)[0] != time.Second {
		t.Errorf("sleeps = %v, want [1s]", *sleeps)
	}
}

func TestClient_GivesUpAfterFiveRetries(t *testing.T) {
	rt := &scripted{replies: []func() (*http.Response, error){reply(503, nil, "")}}
	c, _, sleeps := newTestClient(rt, "http://api.test")
	_, err := c.Send(context.Background(), podium(), heartbeat())
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Status != 503 {
		t.Fatalf("err = %v", err)
	}
	if len(rt.got) != 6 || len(*sleeps) != 5 {
		t.Errorf("attempts = %d, sleeps = %v", len(rt.got), *sleeps)
	}
	if (*sleeps)[4] != 1600*time.Millisecond {
		t.Errorf("last backoff = %v", (*sleeps)[4])
	}
}

func TestClient_RetriesTransportErrors(t *testing.T) {
	boom := func() (*http.Response, error) { return nil, errors.New("connection reset") }
	rt := &scripted{replies: []func() (*http.Response, error){boom, reply(200, nil, okBody)}}
	c, _, sleeps := newTestClient(rt, "http://api.test")
	if _, err := c.Send(context.Background(), podium(), heartbeat()); err != nil {
		t.Fatal(err)
	}
	if len(rt.got) != 2 || len(*sleeps) != 1 || (*sleeps)[0] != 100*time.Millisecond {
		t.Errorf("attempts = %d, sleeps = %v", len(rt.got), *sleeps)
	}
}

func TestClient_DoesNotRetryClientErrors(t *testing.T) {
	rt := &scripted{replies: []func() (*http.Response, error){
		reply(409, nil, `{"error":{"code":"device.event_conflict","message":"x"}}`),
	}}
	c, _, sleeps := newTestClient(rt, "http://api.test")
	rep, err := c.Send(context.Background(), podium(), heartbeat())
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Status != 409 || apiErr.Code != "device.event_conflict" {
		t.Fatalf("err = %v", err)
	}
	if rep.Status != 409 || len(rt.got) != 1 || len(*sleeps) != 0 {
		t.Errorf("reply = %+v, attempts = %d, sleeps = %v", rep, len(rt.got), *sleeps)
	}
}

func TestClient_ParsesEnvelope(t *testing.T) {
	const body = `{"data":{"result":"open","stay_id":"019a6d00-0000-7000-8000-000000000001","ticket_code":"TK-1","status":"entry_pending","amount_cents":15000,"receipt_no":"R-1","breakdown":{"x":1},"paid_at":"2026-11-24T14:00:00Z","exit_deadline":"2026-11-24T14:15:00Z"},"meta":{"request_id":"r"}}`
	rt := &scripted{replies: []func() (*http.Response, error){
		reply(200, http.Header{"Idempotent-Replayed": {"true"}}, body),
	}}
	c, _, _ := newTestClient(rt, "http://api.test")
	rep, err := c.Send(context.Background(), podium(), heartbeat())
	if err != nil {
		t.Fatal(err)
	}
	want := Reply{
		Status: 200, Result: "open", StayID: "019a6d00-0000-7000-8000-000000000001",
		TicketCode: "TK-1", StayStatus: "entry_pending", AmountCents: 15000, Replayed: true,
		PaidAt:       time.Date(2026, 11, 24, 14, 0, 0, 0, time.UTC),
		ExitDeadline: time.Date(2026, 11, 24, 14, 15, 0, 0, time.UTC),
	}
	if rep != want {
		t.Errorf("reply = %+v\nwant    %+v", rep, want)
	}
}

func TestClient_RejectsMalformedReplies(t *testing.T) {
	for name, body := range map[string]string{
		"unknown data field": `{"data":{"result":"ack","surprise":1}}`,
		"unknown envelope":   `{"data":{"result":"ack"},"extra":1}`,
		"missing data":       `{"meta":{}}`,
		"unknown result":     `{"data":{"result":"maybe"}}`,
		"trailing data":      `{"data":{"result":"ack"}} {}`,
		"not json":           `<html>`,
	} {
		rt := &scripted{replies: []func() (*http.Response, error){reply(200, nil, body)}}
		c, _, _ := newTestClient(rt, "http://api.test")
		if _, err := c.Send(context.Background(), podium(), heartbeat()); err == nil {
			t.Errorf("%s: accepted", name)
		}
		if len(rt.got) != 1 {
			t.Errorf("%s: %d attempts, a bad 200 must not be retried", name, len(rt.got))
		}
	}
}

func TestClient_RefusesWeakFleetSecret(t *testing.T) {
	for _, s := range []secret.Bytes{nil, fleetSecret[:31]} {
		rt := &scripted{replies: []func() (*http.Response, error){reply(200, nil, okBody)}}
		c := NewDeviceClient(ClientConfig{
			BaseURL: "http://api.test", Transport: rt, FleetSecret: s,
			SignClock: fakeclock.New(t0),
		})
		if _, err := c.Send(context.Background(), podium(), heartbeat()); !errors.Is(err, ErrFleetSecret) {
			t.Errorf("len %d: err = %v", len(s), err)
		}
		if len(rt.got) != 0 {
			t.Error("a request was sent with a weak fleet secret")
		}
	}
}

func TestClient_StopsOnCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	boom := func() (*http.Response, error) { cancel(); return nil, ctx.Err() }
	rt := &scripted{replies: []func() (*http.Response, error){boom}}
	c, _, sleeps := newTestClient(rt, "http://api.test")
	if _, err := c.Send(ctx, podium(), heartbeat()); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	if len(rt.got) != 1 || len(*sleeps) != 0 {
		t.Errorf("attempts = %d, sleeps = %v", len(rt.got), *sleeps)
	}
}

func TestHandlerTransport_ServesRequestInProcess(t *testing.T) {
	dev := podium()
	key := devicefleet.DeviceSecret(fleetSecret, dev.PublicID).Reveal()
	var verified bool
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		// The server reads the signed URI from r.RequestURI.
		canon := devicesig.Canonical(r.Method, r.RequestURI, r.Header.Get("X-Timestamp"), body)
		verified = devicesig.Verify([]byte(key), canon, r.Header.Get("X-Signature"))
		w.Header().Set("Idempotent-Replayed", "true")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":{"result":"deny","reason":"gate.lot_full"}}`)
	})
	c, _, _ := newTestClient(HandlerTransport(h), "http://api.test/?trace=1")
	rep, err := c.Send(context.Background(), dev, heartbeat())
	if err != nil {
		t.Fatal(err)
	}
	if !verified {
		t.Error("handler could not verify the signature over r.RequestURI")
	}
	if rep.Result != "deny" || rep.Reason != "gate.lot_full" || !rep.Replayed {
		t.Errorf("reply = %+v", rep)
	}
}
