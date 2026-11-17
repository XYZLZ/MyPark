package main

import (
	"bytes"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/XYZLZ/MyPark/apps/server/internal/domain/authz"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/tariffs"
	"github.com/XYZLZ/MyPark/apps/server/internal/testsupport"
)

// rsvNow is the fixed test time of the reservation tests (design 7.3 examples).
var rsvNow = time.Date(2026, 12, 1, 13, 58, 10, 0, time.UTC)

// rsvEnv is the production handler over the seeded pilot lot, with the clock
// at rsvNow and seeded drivers who can sign in.
type rsvEnv struct {
	*lotEnv
	logs *bytes.Buffer
}

func newRsvEnv(t *testing.T) *rsvEnv {
	t.Helper()
	logs := &bytes.Buffer{}
	e := newLotEnv(t, func(d *deps) { d.log = slog.New(slog.NewTextHandler(logs, nil)) })
	// Before any token is signed, so the tokens are valid at rsvNow.
	e.clk.Set(rsvNow)
	return &rsvEnv{lotEnv: e, logs: logs}
}

// token opens a session for a seeded driver and signs its access token.
func (e *rsvEnv) token(d testsupport.Driver) string {
	e.t.Helper()
	var session uuid.UUID
	err := e.db.Super.QueryRow(e.t.Context(),
		`insert into auth_sessions (user_id, client_kind, absolute_expires_at) values ($1, 'web', $2) returning public_id`,
		d.UserID, e.clk.Now().Add(24*time.Hour)).Scan(&session)
	if err != nil {
		e.t.Fatalf("insert session: %v", err)
	}
	tok, _, err := e.signer.Sign(d.PublicID, session, authz.RoleDriver)
	if err != nil {
		e.t.Fatalf("sign: %v", err)
	}
	return tok
}

// post sends a JSON body as the driver and decodes the answer.
func (e *rsvEnv) post(token, path string, body any) (int, map[string]any) {
	e.t.Helper()
	rec := e.serve(jsonReq(http.MethodPost, path, body), token)
	return rec.Code, decodeBody(e.t, rec)
}

func (e *rsvEnv) createBody(d testsupport.Driver, arrival string) map[string]any {
	return map[string]any{"lot_id": e.publicID, "vehicle_id": d.VehicleID.String(), "arrival_at": arrival}
}

func (e *rsvEnv) count(query string, args ...any) int {
	e.t.Helper()
	var n int
	if err := e.db.App.QueryRow(e.t.Context(), query, args...).Scan(&n); err != nil {
		e.t.Fatalf("count: %v", err)
	}
	return n
}

// field reads a nested value of a decoded body by dotted path.
func field(body map[string]any, path string) any {
	var cur any = body
	for _, k := range strings.Split(path, ".") {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = m[k]
	}
	return cur
}

const arrival1502 = "2026-12-01T15:02:00Z"

func TestCreate_Success(t *testing.T) {
	e := newRsvEnv(t)
	d := testsupport.SeedDrivers(t, e.db, 1)[0]
	code, body := e.post(e.token(d), "/v1/reservations", e.createBody(d, arrival1502))
	if code != http.StatusCreated {
		t.Fatalf("create = %d %v, want 201", code, body)
	}
	for path, want := range map[string]any{
		"data.arrival_at":            "2026-12-01T15:05:00Z",
		"data.hold_window.starts_at": "2026-12-01T14:50:00Z",
		"data.hold_window.ends_at":   "2026-12-01T15:25:00Z",
		"data.status":                "pending_payment",
		"data.fee_cents":             float64(tariffs.DefaultTariff().ReservationFee),
		"data.payment_due_at":        "2026-12-01T14:08:10Z",
		"data.lot_id":                e.publicID,
		"data.vehicle.id":            d.VehicleID.String(),
	} {
		if got := field(body, path); got != want {
			t.Errorf("%s = %v, want %v", path, got, want)
		}
	}
	if n := e.count(`select count(*) from reservation_slot_loads where lot_id = $1 and held = 1`, e.lotID); n != 7 {
		t.Errorf("slot rows held 1 = %d, want 7", n)
	}
	testsupport.AssertSlotInvariant(t, e.db.App, e.lotID)
}

func TestCreate_ChecksInOrder(t *testing.T) {
	type tc struct {
		name   string
		status int
		code   string
		// setup prepares the env and returns the actor and the arrival to send.
		setup func(e *rsvEnv, actor, other testsupport.Driver) (testsupport.Driver, string, map[string]any)
	}
	exec := func(t *testing.T, e *rsvEnv, q string, args ...any) {
		t.Helper()
		if _, err := e.db.Super.Exec(t.Context(), q, args...); err != nil {
			t.Fatalf("setup %q: %v", q, err)
		}
	}
	cases := []tc{
		{"unknown lot", 404, "lot.not_found", func(e *rsvEnv, a, _ testsupport.Driver) (testsupport.Driver, string, map[string]any) {
			b := e.createBody(a, arrival1502)
			b["lot_id"] = uuid.NewString()
			return a, "", b
		}},
		{"other drivers vehicle", 404, "vehicle.not_found", func(e *rsvEnv, a, o testsupport.Driver) (testsupport.Driver, string, map[string]any) {
			b := e.createBody(a, arrival1502)
			b["vehicle_id"] = o.VehicleID.String()
			return a, "", b
		}},
		{"blocked", 409, "reservation.blocked_no_shows", func(e *rsvEnv, a, _ testsupport.Driver) (testsupport.Driver, string, map[string]any) {
			exec(e.t, e, `update users set reservation_blocked_until = $2 where id = $1`, a.UserID, rsvNow.Add(24*time.Hour))
			return a, arrival1502, nil
		}},
		{"lot closed", 409, "reservation.lot_closed", func(e *rsvEnv, a, _ testsupport.Driver) (testsupport.Driver, string, map[string]any) {
			exec(e.t, e, `update lots set status = 'closed' where id = $1`, e.lotID)
			return a, arrival1502, nil
		}},
		{"arrival in the past", 422, "reservation.arrival_in_past", func(e *rsvEnv, a, _ testsupport.Driver) (testsupport.Driver, string, map[string]any) {
			return a, "2026-12-01T13:00:00Z", nil
		}},
		{"beyond horizon", 422, "reservation.beyond_horizon", func(e *rsvEnv, a, _ testsupport.Driver) (testsupport.Driver, string, map[string]any) {
			return a, rsvNow.Add(8 * 24 * time.Hour).Format(time.RFC3339), nil
		}},
		{"active exists", 409, "reservation.active_exists", func(e *rsvEnv, a, _ testsupport.Driver) (testsupport.Driver, string, map[string]any) {
			if code, body := e.post(e.token(a), "/v1/reservations", e.createBody(a, arrival1502)); code != http.StatusCreated {
				e.t.Fatalf("first create = %d %v", code, body)
			}
			return a, "2026-12-01T16:00:00Z", nil
		}},
		{"no space now", 409, "reservation.no_space_now", func(e *rsvEnv, a, _ testsupport.Driver) (testsupport.Driver, string, map[string]any) {
			testsupport.FillLot(e.t, e.db, e.lotID, 280, rsvNow)
			return a, rsvNow.Add(10 * time.Minute).Format(time.RFC3339), nil
		}},
		{"blocked and closed answers blocked", 409, "reservation.blocked_no_shows", func(e *rsvEnv, a, _ testsupport.Driver) (testsupport.Driver, string, map[string]any) {
			exec(e.t, e, `update users set reservation_blocked_until = $2 where id = $1`, a.UserID, rsvNow.Add(24*time.Hour))
			exec(e.t, e, `update lots set status = 'closed' where id = $1`, e.lotID)
			return a, arrival1502, nil
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := newRsvEnv(t)
			ds := testsupport.SeedDrivers(t, e.db, 2)
			actor, arrival, body := c.setup(e, ds[0], ds[1])
			if body == nil {
				body = e.createBody(actor, arrival)
			}
			code, got := e.post(e.token(actor), "/v1/reservations", body)
			if code != c.status || errCode(got) != c.code {
				t.Fatalf("create = %d %v, want %d %s", code, got, c.status, c.code)
			}
			if c.code == "reservation.blocked_no_shows" && field(got, "error.details.blocked_until") == nil {
				t.Errorf("blocked_no_shows without details.blocked_until: %v", got)
			}
			testsupport.AssertSlotInvariant(t, e.db.App, e.lotID)
		})
	}
}

func TestCreate_ZeroFeeTariffIs500(t *testing.T) {
	e := newRsvEnv(t)
	d := testsupport.SeedDrivers(t, e.db, 1)[0]
	if _, err := e.db.Super.Exec(t.Context(),
		`insert into tariff_versions (lot_id, effective_from, first_hour_cents, additional_hour_cents, exit_grace_min,
		                              post_payment_exit_min, daily_cap_cents, lost_ticket_cents, reservation_fee_cents)
		 select lot_id, '2026-11-15T00:00:00Z', first_hour_cents, additional_hour_cents, exit_grace_min,
		        post_payment_exit_min, daily_cap_cents, lost_ticket_cents, 0
		 from tariff_versions where lot_id = $1 limit 1`, e.lotID); err != nil {
		t.Fatal(err)
	}
	code, body := e.post(e.token(d), "/v1/reservations", e.createBody(d, arrival1502))
	if code != http.StatusInternalServerError || errCode(body) != "internal.unexpected" {
		t.Fatalf("create = %d %v, want 500 internal.unexpected", code, body)
	}
	if n := e.count(`select count(*) from reservations`); n != 0 {
		t.Errorf("reservations stored = %d, want 0", n)
	}
	if !strings.Contains(e.logs.String(), "reservation fee") {
		t.Errorf("log has no cause for the zero fee: %s", e.logs)
	}
	testsupport.AssertSlotInvariant(t, e.db.App, e.lotID)
}

func TestCreate_OneActiveUnderParallelism(t *testing.T) {
	e := newRsvEnv(t)
	d := testsupport.SeedDrivers(t, e.db, 1)[0]
	tok := e.token(d)
	const n = 10
	type res struct {
		code int
		err  string
	}
	out := make(chan res, n)
	for i := range n {
		go func() {
			arrival := rsvNow.Add(time.Hour + time.Duration(i)*5*time.Minute).Format(time.RFC3339)
			code, body := e.post(tok, "/v1/reservations", e.createBody(d, arrival))
			out <- res{code, errCode(body)}
		}()
	}
	created, active := 0, 0
	for range n {
		r := <-out
		switch {
		case r.code == http.StatusCreated:
			created++
		case r.code == http.StatusConflict && r.err == "reservation.active_exists":
			active++
		default:
			t.Errorf("unexpected answer %d %s", r.code, r.err)
		}
	}
	if created != 1 || active != n-1 {
		t.Errorf("created = %d, active_exists = %d, want 1 and %d", created, active, n-1)
	}
	testsupport.AssertSlotInvariant(t, e.db.App, e.lotID)
}

func TestQuote_ReadOnly(t *testing.T) {
	e := newRsvEnv(t)
	d := testsupport.SeedDrivers(t, e.db, 1)[0]
	code, body := e.post(e.token(d), "/v1/reservations/quote",
		map[string]any{"lot_id": e.publicID, "arrival_at": arrival1502, "vehicle_id": d.VehicleID.String()})
	if code != http.StatusOK {
		t.Fatalf("quote = %d %v, want 200", code, body)
	}
	for path, want := range map[string]any{
		"data.arrival_at":            "2026-12-01T15:05:00Z",
		"data.hold_window.starts_at": "2026-12-01T14:50:00Z",
		"data.hold_window.ends_at":   "2026-12-01T15:25:00Z",
		"data.fee_cents":             float64(tariffs.DefaultTariff().ReservationFee),
		"data.payment_timeout_min":   float64(10),
		"data.lot_id":                e.publicID,
	} {
		if got := field(body, path); got != want {
			t.Errorf("%s = %v, want %v", path, got, want)
		}
	}
	if n := e.count(`select count(*) from reservations`); n != 0 {
		t.Errorf("reservations = %d, want 0", n)
	}
	if n := e.count(`select count(*) from reservation_slot_loads`); n != 0 {
		t.Errorf("slot rows = %d, want 0", n)
	}
	testsupport.AssertSlotInvariant(t, e.db.App, e.lotID)
}

func TestQuote_SameErrorsAsCreate(t *testing.T) {
	cases := []struct {
		name    string
		code    string
		arrival string
		setup   func(e *rsvEnv, d testsupport.Driver)
	}{
		{"past", "reservation.arrival_in_past", "2026-12-01T13:00:00Z", nil},
		{"beyond horizon", "reservation.beyond_horizon", rsvNow.Add(8 * 24 * time.Hour).Format(time.RFC3339), nil},
		{"blocked", "reservation.blocked_no_shows", arrival1502, func(e *rsvEnv, d testsupport.Driver) {
			if _, err := e.db.Super.Exec(t.Context(), `update users set reservation_blocked_until = $2 where id = $1`,
				d.UserID, rsvNow.Add(time.Hour)); err != nil {
				e.t.Fatal(err)
			}
		}},
		{"active exists", "reservation.active_exists", arrival1502, func(e *rsvEnv, d testsupport.Driver) {
			if code, body := e.post(e.token(d), "/v1/reservations", e.createBody(d, arrival1502)); code != http.StatusCreated {
				e.t.Fatalf("create = %d %v", code, body)
			}
		}},
		{"capacity reached", "reservation.capacity_reached", arrival1502, func(e *rsvEnv, _ testsupport.Driver) {
			e.fillWindow(84)
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := newRsvEnv(t)
			d := testsupport.SeedDrivers(t, e.db, 1)[0]
			if c.setup != nil {
				c.setup(e, d)
			}
			code, body := e.post(e.token(d), "/v1/reservations/quote",
				map[string]any{"lot_id": e.publicID, "arrival_at": c.arrival})
			if code < 400 || errCode(body) != c.code {
				t.Fatalf("quote = %d %v, want error %s", code, body, c.code)
			}
			testsupport.AssertSlotInvariant(t, e.db.App, e.lotID)
		})
	}
}

// Review Focus 1: any offset or fraction normalizes to the UTC 5-minute
// boundary, never a 422 and never a window off by an offset.
func TestQuote_OffsetArrivalNormalized(t *testing.T) {
	for in, want := range map[string]string{
		"2026-12-01T11:02:30.5-04:00": "2026-12-01T15:05:00Z",
		"2026-12-01T15:00:00.000Z":    "2026-12-01T15:00:00Z",
	} {
		t.Run(in, func(t *testing.T) {
			e := newRsvEnv(t)
			d := testsupport.SeedDrivers(t, e.db, 1)[0]
			code, body := e.post(e.token(d), "/v1/reservations/quote",
				map[string]any{"lot_id": e.publicID, "arrival_at": in})
			if code != http.StatusOK {
				t.Fatalf("quote = %d %v, want 200", code, body)
			}
			if got := field(body, "data.arrival_at"); got != want {
				t.Errorf("arrival_at = %v, want %s", got, want)
			}
			testsupport.AssertSlotInvariant(t, e.db.App, e.lotID)
		})
	}
}

// A quote with a vehicle_id must fail fast behind a writer holding the vehicle
// row (M3B-55): 500 within the lock timeout, never a hang. The 15s timer is
// only the hang detector.
func TestQuote_BlockedVehicleLockFailsFast(t *testing.T) {
	e := newRsvEnv(t)
	d := testsupport.SeedDrivers(t, e.db, 1)[0]
	tok := e.token(d)
	tx, err := e.db.Super.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(t.Context()) }()
	if _, err := tx.Exec(t.Context(), `select 1 from vehicles where public_id = $1 for update`, d.VehicleID); err != nil {
		t.Fatal(err)
	}
	type res struct {
		code int
		body map[string]any
	}
	done := make(chan res, 1)
	go func() {
		code, body := e.post(tok, "/v1/reservations/quote",
			map[string]any{"lot_id": e.publicID, "arrival_at": arrival1502, "vehicle_id": d.VehicleID.String()})
		done <- res{code, body}
	}()
	select {
	case r := <-done:
		if r.code != http.StatusInternalServerError || errCode(r.body) != "internal.unexpected" {
			t.Fatalf("quote = %d %v, want 500 internal.unexpected", r.code, r.body)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("quote hung behind a locked vehicle row")
	}
}
