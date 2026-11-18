package testsupport

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// AssertSlotInvariant fails t unless every slot row of the lot holds exactly
// the number of pending_payment and confirmed reservations whose hold window
// covers it, and the held total equals the slots those reservations span
// (so a reservation without slot rows is caught too). Call it at the end of
// every reservation integration test. A read error is fatal; use
// CheckSlotInvariant off the test goroutine.
func AssertSlotInvariant(t testing.TB, db *pgxpool.Pool, lotID int64) {
	t.Helper()
	broken, err := CheckSlotInvariant(t.Context(), db, lotID)
	if err != nil {
		t.Fatalf("testsupport: %v", err)
	}
	for _, msg := range broken {
		t.Error(msg)
	}
}

// CheckSlotInvariant is the non-fatal form of AssertSlotInvariant, safe on any
// goroutine: it returns one message per broken slot or total, and an error when
// the database cannot be read.
func CheckSlotInvariant(ctx context.Context, db *pgxpool.Pool, lotID int64) (broken []string, err error) {
	rows, err := db.Query(ctx,
		`select s.slot_start, s.held,
		        (select count(*) from reservations r
		          where r.lot_id = s.lot_id and r.status in ('pending_payment', 'confirmed')
		            and r.hold_window @> s.slot_start)
		 from reservation_slot_loads s where s.lot_id = $1 order by s.slot_start`, lotID)
	if err != nil {
		return nil, fmt.Errorf("read slot loads of lot %d: %w", lotID, err)
	}
	defer rows.Close()
	for rows.Next() {
		var start time.Time
		var held, covering int64
		if err := rows.Scan(&start, &held, &covering); err != nil {
			return nil, fmt.Errorf("scan slot load: %w", err)
		}
		if held != covering {
			broken = append(broken, fmt.Sprintf("slot invariant broken for lot %d at %s: held = %d, active reservations covering = %d",
				lotID, start.UTC().Format(time.RFC3339), held, covering))
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate slot loads: %w", err)
	}
	var total, spanned int64
	if err := db.QueryRow(ctx,
		`select coalesce((select sum(held) from reservation_slot_loads where lot_id = $1), 0),
		        coalesce((select sum(extract(epoch from upper(hold_window) - lower(hold_window)) / 300)
		                    from reservations
		                   where lot_id = $1 and status in ('pending_payment', 'confirmed')), 0)`,
		lotID).Scan(&total, &spanned); err != nil {
		return nil, fmt.Errorf("sum slot loads of lot %d: %w", lotID, err)
	}
	if total != spanned {
		broken = append(broken, fmt.Sprintf("slot invariant broken for lot %d: held total = %d, slots spanned by active reservations = %d",
			lotID, total, spanned))
	}
	return broken, nil
}

// HoldSlots sets held on every 5-minute slot in [start, end), creating the
// rows, without any reservation behind them. It is for cap edges; pair it
// with SeedReservations when the invariant must hold.
func HoldSlots(t testing.TB, db *TestDB, lotID int64, start, end time.Time, held int) {
	t.Helper()
	if _, err := db.App.Exec(t.Context(),
		`insert into reservation_slot_loads (lot_id, slot_start, held)
		 select $1, s, $4 from generate_series($2::timestamptz, $3::timestamptz - interval '5 minutes', interval '5 minutes') s
		 on conflict (lot_id, slot_start) do update set held = excluded.held`,
		lotID, start, end, held); err != nil {
		t.Fatalf("testsupport: hold slots of lot %d: %v", lotID, err)
	}
}
