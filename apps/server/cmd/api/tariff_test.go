package main

import (
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/XYZLZ/MyPark/apps/server/internal/domain/errcodes"
	"github.com/XYZLZ/MyPark/apps/server/internal/testsupport"
)

func TestGetTariff_Public(t *testing.T) {
	e := newAPIEnv(t)
	lotID := testsupport.SeedPilotLot(t, e.db)
	var publicID uuid.UUID
	if err := e.db.App.QueryRow(t.Context(), "select public_id from lots where id = $1", lotID).Scan(&publicID); err != nil {
		t.Fatal(err)
	}

	rec := e.serve(jsonReq("GET", "/v1/lots/"+publicID.String()+"/tariff", nil), "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body)
	}
	data, _ := decodeBody(t, rec)["data"].(map[string]any)
	want := map[string]any{
		"effective_from": "2026-11-01T04:00:00Z", "currency": "DOP", "first_hour_cents": 5000.0,
		"additional_hour_cents": 2500.0, "exit_grace_minutes": 10.0, "post_payment_exit_minutes": 15.0,
		"daily_cap_cents": 40000.0, "lost_ticket_cents": 50000.0, "reservation_fee_cents": 15000.0,
	}
	if len(data) != len(want) {
		t.Errorf("data has %d fields, want %d: %v", len(data), len(want), data)
	}
	for k, w := range want {
		if data[k] != w {
			t.Errorf("data[%q] = %v, want %v", k, data[k], w)
		}
	}
}

func TestGetTariff_UnknownLot404(t *testing.T) {
	e := newAPIEnv(t)
	testsupport.SeedPilotLot(t, e.db)
	for _, id := range []string{uuid.NewString(), "zona-colonial", "not-a-uuid", "1"} {
		rec := e.serve(jsonReq("GET", "/v1/lots/"+id+"/tariff", nil), "")
		body := decodeBody(t, rec)
		if rec.Code != http.StatusNotFound || errCode(body) != string(errcodes.LotNotFound) {
			t.Errorf("id %q: got %d %q, want 404 lot.not_found (body %s)", id, rec.Code, errCode(body), rec.Body)
		}
	}
}
