package main

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/XYZLZ/MyPark/apps/server/internal/devicefleet"
	"github.com/XYZLZ/MyPark/apps/server/internal/domain/authz"
	"github.com/XYZLZ/MyPark/apps/server/internal/simulator"
	"github.com/XYZLZ/MyPark/apps/server/internal/testsupport"
)

// TestScenario_Guarantee (G2, FR-RES-6): the lot is one space from full, a
// simulated driver books and pays for that space, and once the hold is
// effective drive-ups are refused while the reserved car gets in. The lot is
// never physically full here, so the guarantee is exercised by admission (the
// refund path of the physically full lot is covered by TestGateQR_PhysicallyFull).
func TestScenario_Guarantee(t *testing.T) {
	start := weekdayStart.Add(2 * time.Minute) // 07:02: the next 5-minute slot is 07:05
	r := newScenarioRig(t, start)
	r.withDrivers()
	testsupport.FillLot(t, r.db, r.lotID, 279, start)
	r.start(simulator.ScenarioIdle, 42)

	arrival := start.Truncate(5 * time.Minute).Add(5*time.Minute + 15*time.Minute) // ceil5(now) + 15m
	if err := r.drivers.Login(t.Context(), 1); err != nil {
		t.Fatalf("login: %v", err)
	}
	vehicle, err := r.drivers.Vehicle(t.Context(), 1)
	if err != nil {
		t.Fatalf("vehicle: %v", err)
	}
	rsv, err := r.drivers.Create(t.Context(), 1, arrival, vehicle)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if status, err := r.drivers.Pay(t.Context(), 1, rsv.ID, uuid.NewString()); err != nil || status != "confirmed" {
		t.Fatalf("pay = %q, %v, want confirmed", status, err)
	}
	qr, err := r.drivers.QR(t.Context(), 1, rsv.ID)
	if err != nil {
		t.Fatalf("qr: %v", err)
	}

	// Before the window opens the hold is not effective: the last space is
	// still public. At the boundary tick it becomes effective.
	if got := r.availability(); got["available"] != 1.0 || got["effective_holds"] != 0.0 {
		t.Fatalf("before the window: availability = %v, want 1 available and no effective hold", got)
	}
	windowOpens := arrival.Add(-15 * time.Minute)
	r.pace.runTo(t, windowOpens)
	if got := r.availability(); got["available"] != 0.0 || got["effective_holds"] != 1.0 {
		t.Fatalf("at the boundary: availability = %v, want 0 available and 1 effective hold", got)
	}
	rep, err := r.client.Send(t.Context(), devicefleet.ByKind(authz.DeviceDispenser), simulator.Outgoing{
		EventID: uuid.New(), Type: "entry.credential_presented", Payload: map[string]any{"kind": "ticket_request"},
	})
	if err != nil || rep.Result != "deny" || rep.Reason != "gate.lot_full" {
		t.Fatalf("drive-up ticket_request = %+v, %v; want deny gate.lot_full", rep, err)
	}

	// The reserved car presents its QR and gets in.
	if _, err := r.runner.Inject(simulator.InjectParams{Cred: simulator.CredReservationQR, QR: qr, DwellMinutes: 120}); err != nil {
		t.Fatalf("inject: %v", err)
	}
	r.pace.runTo(t, windowOpens.Add(5*time.Minute))
	if open, other := r.qrEntries(); open != 1 || other != 0 {
		t.Fatalf("QR entries: %d open, %d other; want 1 open", open, other)
	}
	if n := r.count(`select count(*) from reservations where public_id = $1 and status = 'checked_in'`, rsv.ID); n != 1 {
		t.Fatalf("reservation checked_in = %d, want 1", n)
	}

	// After its 2-hour dwell the car pays at the pay station and leaves; the
	// reservation completes. The drain then finds nothing to move.
	r.pace.runTo(t, windowOpens.Add(3*time.Hour))
	r.drain()
	if n := r.count(`select count(*) from reservations where public_id = $1 and status = 'completed'`, rsv.ID); n != 1 {
		t.Errorf("reservation completed = %d, want 1", n)
	}
	if n := r.count(`select count(*) from payments p join stays s on s.id = p.stay_id
	                 where s.reservation_id = (select id from reservations where public_id = $1) and p.amount_cents > 0`, rsv.ID); n < 1 {
		t.Errorf("pay-station payments with an amount for the reserved stay = %d, want at least 1", n)
	}
	if st := r.runner.Status(); st.CarsInside != 0 {
		t.Errorf("cars inside after the drain = %d, want 0", st.CarsInside)
	}
	r.finish()
}

// TestScenario_ReservationArrivals: two virtual hours of weekday traffic plus
// reserved cars booked at 8 an hour. Every reserved car reaches the gate inside
// its window and gets in; the pacer checks the occupancy and slot invariants
// after every step; a drain completes every reservation.
func TestScenario_ReservationArrivals(t *testing.T) {
	r := newScenarioRig(t, weekdayStart)
	r.withDrivers()
	r.start(simulator.ScenarioReservationArrivals, 42)
	r.pace.runTo(t, weekdayStart.Add(2*time.Hour))
	if r.pace.steps < 100 {
		t.Fatalf("only %d engine steps ran; the invariant checks would prove little", r.pace.steps)
	}
	r.runQuiet()
	testsupport.AssertSlotInvariant(t, r.d.pool, r.lotID)

	booked := r.count(`select count(*) from reservations`)
	if booked < 5 {
		t.Fatalf("only %d reservations were booked in two hours; the scenario proved little", booked)
	}
	r.drain()

	open, other := r.qrEntries()
	if other != 0 || int64(open) != booked {
		t.Errorf("QR entries: %d open, %d not open; want all %d reserved cars in", open, other, booked)
	}
	if n := r.count(`select count(*) from reservations where status = 'completed'`); n != booked {
		t.Errorf("completed reservations = %d, want %d (every car entered and left)", n, booked)
	}
	if st := r.runner.Status(); st.CarsInside != 0 || st.Running {
		t.Errorf("after the drain: %+v, want no cars and not running", st)
	}
	r.finish()
}
