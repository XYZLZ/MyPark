package main

import (
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/XYZLZ/MyPark/apps/server/internal/devicefleet"
	"github.com/XYZLZ/MyPark/apps/server/internal/domain/authz"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/devices"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/database/dbgen"
	"github.com/XYZLZ/MyPark/apps/server/internal/simulator"
)

// TestSimulator_FillToFullAgainstAPI runs fill_to_full over the real API and
// database on the shared clock: the lot reaches 0 available, the occupancy
// invariant holds, and the dispenser denies the next ticket with
// gate.lot_full (M2 exit criterion).
func TestSimulator_FillToFullAgainstAPI(t *testing.T) {
	e := newDeviceEnv(t)
	if _, err := devices.SeedFleet(t.Context(), dbgen.New(e.db.App), e.d.sealer, e.lotID, simFleet); err != nil {
		t.Fatalf("seed fleet: %v", err)
	}
	var publicID uuid.UUID
	if err := e.db.App.QueryRow(t.Context(), "select public_id from lots where id = $1", e.lotID).Scan(&publicID); err != nil {
		t.Fatal(err)
	}

	transport := simulator.HandlerTransport(e.h)
	client := simulator.NewDeviceClient(simulator.ClientConfig{
		BaseURL: "http://api.test", Transport: transport, SignClock: e.clk, FleetSecret: simFleet,
	})
	log := simulator.NewEventLog(5000)
	runner := simulator.NewRunner(simulator.RunnerDeps{
		Client: client, FleetSecret: simFleet, Log: log, Wall: e.clk,
		Spaces: simulator.PublicSpaces(&http.Client{Transport: transport}, "http://api.test"),
		NewClock: func(start time.Time, _ int) simulator.Clock {
			return serverSyncedClock{simulator.NewStepClock(start), e.clk}
		},
	})
	t.Cleanup(func() { _, _ = runner.Stop(false); runner.Wait() })

	if _, err := runner.Start(t.Context(), simulator.StartParams{
		Scenario: simulator.ScenarioFillToFull, Seed: 11, Speed: simulator.DefaultSpeed, StartAt: e.clk.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	// Settled: the lot is full, and every car has reported its space (the last
	// request is out), so stopping cuts nothing mid-flight.
	sensorEvents := func() (n int) {
		for _, entry := range log.After(0) {
			if entry.Type == "sensor.space_changed" {
				n++
			}
		}
		return n
	}
	deadline := time.Now().Add(90 * time.Second)
	for st := runner.Status(); !st.ReachedFull || st.CarsInside != 280 || sensorEvents() != 280; st = runner.Status() {
		if time.Now().After(deadline) {
			t.Fatalf("fill_to_full did not settle: %+v, %d sensor events", st, sensorEvents())
		}
		time.Sleep(5 * time.Millisecond)
	}
	if _, err := runner.Stop(false); err != nil {
		t.Fatal(err)
	}

	rec := e.serve(jsonReq("GET", "/v1/lots/"+publicID.String()+"/availability", nil), "")
	if got := decodeBody(t, rec)["data"].(map[string]any); got["available"] != 0.0 || got["occupied"] != 280.0 {
		t.Errorf("availability = %v, want 0 available, 280 occupied", got)
	}
	rep, err := client.Send(t.Context(), devicefleet.ByKind(authz.DeviceDispenser), simulator.Outgoing{
		EventID: uuid.New(), Type: "entry.credential_presented", Payload: map[string]any{"kind": "ticket_request"},
	})
	if err != nil || rep.Result != "deny" || rep.Reason != "gate.lot_full" {
		t.Errorf("next ticket_request = %+v, %v; want deny gate.lot_full", rep, err)
	}
	for _, entry := range log.After(0) {
		if entry.Result == "error" || entry.Result == "rejected" {
			t.Fatalf("unexpected answer: %+v", entry)
		}
	}
	e.invariant()
}
