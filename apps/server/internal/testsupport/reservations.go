package testsupport

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

// Driver is a seeded driver with one live vehicle.
type Driver struct {
	UserID    int64
	PublicID  uuid.UUID
	VehicleID uuid.UUID
}

// SeedDrivers inserts n verified drivers who accepted the current legal
// versions, each with one vehicle, and returns them in creation order.
func SeedDrivers(t testing.TB, db *TestDB, n int) []Driver {
	t.Helper()
	rows, err := db.Super.Query(t.Context(), `
		with u as (
		    insert into users (email, password_hash, first_name, last_name, role, status, locale, email_verified_at,
		                       terms_version, terms_accepted_at, privacy_version, privacy_accepted_at)
		    select 'seed-' || gen_random_uuid() || '@example.test', 'not-a-real-hash', 'Seed', 'Driver',
		           'driver', 'active', 'en', now(), '2026-11-16', now(), '2026-11-16', now()
		    from generate_series(1, $1::int)
		    returning id, public_id
		), v as (
		    insert into vehicles (user_id, plate_normalized, plate_display, make, model, color)
		    select id, 'SD' || upper(substr(md5(gen_random_uuid()::text), 1, 8)),
		           'SD' || upper(substr(md5(gen_random_uuid()::text), 1, 8)), 'Toyota', 'Corolla', 'Gris'
		    from u
		    returning user_id, public_id
		)
		select u.id, u.public_id, v.public_id from u join v on v.user_id = u.id order by u.id`, n)
	if err != nil {
		t.Fatalf("testsupport: seed %d drivers: %v", n, err)
	}
	defer rows.Close()
	var out []Driver
	for rows.Next() {
		var d Driver
		if err := rows.Scan(&d.UserID, &d.PublicID, &d.VehicleID); err != nil {
			t.Fatalf("testsupport: scan driver: %v", err)
		}
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("testsupport: iterate drivers: %v", err)
	}
	return out
}

// SeedReservations inserts one pending_payment reservation per driver for the
// window [start, end) and the arrival, under the lot's latest tariff and
// policy. It leaves the slot rows alone: pair it with HoldSlots.
func SeedReservations(t testing.TB, db *TestDB, lotID int64, drivers []Driver, arrival, start, end, now time.Time) {
	t.Helper()
	for _, d := range drivers {
		if _, err := db.App.Exec(t.Context(),
			`insert into reservations (user_id, vehicle_id, lot_id, arrival_at, hold_window, status, fee_cents,
			                           tariff_version_id, policy_version_id, payment_due_at, created_at, updated_at)
			 select $1, v.id, $2, $3, tstzrange($4::timestamptz, $5::timestamptz, '[)'), 'pending_payment', 15000,
			        (select id from tariff_versions where lot_id = $2 order by effective_from desc limit 1),
			        (select id from policy_versions where lot_id = $2 order by effective_from desc limit 1),
			        $6::timestamptz + interval '10 minutes', $6, $6
			 from vehicles v where v.public_id = $7`,
			d.UserID, lotID, arrival, start, end, now, d.VehicleID); err != nil {
			t.Fatalf("testsupport: seed reservation for user %d: %v", d.UserID, err)
		}
	}
}
