package main

import (
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/XYZLZ/MyPark/apps/server/internal/features/reservations"
	"github.com/XYZLZ/MyPark/apps/server/internal/testsupport"
)

// seedRes inserts a reservation of d straight into the database, with the
// hold window [arrival-15m, arrival+20m). A pending_payment or confirmed one
// also takes its slots, so the slot invariant stays true.
func (e *rsvEnv) seedRes(d testsupport.Driver, status string, arrival time.Time) uuid.UUID {
	e.t.Helper()
	start, end := arrival.Add(-15*time.Minute), arrival.Add(20*time.Minute)
	var id uuid.UUID
	err := e.db.Super.QueryRow(e.t.Context(),
		`insert into reservations (user_id, vehicle_id, lot_id, arrival_at, hold_window, status, fee_cents,
		                           tariff_version_id, policy_version_id, payment_due_at, created_at, updated_at,
		                           confirmed_at, qr_nonce, cancelled_at, cancelled_by, checked_in_at, completed_at, expired_at)
		 select $1, v.id, $2, $3::timestamptz, tstzrange($4::timestamptz, $5::timestamptz, '[)'), $6::text, 15000,
		        (select id from tariff_versions where lot_id = $2 order by effective_from desc limit 1),
		        (select id from policy_versions where lot_id = $2 order by effective_from desc limit 1),
		        $7::timestamptz + interval '10 minutes', $7::timestamptz, $7::timestamptz,
		        case when $6::text in ('confirmed', 'checked_in', 'completed') then $7::timestamptz end,
		        case when $6::text in ('confirmed', 'checked_in', 'completed') then decode(md5(random()::text), 'hex') end,
		        case when $6::text = 'cancelled' then $7::timestamptz end,
		        case when $6::text = 'cancelled' then 'driver' end,
		        case when $6::text in ('checked_in', 'completed') then $7::timestamptz end,
		        case when $6::text = 'completed' then $7::timestamptz end,
		        case when $6::text like 'expired%' then $7::timestamptz end
		 from vehicles v where v.public_id = $8
		 returning public_id`,
		d.UserID, e.lotID, arrival, start, end, status, rsvNow, d.VehicleID).Scan(&id)
	if err != nil {
		e.t.Fatalf("seed %s reservation: %v", status, err)
	}
	if status == "pending_payment" || status == "confirmed" {
		if _, err := e.db.Super.Exec(e.t.Context(),
			`insert into reservation_slot_loads (lot_id, slot_start, held)
			 select $1, s, 1 from generate_series($2::timestamptz, $3::timestamptz - interval '5 minutes', interval '5 minutes') s
			 on conflict (lot_id, slot_start) do update set held = reservation_slot_loads.held + 1`,
			e.lotID, start, end); err != nil {
			e.t.Fatalf("hold slots: %v", err)
		}
	}
	return id
}

func TestHolds_EffectiveAndUpcoming(t *testing.T) {
	e := newRsvEnv(t)
	ds := testsupport.SeedDrivers(t, e.db, 3)
	at := func(h, m int) time.Time { return time.Date(2026, 12, 1, h, m, 0, 0, time.UTC) }
	e.seedRes(ds[0], "confirmed", at(15, 0)) // window [14:45, 15:20)
	e.seedRes(ds[1], "confirmed", at(17, 0)) // window [16:45, 17:20)
	e.seedRes(ds[2], "pending_payment", at(15, 0))

	h := reservations.NewHolds(e.db.App)
	for _, c := range []struct {
		now  time.Time
		want int
	}{{at(15, 0), 1}, {at(15, 20), 0}, {at(14, 44).Add(59 * time.Second), 0}, {at(14, 45), 1}} {
		got, err := h.EffectiveHolds(t.Context(), e.lotID, c.now)
		if err != nil || got != c.want {
			t.Errorf("EffectiveHolds(%s) = %d, %v, want %d", c.now.Format("15:04:05"), got, err, c.want)
		}
	}
	// Arrivals at 15:00 and 17:00: only the first is within (14:00, 16:00].
	if got, err := h.UpcomingArrivals(t.Context(), e.lotID, at(14, 0), 2*time.Hour); err != nil || got != 1 {
		t.Errorf("UpcomingArrivals(14:00, 2h) = %d, %v, want 1", got, err)
	}
	if got, err := h.UpcomingArrivals(t.Context(), e.lotID, at(15, 0), time.Hour); err != nil || got != 0 {
		t.Errorf("UpcomingArrivals(15:00, 1h) = %d, %v, want 0 (arrival at now is not upcoming)", got, err)
	}
	if got, err := h.UpcomingArrivals(t.Context(), e.lotID, at(14, 0), time.Hour); err != nil || got != 1 {
		t.Errorf("UpcomingArrivals(14:00, 1h) = %d, %v, want 1 (upper bound inclusive)", got, err)
	}
	testsupport.AssertSlotInvariant(t, e.db.App, e.lotID)
}

func TestAvailability_NetsHolds(t *testing.T) {
	e := newRsvEnv(t)
	d := testsupport.SeedDrivers(t, e.db, 1)[0]
	e.seedRes(d, "confirmed", rsvAt(10*time.Minute)) // window [13:55, 14:30) holds rsvNow
	data := e.data("/v1/lots/" + e.publicID + "/availability")
	if data["effective_holds"] != 1.0 || data["available"] != 279.0 || data["upcoming_reservations_2h"] != 1.0 {
		t.Errorf("availability = %v, want effective_holds 1, available 279, upcoming 1", data)
	}
	testsupport.AssertSlotInvariant(t, e.db.App, e.lotID)
}

func TestVehicleDelete_InUseForEachActiveStatus(t *testing.T) {
	for _, status := range []string{"pending_payment", "confirmed", "checked_in", "cancelled"} {
		t.Run(status, func(t *testing.T) {
			e := newRsvEnv(t)
			d := testsupport.SeedDrivers(t, e.db, 1)[0]
			e.seedRes(d, status, rsvAt(time.Hour))
			rec := e.serve(jsonReq(http.MethodDelete, "/v1/vehicles/"+d.VehicleID.String(), nil), e.token(d))
			if status == "cancelled" {
				if rec.Code != http.StatusNoContent {
					t.Fatalf("delete = %d %s, want 204", rec.Code, rec.Body)
				}
			} else if got := errCode(decodeBody(t, rec)); rec.Code != http.StatusConflict || got != "vehicle.in_use" {
				t.Fatalf("delete = %d %s, want 409 vehicle.in_use", rec.Code, rec.Body)
			}
			testsupport.AssertSlotInvariant(t, e.db.App, e.lotID)
		})
	}
}

// M3B-31: an in-flight create holds the vehicle FOR SHARE until it commits, so
// a delete cannot slip between the create's vehicle check and its insert.
func TestCreate_VehicleDeleteSerialized(t *testing.T) {
	e := newRsvEnv(t)
	d := testsupport.SeedDrivers(t, e.db, 1)[0]
	tok := e.token(d)
	tx, err := e.db.Super.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(t.Context()) }()
	if _, err := tx.Exec(t.Context(), `select 1 from vehicles where public_id = $1 for share`, d.VehicleID); err != nil {
		t.Fatal(err)
	}
	type res struct {
		code int
		body map[string]any
	}
	done := make(chan res, 1)
	go func() {
		rec := e.serve(jsonReq(http.MethodDelete, "/v1/vehicles/"+d.VehicleID.String(), nil), tok)
		done <- res{rec.Code, decodeBody(t, rec)}
	}()
	if _, err := tx.Exec(t.Context(),
		`insert into reservations (user_id, vehicle_id, lot_id, arrival_at, hold_window, status, fee_cents,
		                           tariff_version_id, policy_version_id, payment_due_at, created_at, updated_at)
		 select $1, v.id, $2, $3::timestamptz, tstzrange($3::timestamptz - interval '15 minutes', $3::timestamptz + interval '20 minutes', '[)'),
		        'pending_payment', 15000,
		        (select id from tariff_versions where lot_id = $2 order by effective_from desc limit 1),
		        (select id from policy_versions where lot_id = $2 order by effective_from desc limit 1),
		        $4::timestamptz, $4::timestamptz, $4::timestamptz
		 from vehicles v where v.public_id = $5`,
		d.UserID, e.lotID, rsvNow.Add(time.Hour), rsvNow, d.VehicleID); err != nil {
		t.Fatal(err)
	}
	select {
	case r := <-done:
		t.Fatalf("delete answered %d %v while the vehicle was share-locked", r.code, r.body)
	case <-time.After(300 * time.Millisecond):
	}
	if err := tx.Commit(t.Context()); err != nil {
		t.Fatal(err)
	}
	select {
	case r := <-done:
		if r.code != http.StatusConflict || errCode(r.body) != "vehicle.in_use" {
			t.Fatalf("delete = %d %v, want 409 vehicle.in_use", r.code, r.body)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("delete never answered after the create committed")
	}
	if n := e.count(`select count(*) from vehicles where public_id = $1 and deleted_at is null`, d.VehicleID); n != 1 {
		t.Errorf("live vehicles = %d, want 1", n)
	}
}

// rsvAt is rsvNow plus d on the 5-minute grid, as the slot rows need.
func rsvAt(d time.Duration) time.Time { return reservations.Ceil5(rsvNow.Add(d)) }
