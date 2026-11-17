package main

import (
	"context"
	"encoding/base64"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/XYZLZ/MyPark/apps/server/internal/config"
	"github.com/XYZLZ/MyPark/apps/server/internal/devicefleet"
	"github.com/XYZLZ/MyPark/apps/server/internal/domain/secret"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/auth"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/legal"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/vehicles"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/database"
	"github.com/XYZLZ/MyPark/apps/server/internal/simulator"
)

// TestDriverClient_Flow drives the production handler through the simulator's
// driver client: seeded drivers log in with their derived passwords, then one
// quotes, reserves, pays and fetches the QR over HTTP only.
func TestDriverClient_Flow(t *testing.T) {
	e := newFeeEnv(t)
	raw, err := base64.StdEncoding.DecodeString(config.DevSimulatorFleetSecret)
	if err != nil {
		t.Fatal(err)
	}
	fleet := secret.Bytes(raw)
	err = database.NewTxRunner(e.d.pool).InTx(t.Context(), func(ctx context.Context) error {
		for n := 1; n <= devicefleet.SimDriverCount; n++ {
			email := devicefleet.SimDriverEmail(n)
			if _, err := auth.SeedDriver(ctx, auth.SeedDeps{Hasher: e.hasher, Clock: e.clk}, auth.SeedDriverInput{
				Email: email, FirstName: "Sim", LastName: "Driver", Password: devicefleet.SimDriverPassword(fleet, email),
				TermsVersion: legal.InitialVersion, PrivacyVersion: legal.InitialVersion,
			}); err != nil {
				return err
			}
			id, err := auth.SeedDriverID(ctx, email)
			if err != nil {
				return err
			}
			if err := vehicles.SeedVehicle(ctx, id, devicefleet.SimDriverPlate(n), "Toyota", "Corolla", "Gris", e.clk.Now()); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	srv := httptest.NewServer(e.h)
	t.Cleanup(srv.Close)
	c := simulator.NewDriverClient(simulator.DriverClientConfig{
		BaseURL: srv.URL, Fleet: fleet, Clock: e.clk, LotID: uuid.MustParse(e.publicID),
	})
	for n := 1; n <= devicefleet.SimDriverCount; n++ {
		if err := c.Login(t.Context(), n); err != nil {
			t.Fatalf("login driver %d: %v", n, err)
		}
	}
	arrival := time.Date(2026, 12, 1, 14, 2, 0, 0, time.UTC)
	vehicleID, err := c.Vehicle(t.Context(), 1)
	if err != nil {
		t.Fatalf("Vehicle: %v", err)
	}
	quote, err := c.Quote(t.Context(), 1, arrival)
	if err != nil || quote.FeeCents <= 0 {
		t.Fatalf("Quote = %+v, %v", quote, err)
	}
	rsv, err := c.Create(t.Context(), 1, arrival, vehicleID)
	if err != nil || rsv.Status != "pending_payment" || rsv.FeeCents != quote.FeeCents {
		t.Fatalf("Create = %+v, %v", rsv, err)
	}
	if status, err := c.Pay(t.Context(), 1, rsv.ID, "sim-key-000001"); err != nil || status != "confirmed" {
		t.Fatalf("Pay = %q, %v", status, err)
	}
	token, err := c.QR(t.Context(), 1, rsv.ID)
	if err != nil || len(token) != 86 {
		t.Fatalf("QR: %d chars, %v, want 86", len(token), err)
	}
}
