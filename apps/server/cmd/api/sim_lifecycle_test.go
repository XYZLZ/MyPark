package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/XYZLZ/MyPark/apps/server/internal/domain/secret"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/devices"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/gate"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/database/dbgen"
	"github.com/XYZLZ/MyPark/apps/server/internal/simulator"
)

var simFleet = secret.Bytes(bytes.Repeat([]byte{9}, 32))

// serverSyncedClock is the simulator's step clock that also moves the API's
// fake clock, so both agree on time (M2C-14).
type serverSyncedClock struct {
	*simulator.StepClock
	server interface{ Set(time.Time) }
}

func (c serverSyncedClock) WaitUntil(ctx context.Context, t time.Time) error {
	if err := c.StepClock.WaitUntil(ctx, t); err != nil {
		return err
	}
	c.server.Set(c.StepClock.Now())
	return nil
}

// cashTap sums the cash the API accepted, as the simulator saw it.
type cashTap struct {
	next  http.RoundTripper
	acked atomic.Int64
}

func (c *cashTap) RoundTrip(r *http.Request) (*http.Response, error) {
	raw, _ := io.ReadAll(r.Body)
	r.Body = io.NopCloser(bytes.NewReader(raw))
	res, err := c.next.RoundTrip(r)
	if err != nil {
		return res, err
	}
	var req struct {
		Type    string `json:"type"`
		Payload struct {
			Amount int64 `json:"amount_cents"`
		} `json:"payload"`
	}
	reply, _ := io.ReadAll(res.Body)
	res.Body = io.NopCloser(bytes.NewReader(reply))
	var ans struct {
		Data struct {
			Result string `json:"result"`
		} `json:"data"`
	}
	if json.Unmarshal(raw, &req) == nil && req.Type == "paystation.cash_paid" &&
		json.Unmarshal(reply, &ans) == nil && ans.Data.Result == "ack" {
		c.acked.Add(req.Payload.Amount)
	}
	return res, err
}

// TestSimulator_CarLifecycleAgainstAPI runs the car lifecycle over the real
// API and database: tickets, employees, a lost ticket, an overstay and a held
// car, then checks the occupancy invariant and the money.
func TestSimulator_CarLifecycleAgainstAPI(t *testing.T) {
	e := newDeviceEnv(t)
	q := dbgen.New(e.db.App)
	if _, err := devices.SeedFleet(t.Context(), q, e.d.sealer, e.lotID, simFleet); err != nil {
		t.Fatalf("seed fleet: %v", err)
	}
	if _, err := gate.SeedCards(t.Context(), q, e.lotID, simFleet, e.cfg.CardUIDPepper()); err != nil {
		t.Fatalf("seed cards: %v", err)
	}

	tap := &cashTap{next: simulator.HandlerTransport(e.h)}
	client := simulator.NewDeviceClient(simulator.ClientConfig{
		BaseURL: "http://api.test", Transport: tap, SignClock: e.clk, FleetSecret: simFleet,
	})
	clk := serverSyncedClock{simulator.NewStepClock(e.clk.Now()), e.clk}
	eng := simulator.NewEngine(clk, 7)
	log := simulator.NewEventLog(500)
	sim := simulator.NewFleetSim(eng, client, simFleet, log)

	held := &simulator.Car{Cred: simulator.CredTicket, Hold: true}
	cars := []*simulator.Car{
		{Cred: simulator.CredTicket, Dwell: 5 * time.Minute},
		{Cred: simulator.CredTicket, Dwell: 50 * time.Minute},
		{Cred: simulator.CredTicket, Dwell: 95 * time.Minute},
		{Cred: simulator.CredTicket, Dwell: 40 * time.Minute, LoseTicket: true},
		{Cred: simulator.CredTicket, Dwell: 40 * time.Minute, PayLate: true},
		{Cred: simulator.CredEmployeeCard, Card: "EMP-001", Dwell: 3 * time.Hour},
		{Cred: simulator.CredEmployeeCard, Card: "EMP-002", Dwell: 3 * time.Hour},
		held,
	}
	for _, c := range cars {
		eng.At(clk.Now().Add(time.Duration(eng.Rand().IntN(600))*time.Second), func(context.Context) { sim.Arrive(c) })
	}
	eng.At(clk.Now().Add(4*time.Hour), func(context.Context) {
		if err := sim.Leave(held.ID, false); err != nil {
			t.Errorf("leave: %v", err)
		}
	})
	if err := eng.Run(t.Context()); err != nil {
		t.Fatalf("run: %v", err)
	}

	overstays := 0
	for _, entry := range log.After(0) {
		if entry.Reason == "gate.payment_required" {
			overstays++
		}
		if entry.Result == "error" || entry.Result == "rejected" || entry.Result == "deny" && entry.Reason != "gate.payment_required" {
			t.Errorf("unexpected answer: %+v", entry)
		}
	}
	if overstays != 2 {
		t.Errorf("payment_required denials = %d, want 2: the late payer, and the grace-period car that walked past its grace", overstays)
	}
	e.invariant()
	var inside int
	if err := e.db.Super.QueryRow(t.Context(),
		`select count(*) from stays where status in ('active', 'paid')`).Scan(&inside); err != nil {
		t.Fatal(err)
	}
	if sim.Inside() != 0 || inside != 0 {
		t.Errorf("cars inside: simulator %d, database %d, want 0", sim.Inside(), inside)
	}
	var exited int
	var paid int64
	if err := e.db.Super.QueryRow(t.Context(),
		`select (select count(*) from stays where status = 'exited'),
		        (select coalesce(sum(amount_cents), 0) from payments where method = 'cash' and status = 'succeeded')`).
		Scan(&exited, &paid); err != nil {
		t.Fatal(err)
	}
	if exited != len(cars) {
		t.Errorf("exited stays = %d, want %d", exited, len(cars))
	}
	if paid == 0 || paid != tap.acked.Load() {
		t.Errorf("cash in payments = %d, cash the simulator saw accepted = %d", paid, tap.acked.Load())
	}
}
