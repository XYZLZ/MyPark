package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"log/slog"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/XYZLZ/MyPark/apps/server/internal/devicefleet"
	"github.com/XYZLZ/MyPark/apps/server/internal/domain/authz"
	"github.com/XYZLZ/MyPark/apps/server/internal/domain/secret"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/gate"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/database"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/database/dbgen"
	"github.com/XYZLZ/MyPark/apps/server/internal/testsupport"
)

// entryData is the data of an answer to entry.credential_presented.
type entryData struct {
	Result     string     `json:"result"`
	Reason     string     `json:"reason"`
	StayID     *uuid.UUID `json:"stay_id"`
	TicketCode string     `json:"ticket_code"`
}

func entryOf(t *testing.T, rec *httptest.ResponseRecorder) entryData {
	t.Helper()
	var d entryData
	if err := json.Unmarshal([]byte(dataOf(t, rec)), &d); err != nil {
		t.Fatalf("entry data: %v", err)
	}
	return d
}

func (e *deviceEnv) entry(d testsupport.TestDevice, kind, value string) *httptest.ResponseRecorder {
	e.t.Helper()
	p := map[string]any{"kind": kind}
	if value != "" {
		p["value"] = value
	}
	return e.send(d, e.eventBody(uuid.New(), "entry.credential_presented", p))
}

func (e *deviceEnv) ticketRequest(d testsupport.TestDevice) *httptest.ResponseRecorder {
	e.t.Helper()
	return e.entry(d, gate.KindTicketRequest, "")
}

func wantDeny(t *testing.T, rec *httptest.ResponseRecorder, reason string) {
	t.Helper()
	if got := entryOf(t, rec); got.Result != "deny" || got.Reason != reason || got.StayID != nil || got.TicketCode != "" {
		t.Fatalf("answer = %+v, want deny %s", got, reason)
	}
}

func (e *deviceEnv) invariant() {
	e.t.Helper()
	testsupport.AssertOccupancyInvariant(e.t, e.db.App, e.lotID)
}

var entryFleet = secret.Bytes(bytes.Repeat([]byte{7}, 32))

// cardUID seeds the fleet's employee cards and returns the UID of code as a
// reader may send it: lowercase hex bytes separated by colons.
func (e *deviceEnv) cardUID(code string) string {
	e.t.Helper()
	if _, err := gate.SeedCards(e.t.Context(), dbgen.New(e.db.App), e.lotID, entryFleet, e.cfg.CardUIDPepper()); err != nil {
		e.t.Fatalf("seed cards: %v", err)
	}
	uid := strings.ToLower(devicefleet.CardUID(entryFleet, code))
	var parts []string
	for i := 0; i < len(uid); i += 2 {
		parts = append(parts, uid[i:i+2])
	}
	return strings.Join(parts, ":")
}

func TestEntry_TicketIssue(t *testing.T) {
	var logs bytes.Buffer
	e := newDeviceEnv(t, func(d *deps) {
		d.log = slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	})
	d := e.device(authz.DeviceDispenser)
	body := e.eventBody(uuid.New(), "entry.credential_presented", map[string]any{"kind": "ticket_request"})
	got := entryOf(t, e.send(d, body))
	if got.Result != "open" || got.StayID == nil || len(got.TicketCode) != 16 {
		t.Fatalf("answer = %+v, want open with a stay and a 16-character code", got)
	}

	var (
		status     string
		acceptedAt time.Time
		tariffID   int64
		hash       []byte
	)
	if err := e.db.Super.QueryRow(t.Context(),
		`select status, accepted_at, tariff_version_id, ticket_code_hash from stays
		 where lot_id = $1 and public_id = $2 and credential_kind = 'ticket'`,
		e.lotID, *got.StayID).Scan(&status, &acceptedAt, &tariffID, &hash); err != nil {
		t.Fatalf("stay: %v", err)
	}
	if status != "entry_pending" || !acceptedAt.Equal(e.clk.Now()) {
		t.Errorf("stay = %s accepted %v, want entry_pending at %v", status, acceptedAt, e.clk.Now())
	}
	if want := e.count(`select id from tariff_versions where lot_id = $1 order by effective_from desc limit 1`, e.lotID); tariffID != want {
		t.Errorf("tariff_version_id = %d, want the current %d", tariffID, want)
	}
	if want := sha256.Sum256([]byte(got.TicketCode)); !bytes.Equal(hash, want[:]) {
		t.Error("ticket_code_hash is not sha256 of the issued code")
	}

	var stored string
	if err := e.db.Super.QueryRow(t.Context(), `select response::text from device_events`).Scan(&stored); err != nil {
		t.Fatalf("event: %v", err)
	}
	if strings.Contains(stored, got.TicketCode) || strings.Contains(stored, "ticket_code") || !strings.Contains(stored, "sealed") {
		t.Errorf("stored response %s: want the code sealed", stored)
	}
	if strings.Contains(logs.String(), got.TicketCode) {
		t.Error("the plaintext ticket code reached the logs")
	}
	if again := entryOf(t, e.send(d, body)); again.TicketCode != got.TicketCode {
		t.Errorf("replay code = %q, want the issued one", again.TicketCode)
	}
	if n := e.count(`select count(*) from stays`); n != 1 {
		t.Errorf("stays = %d, want 1 (the replay issues nothing)", n)
	}
	e.invariant()
}

// N parallel requests for K free spaces: the lot lock serializes the
// decisions, so exactly K open and the rest are denied lot_full. With a pool
// as large as the burst every request queues on the lock itself, within its
// 1 s lock_timeout, and none answers 500.
func TestEntry_ConcurrentLastSpace(t *testing.T) {
	for _, tc := range []struct {
		name  string
		free  int
		conns int32 // 0 keeps the small test pool
	}{{"one free", 1, 0}, {"three free", 3, 0}, {"one free, 24 connections", 1, 24}} {
		free := tc.free
		t.Run(tc.name, func(t *testing.T) {
			var mods []func(*deps)
			if tc.conns > 0 {
				mods = append(mods, func(d *deps) {
					pool, err := database.NewPool(t.Context(), d.cfg.DatabaseURL(), tc.conns)
					if err != nil {
						t.Fatalf("pool: %v", err)
					}
					t.Cleanup(pool.Close)
					d.pool = pool
				})
			}
			e := newDeviceEnv(t, mods...)
			testsupport.FillLot(t, e.db, e.lotID, 280-free, e.clk.Now())
			d := e.device(authz.DeviceDispenser)

			const n = 20
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			recs := make([]*httptest.ResponseRecorder, n)
			start := make(chan struct{})
			var wg sync.WaitGroup
			for i := range n {
				body := e.eventBody(uuid.New(), "entry.credential_presented", map[string]any{"kind": "ticket_request"})
				req := d.Request(t, deviceEventsPath, body, e.clk.Now()).WithContext(ctx)
				req.RemoteAddr = "203.0.113.7:5555"
				wg.Go(func() {
					<-start
					recs[i] = httptest.NewRecorder()
					e.h.ServeHTTP(recs[i], req)
				})
			}
			close(start)
			wg.Wait()
			if ctx.Err() != nil {
				t.Fatal("requests did not finish within 10 s (deadlock?)")
			}

			opened := 0
			for i, rec := range recs {
				switch got := entryOf(t, rec); {
				case got.Result == "open" && got.StayID != nil && len(got.TicketCode) == 16:
					opened++
				case got.Result == "deny" && got.Reason == "gate.lot_full":
				default:
					t.Errorf("request %d: %+v", i, got)
				}
			}
			if opened != free {
				t.Errorf("opened = %d, want %d", opened, free)
			}
			if p := e.count(`select count(*) from stays where status = 'entry_pending'`); p != int64(free) {
				t.Errorf("pending stays = %d, want %d", p, free)
			}
			if occ := e.count(`select occupied from lot_occupancy where lot_id = $1`, e.lotID); occ != int64(280-free) {
				t.Errorf("occupied = %d, want %d (entry does not move the counter)", occ, 280-free)
			}
			if rows := e.eventRows(); rows != n {
				t.Errorf("events = %d, want %d", rows, n)
			}
			e.invariant()
		})
	}
}

// A pending stay holds its space for 60 s, then stops counting.
func TestEntry_PendingWindow(t *testing.T) {
	e := newDeviceEnv(t)
	testsupport.FillLot(t, e.db, e.lotID, 279, e.clk.Now())
	d := e.device(authz.DeviceDispenser)
	if _, err := e.db.App.Exec(t.Context(),
		`insert into stays (lot_id, credential_kind, ticket_code_hash, status, tariff_version_id, accepted_at, updated_at)
		 select $1, 'ticket', sha256('pending'::bytea), 'entry_pending', id, $2, $2
		 from tariff_versions where lot_id = $1`, e.lotID, e.clk.Now().Add(-59*time.Second)); err != nil {
		t.Fatal(err)
	}

	wantDeny(t, e.ticketRequest(d), "gate.lot_full")
	e.clk.Advance(2 * time.Second) // the pending stay is now 61 s old
	if got := entryOf(t, e.ticketRequest(d)); got.Result != "open" {
		t.Fatalf("answer = %+v, want open once the pending stay is stale", got)
	}
	e.invariant()
}

func TestEntry_LotClosedDenied(t *testing.T) {
	e := newDeviceEnv(t)
	if _, err := e.db.Super.Exec(t.Context(), `update lots set status = 'closed' where id = $1`, e.lotID); err != nil {
		t.Fatal(err)
	}
	wantDeny(t, e.ticketRequest(e.device(authz.DeviceDispenser)), "gate.lot_closed")
	if n := e.count(`select count(*) from stays`); n != 0 {
		t.Errorf("stays = %d, want 0", n)
	}
	e.invariant()
}

// M2 has no reservations: a QR is denied without touching the lot lock, so
// a side transaction holding it does not delay the answer.
func TestEntry_ReservationQRDenied(t *testing.T) {
	e := newDeviceEnv(t)
	d := e.device(authz.DeviceEntryBarrier)
	side, err := e.db.Super.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = side.Rollback(context.WithoutCancel(t.Context())) }()
	if _, err := side.Exec(t.Context(), `select 1 from lot_occupancy where lot_id = $1 for update`, e.lotID); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	body := e.eventBody(uuid.New(), "entry.credential_presented", map[string]any{"kind": "reservation_qr", "value": "RSV-123"})
	req := d.Request(t, deviceEventsPath, body, e.clk.Now()).WithContext(ctx)
	wantDeny(t, e.serve(req, ""), "gate.credential_invalid")
	if err := side.Rollback(t.Context()); err != nil {
		t.Fatal(err)
	}
	if n := e.count(`select count(*) from stays`); n != 0 {
		t.Errorf("stays = %d, want 0", n)
	}
	e.invariant()
}

func TestEntry_EmployeeCard(t *testing.T) {
	e := newDeviceEnv(t)
	d := e.device(authz.DeviceEntryBarrier)
	got := entryOf(t, e.entry(d, gate.KindEmployeeCard, e.cardUID("EMP-007")))
	if got.Result != "open" || got.StayID == nil || got.TicketCode != "" {
		t.Fatalf("answer = %+v, want open with a stay and no ticket code", got)
	}

	var cardID, kind string
	if err := e.db.Super.QueryRow(t.Context(),
		`select c.public_id::text, s.credential_kind from stays s join employee_cards c on c.id = s.employee_card_id
		 where s.public_id = $1 and s.status = 'entry_pending' and c.employee_code = 'EMP-007'`,
		*got.StayID).Scan(&cardID, &kind); err != nil {
		t.Fatalf("card stay: %v", err)
	}
	var actor, entity, auditCard, auditDevice string
	if err := e.db.Super.QueryRow(t.Context(),
		`select actor_kind, entity_type || ':' || entity_id, after->>'employee_card_id', after->>'device_id'
		 from audit_log where action = 'gate.employee_entry'`).Scan(&actor, &entity, &auditCard, &auditDevice); err != nil {
		t.Fatalf("audit: %v", err)
	}
	if actor != "system" || entity != "stay:"+got.StayID.String() || auditCard != cardID || auditDevice != d.PublicID.String() {
		t.Errorf("audit = %s %s card %s device %s; want system stay:%s card %s device %s",
			actor, entity, auditCard, auditDevice, got.StayID, cardID, d.PublicID)
	}
	e.invariant()
}

func TestEntry_EmployeeCardRevokedOrUnknown(t *testing.T) {
	e := newDeviceEnv(t)
	d := e.device(authz.DeviceEntryBarrier)
	wantDeny(t, e.entry(d, gate.KindEmployeeCard, e.cardUID("EMP-031")), "gate.credential_invalid")
	wantDeny(t, e.entry(d, gate.KindEmployeeCard, "04:a2:19:7c:00:11:22"), "gate.credential_invalid")
	if n := e.count(`select count(*) from stays`); n != 0 {
		t.Errorf("stays = %d, want 0", n)
	}
	e.invariant()
}

// One open stay per card (anti-passback).
func TestEntry_EmployeeCardInUse(t *testing.T) {
	e := newDeviceEnv(t)
	d := e.device(authz.DeviceEntryBarrier)
	uid := e.cardUID("EMP-007")
	if got := entryOf(t, e.entry(d, gate.KindEmployeeCard, uid)); got.Result != "open" {
		t.Fatalf("first entry = %+v, want open", got)
	}
	wantDeny(t, e.entry(d, gate.KindEmployeeCard, strings.ToUpper(uid)), "gate.credential_in_use")
	if n := e.count(`select count(*) from stays`); n != 1 {
		t.Errorf("stays = %d, want 1", n)
	}
	e.invariant()
}

// A full lot denies a valid card too: same capacity rule as drive-up.
func TestEntry_EmployeeCardLotFull(t *testing.T) {
	e := newDeviceEnv(t)
	testsupport.FillLot(t, e.db, e.lotID, 280, e.clk.Now())
	wantDeny(t, e.entry(e.device(authz.DeviceEntryBarrier), gate.KindEmployeeCard, e.cardUID("EMP-007")), "gate.lot_full")
	e.invariant()
}

// Spaces out of service shrink the effective capacity.
func TestEntry_OutOfServiceShrinksCapacity(t *testing.T) {
	e := newDeviceEnv(t)
	testsupport.FillLot(t, e.db, e.lotID, 278, e.clk.Now())
	if _, err := e.db.App.Exec(t.Context(),
		`update spaces set status = 'out_of_service' where lot_id = $1 and code in ('E279', 'E280')`, e.lotID); err != nil {
		t.Fatal(err)
	}
	wantDeny(t, e.ticketRequest(e.device(authz.DeviceDispenser)), "gate.lot_full")
	e.invariant()
}

// Two entries of one card at once: the lot lock serializes them and the
// lookup denies the second, so the stays_card_open_key backstop never fires
// (it would answer 500).
func TestEntry_EmployeeCardConcurrentSameCard(t *testing.T) {
	e := newDeviceEnv(t)
	d := e.device(authz.DeviceEntryBarrier)
	uid := e.cardUID("EMP-007")
	recs := make([]*httptest.ResponseRecorder, 2)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range recs {
		body := e.eventBody(uuid.New(), "entry.credential_presented", map[string]any{"kind": gate.KindEmployeeCard, "value": uid})
		req := d.Request(t, deviceEventsPath, body, e.clk.Now())
		req.RemoteAddr = "203.0.113.7:5555"
		wg.Go(func() {
			<-start
			recs[i] = httptest.NewRecorder()
			e.h.ServeHTTP(recs[i], req)
		})
	}
	close(start)
	wg.Wait()

	outcomes := map[string]int{}
	for i, rec := range recs {
		got := entryOf(t, rec)
		switch {
		case got.Result == "open" && got.StayID != nil:
			outcomes["open"]++
		case got.Result == "deny" && got.Reason == "gate.credential_in_use":
			outcomes["in use"]++
		default:
			t.Errorf("request %d: %+v", i, got)
		}
	}
	if outcomes["open"] != 1 || outcomes["in use"] != 1 {
		t.Errorf("outcomes = %v, want one open and one credential_in_use", outcomes)
	}
	if n := e.count(`select count(*) from stays`); n != 1 {
		t.Errorf("stays = %d, want 1", n)
	}
	if n := e.eventRows(); n != 2 {
		t.Errorf("events = %d, want 2", n)
	}
	e.invariant()
}
