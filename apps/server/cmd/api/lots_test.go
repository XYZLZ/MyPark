package main

import (
	"encoding/json"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/XYZLZ/MyPark/apps/server/internal/domain/errcodes"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/ratelimit"
	"github.com/XYZLZ/MyPark/apps/server/internal/testsupport"
)

// lotEnv is the production handler over the seeded pilot lot.
type lotEnv struct {
	*apiEnv
	lotID    int64
	publicID string
}

func newLotEnv(t *testing.T, mods ...func(*deps)) *lotEnv {
	t.Helper()
	e := newAPIEnv(t, mods...)
	lotID := testsupport.SeedPilotLot(t, e.db)
	var pub uuid.UUID
	if err := e.db.App.QueryRow(t.Context(), "select public_id from lots where id = $1", lotID).Scan(&pub); err != nil {
		t.Fatal(err)
	}
	return &lotEnv{apiEnv: e, lotID: lotID, publicID: pub.String()}
}

func (e *lotEnv) get(path string) (int, map[string]any) {
	e.t.Helper()
	rec := e.serve(jsonReq("GET", path, nil), "")
	return rec.Code, decodeBody(e.t, rec)
}

func (e *lotEnv) data(path string) map[string]any {
	e.t.Helper()
	code, body := e.get(path)
	if code != http.StatusOK {
		e.t.Fatalf("GET %s = %d %v, want 200", path, code, body)
	}
	d, _ := body["data"].(map[string]any)
	return d
}

func TestListLots(t *testing.T) {
	e := newLotEnv(t)
	code, body := e.get("/v1/lots")
	if code != http.StatusOK {
		t.Fatalf("status = %d %v", code, body)
	}
	want := []any{map[string]any{"id": e.publicID, "name": "Parqueo Banreservas Zona Colonial", "slug": "zona-colonial"}}
	if !reflect.DeepEqual(body["data"], want) {
		t.Errorf("data = %v, want %v", body["data"], want)
	}
	meta, _ := body["meta"].(map[string]any)
	page, _ := meta["page"].(map[string]any)
	if v, ok := page["next_cursor"]; !ok || v != nil {
		t.Errorf("meta.page = %v, want next_cursor null", page)
	}
}

func TestGetLot(t *testing.T) {
	e := newLotEnv(t)
	d := e.data("/v1/lots/" + e.publicID)
	want := map[string]any{
		"id": e.publicID, "slug": "zona-colonial", "name": "Parqueo Banreservas Zona Colonial",
		"address":       "Calle Arzobispo Meriño No. 386, Zona Colonial, Santo Domingo",
		"latitude":      nil,
		"longitude":     nil,
		"map_url":       "https://www.google.com/maps/search/?api=1&query=Calle+Arzobispo+Meri%C3%B1o+No.+386%2C+Zona+Colonial%2C+Santo+Domingo",
		"timezone":      "America/Santo_Domingo",
		"capacity":      280.0,
		"opening_hours": map[string]any{"always_open": true},
		"status":        "open",
	}
	if !reflect.DeepEqual(d, want) {
		t.Errorf("lot = %v\nwant  %v", d, want)
	}
}

func TestGetLot_CoordinatesAndStatus(t *testing.T) {
	e := newLotEnv(t)
	if _, err := e.db.Super.Exec(t.Context(),
		`update lots set latitude = 18.4735, longitude = -69.8849, status = 'closed' where id = $1`, e.lotID); err != nil {
		t.Fatal(err)
	}
	d := e.data("/v1/lots/" + e.publicID)
	if d["latitude"] != 18.4735 || d["longitude"] != -69.8849 || d["status"] != "closed" {
		t.Errorf("lot = %v", d)
	}
	if got := d["map_url"]; got != "https://www.google.com/maps/search/?api=1&query=18.4735%2C-69.8849" {
		t.Errorf("map_url = %v", got)
	}
}

func TestGetLot_FullStatus(t *testing.T) {
	e := newLotEnv(t)
	testsupport.FillLot(t, e.db, e.lotID, 280, e.clk.Now())
	if got := e.data("/v1/lots/" + e.publicID)["status"]; got != "full" {
		t.Errorf("status = %v, want full", got)
	}
}

func TestAvailability_Snapshot(t *testing.T) {
	e := newLotEnv(t)
	d := e.data("/v1/lots/" + e.publicID + "/availability")
	if d["available"] != 280.0 || d["occupied"] != 0.0 || d["status"] != "open" {
		t.Fatalf("empty lot = %v", d)
	}

	passAt := e.clk.Now().Add(-5 * time.Minute)
	testsupport.FillLot(t, e.db, e.lotID, 1, passAt)
	d = e.data("/v1/lots/" + e.publicID + "/availability")
	want := map[string]any{
		"lot_id": e.publicID, "available": 279.0, "occupied": 1.0, "effective_capacity": 280.0, "capacity": 280.0,
		"effective_holds": 0.0, "upcoming_reservations_2h": 0.0, "status": "open", "version": 1.0,
		"updated_at": passAt.UTC().Format(time.RFC3339),
	}
	if !reflect.DeepEqual(d, want) {
		t.Errorf("availability = %v\nwant         %v", d, want)
	}
}

func TestAvailability_OutOfServiceAndFull(t *testing.T) {
	e := newLotEnv(t)
	if _, err := e.db.App.Exec(t.Context(),
		`update spaces set status = 'out_of_service' where lot_id = $1 and code in ('E001','E002','E003')`, e.lotID); err != nil {
		t.Fatal(err)
	}
	testsupport.FillLot(t, e.db, e.lotID, 277, e.clk.Now())
	d := e.data("/v1/lots/" + e.publicID + "/availability")
	if d["effective_capacity"] != 277.0 || d["available"] != 0.0 || d["status"] != "full" || d["capacity"] != 280.0 {
		t.Errorf("availability = %v", d)
	}
}

func TestAvailability_PendingDoesNotChangeDisplay(t *testing.T) {
	e := newLotEnv(t)
	if _, err := e.db.App.Exec(t.Context(),
		`insert into stays (lot_id, credential_kind, ticket_code_hash, status, tariff_version_id, accepted_at, updated_at)
		 select $1, 'ticket', sha256('pending'::bytea), 'entry_pending', id, $2, $2
		 from tariff_versions where lot_id = $1`, e.lotID, e.clk.Now().Add(-10*time.Second)); err != nil {
		t.Fatal(err)
	}
	d := e.data("/v1/lots/" + e.publicID + "/availability")
	if d["available"] != 280.0 || d["occupied"] != 0.0 {
		t.Errorf("availability = %v, want the pending entry not counted", d)
	}
}

func TestSpaces_280OrderedNoPrivateData(t *testing.T) {
	e := newLotEnv(t)
	if _, err := e.db.App.Exec(t.Context(),
		`update spaces set status = case code when 'E002' then 'occupied' else 'out_of_service' end where lot_id = $1 and code in ('E002', 'E003')`, e.lotID); err != nil {
		t.Fatal(err)
	}
	spaces, _ := e.data("/v1/lots/" + e.publicID + "/spaces")["spaces"].([]any)
	if len(spaces) != 280 {
		t.Fatalf("spaces = %d, want 280", len(spaces))
	}
	codes := make([]string, 0, len(spaces))
	states := map[string]string{}
	for _, s := range spaces {
		m, _ := s.(map[string]any)
		keys := make([]string, 0, 2)
		for k := range m {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		if !slices.Equal(keys, []string{"code", "state"}) {
			t.Fatalf("space keys = %v, want exactly code and state", keys)
		}
		code, _ := m["code"].(string)
		codes = append(codes, code)
		states[code], _ = m["state"].(string)
	}
	if codes[0] != "E001" || codes[279] != "E280" || !slices.IsSorted(codes) {
		t.Errorf("codes not ordered E001..E280: %v ... %v", codes[0], codes[279])
	}
	if states["E001"] != "free" || states["E002"] != "occupied" || states["E003"] != "out_of_service" {
		t.Errorf("states = %v %v %v", states["E001"], states["E002"], states["E003"])
	}
}

// Review focus 4: whatever the id, an unknown lot is one 404 with one body.
func TestLots_GarbageIDIs404(t *testing.T) {
	e := newLotEnv(t)
	suffixes := []string{"", "/availability", "/spaces", "/tariff"}
	var first string
	for _, id := range []string{"zona-colonial", "123", uuid.NewString(), "not-a-uuid"} {
		for _, suf := range suffixes {
			rec := e.serve(jsonReq("GET", "/v1/lots/"+id+suf, nil), "")
			body := decodeBody(t, rec)
			if rec.Code != http.StatusNotFound || errCode(body) != string(errcodes.LotNotFound) {
				t.Fatalf("%s%s: got %d %q, want 404 lot.not_found", id, suf, rec.Code, errCode(body))
			}
			delete(body["meta"].(map[string]any), "request_id")
			got := strings.TrimSpace(toJSON(t, body))
			if first == "" {
				first = got
			} else if got != first {
				t.Errorf("%s%s body = %s, differs from %s", id, suf, got, first)
			}
		}
	}
}

func TestPublicRead_RateLimited(t *testing.T) {
	e := newLotEnv(t, func(d *deps) { d.limiter = ratelimit.NewMemory(d.clock) })
	for i := range 120 {
		if code, body := e.get("/v1/lots"); code != http.StatusOK {
			t.Fatalf("request %d = %d %v, want 200", i+1, code, body)
		}
	}
	rec := e.serve(jsonReq("GET", "/v1/lots", nil), "")
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") == "" {
		t.Fatalf("121st = %d (Retry-After %q), want 429 with Retry-After", rec.Code, rec.Header().Get("Retry-After"))
	}
}

// toJSON encodes v with sorted map keys, so equal bodies compare equal.
func toJSON(t *testing.T, v any) string {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}
