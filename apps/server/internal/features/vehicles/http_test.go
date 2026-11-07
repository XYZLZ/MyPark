package vehicles_test

import (
	"net/http"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/XYZLZ/MyPark/apps/server/internal/domain/authz"
)

func TestCreate_AndListOwnOnly(t *testing.T) {
	e := newEnv(t)
	a, b := e.newUser("a", authz.RoleDriver), e.newUser("b", authz.RoleDriver)
	r := e.do(&a, http.MethodPost, "/v1/vehicles", vehicleBody("A-123 456"))
	if r.Code != http.StatusCreated {
		t.Fatalf("status = %d: %s", r.Code, r.Body)
	}
	d := r.data()
	if d["plate"] != "A-123 456" || d["plate_normalized"] != "A123456" || d["make"] != "Toyota" {
		t.Errorf("data = %#v", d)
	}
	if _, err := uuid.Parse(d["id"].(string)); err != nil {
		t.Errorf("id = %v", d["id"])
	}
	if got := e.do(&a, http.MethodGet, "/v1/vehicles", nil).list(); len(got) != 1 {
		t.Errorf("owner list = %d items, want 1", len(got))
	}
	if got := e.do(&b, http.MethodGet, "/v1/vehicles", nil); got.Code != 200 || len(got.list()) != 0 {
		t.Errorf("other driver list = %d %s, want empty", got.Code, got.Body)
	}
}

func TestCreate_PlateTakenAcrossAccounts(t *testing.T) {
	e := newEnv(t)
	a, b := e.newUser("a", authz.RoleDriver), e.newUser("b", authz.RoleDriver)
	e.create(&a, "ABC 123")
	for name, tc := range map[string]struct {
		u     *user
		plate string
	}{"other driver": {&b, "abc-123"}, "same driver": {&a, "ABC123"}} {
		r := e.do(tc.u, http.MethodPost, "/v1/vehicles", vehicleBody(tc.plate))
		if r.Code != http.StatusConflict || r.errCode() != "vehicle.plate_taken" {
			t.Errorf("%s: %d %s", name, r.Code, r.Body)
		}
	}
}

func TestCreate_ReuseDeletedPlate(t *testing.T) {
	e := newEnv(t)
	a, b := e.newUser("a", authz.RoleDriver), e.newUser("b", authz.RoleDriver)
	id := e.create(&a, "ABC123")
	if r := e.do(&a, http.MethodDelete, "/v1/vehicles/"+id, nil); r.Code != http.StatusNoContent || r.Body.Len() != 0 {
		t.Fatalf("delete: %d %s", r.Code, r.Body)
	}
	if r := e.do(&b, http.MethodPost, "/v1/vehicles", vehicleBody("ABC123")); r.Code != http.StatusCreated {
		t.Errorf("reuse: %d %s", r.Code, r.Body)
	}
	if r := e.do(&a, http.MethodGet, "/v1/vehicles/"+id, nil); r.Code != http.StatusNotFound {
		t.Errorf("deleted vehicle get: %d", r.Code)
	}
}

func TestCreate_LimitReached(t *testing.T) {
	e := newEnv(t)
	a := e.newUser("a", authz.RoleDriver)
	for i := range e.max {
		e.create(&a, plateN(i))
	}
	r := e.do(&a, http.MethodPost, "/v1/vehicles", vehicleBody("EXTRA1"))
	if r.Code != http.StatusConflict || r.errCode() != "vehicle.limit_reached" {
		t.Fatalf("%d %s", r.Code, r.Body)
	}
	details := r.json()["error"].(map[string]any)["details"]
	if !reflect.DeepEqual(details, map[string]any{"limit": float64(e.max)}) {
		t.Errorf("details = %#v", details)
	}
}

func TestVehicles_ConcurrentCreateRespectsLimit(t *testing.T) {
	e := newEnv(t)
	a := e.newUser("a", authz.RoleDriver)
	var wg sync.WaitGroup
	var mu sync.Mutex
	codes := map[string]int{}
	for i := range 10 {
		wg.Go(func() {
			r := e.do(&a, http.MethodPost, "/v1/vehicles", vehicleBody(plateN(i)))
			key := r.errCode()
			if r.Code == http.StatusCreated {
				key = "created"
			}
			mu.Lock()
			codes[key]++
			mu.Unlock()
		})
	}
	wg.Wait()
	if codes["created"] != e.max || codes["vehicle.limit_reached"] != 10-e.max {
		t.Errorf("outcomes = %v", codes)
	}
	if n := e.count(`select count(*) from vehicles where user_id = $1`, a.ID); n != int64(e.max) {
		t.Errorf("rows = %d", n)
	}
}

func TestVehicles_ConcurrentSamePlateTwoUsers(t *testing.T) {
	e := newEnv(t)
	a, b := e.newUser("a", authz.RoleDriver), e.newUser("b", authz.RoleDriver)
	var wg sync.WaitGroup
	res := make([]resp, 2)
	for i, u := range []*user{&a, &b} {
		wg.Go(func() { res[i] = e.do(u, http.MethodPost, "/v1/vehicles", vehicleBody("RACE 77")) })
	}
	wg.Wait()
	created, taken := 0, 0
	for _, r := range res {
		switch {
		case r.Code == http.StatusCreated:
			created++
		case r.Code == http.StatusConflict && r.errCode() == "vehicle.plate_taken":
			taken++
		}
	}
	if created != 1 || taken != 1 {
		t.Errorf("created=%d taken=%d (%s | %s)", created, taken, res[0].Body, res[1].Body)
	}
}

func TestOwnership_OtherDriverGets404Identical(t *testing.T) {
	e := newEnv(t)
	a, b := e.newUser("a", authz.RoleDriver), e.newUser("b", authz.RoleDriver)
	id := e.create(&a, "ABC123")
	ids := []string{id, uuid.NewString(), "not-a-uuid"}
	for _, m := range []struct {
		method string
		body   any
	}{{http.MethodGet, nil}, {http.MethodPatch, map[string]any{"color": "Rojo"}}, {http.MethodDelete, nil}} {
		var bodies []map[string]any
		for _, i := range ids {
			r := e.do(&b, m.method, "/v1/vehicles/"+i, m.body)
			if r.Code != http.StatusNotFound || r.errCode() != "vehicle.not_found" {
				t.Fatalf("%s %s: %d %s", m.method, i, r.Code, r.Body)
			}
			bodies = append(bodies, r.normalized())
		}
		if !reflect.DeepEqual(bodies[0], bodies[1]) || !reflect.DeepEqual(bodies[0], bodies[2]) {
			t.Errorf("%s: bodies differ: %v", m.method, bodies)
		}
	}
	// The owner's vehicle is untouched.
	if r := e.do(&a, http.MethodGet, "/v1/vehicles/"+id, nil); r.Code != 200 || r.data()["color"] != "Gris" {
		t.Errorf("owner get: %d %s", r.Code, r.Body)
	}
}

func TestUpdate_RenormalizesAndBumpsUpdatedAt(t *testing.T) {
	e := newEnv(t)
	a := e.newUser("a", authz.RoleDriver)
	id := e.create(&a, "ABC123")
	before := e.do(&a, http.MethodGet, "/v1/vehicles/"+id, nil).data()
	e.clock.Advance(time.Hour)
	r := e.do(&a, http.MethodPatch, "/v1/vehicles/"+id, map[string]any{"plate": "xy-99 z", "color": "Azul"})
	if r.Code != 200 {
		t.Fatalf("%d %s", r.Code, r.Body)
	}
	d := r.data()
	if d["plate"] != "xy-99 z" || d["plate_normalized"] != "XY99Z" || d["color"] != "Azul" || d["make"] != "Toyota" {
		t.Errorf("data = %#v", d)
	}
	if d["created_at"] != before["created_at"] || d["updated_at"] == before["updated_at"] {
		t.Errorf("timestamps: before %v after %v", before, d)
	}
}

func TestUpdate_PlateTaken(t *testing.T) {
	e := newEnv(t)
	a, b := e.newUser("a", authz.RoleDriver), e.newUser("b", authz.RoleDriver)
	e.create(&a, "TAKEN1")
	id := e.create(&b, "MINE11")
	r := e.do(&b, http.MethodPatch, "/v1/vehicles/"+id, map[string]any{"plate": "taken-1"})
	if r.Code != http.StatusConflict || r.errCode() != "vehicle.plate_taken" {
		t.Fatalf("%d %s", r.Code, r.Body)
	}
	if got := e.do(&b, http.MethodGet, "/v1/vehicles/"+id, nil).data()["plate_normalized"]; got != "MINE11" {
		t.Errorf("plate changed to %v", got)
	}
}

func TestUpdate_NullAndBlankAreValidationErrors(t *testing.T) {
	e := newEnv(t)
	a := e.newUser("a", authz.RoleDriver)
	id := e.create(&a, "ABC123")
	for _, body := range []string{
		`{"make":null}`, `{"make":""}`, `{"model":"   "}`, `{"color":" "}`, `{"plate":"  "}`,
		`{"plate":"A#1"}`, `{"color":"` + strings.Repeat("x", 31) + `"}`,
	} {
		r := e.do(&a, http.MethodPatch, "/v1/vehicles/"+id, body)
		if r.Code != http.StatusUnprocessableEntity || r.errCode() != "validation.failed" {
			t.Errorf("%s: %d %s", body, r.Code, r.Body)
		}
	}
	// An empty patch is a no-op, not an error.
	if r := e.do(&a, http.MethodPatch, "/v1/vehicles/"+id, `{}`); r.Code != 200 {
		t.Errorf("empty patch: %d %s", r.Code, r.Body)
	}
}

func TestCreate_BlankFieldsAre422(t *testing.T) {
	e := newEnv(t)
	a := e.newUser("a", authz.RoleDriver)
	for _, mod := range []map[string]any{
		{"plate": "   "}, {"plate": ""}, {"make": " "}, {"model": ""}, {"color": "\t"}, {"plate": "A"}, {"plate": "ABCDEFGHIJK"},
	} {
		b := vehicleBody("ABC123")
		for k, v := range mod {
			b[k] = v
		}
		r := e.do(&a, http.MethodPost, "/v1/vehicles", b)
		if r.Code != http.StatusUnprocessableEntity || r.errCode() != "validation.failed" {
			t.Errorf("%v: %d %s", mod, r.Code, r.Body)
		}
	}
	for _, body := range []string{`null`, `[]`, `{}garbage`} {
		r := e.do(&a, http.MethodPost, "/v1/vehicles", body)
		if r.Code != http.StatusBadRequest || r.errCode() != "http.invalid_json" {
			t.Errorf("%q: %d %s", body, r.Code, r.Body)
		}
	}
}

func TestDelete_InUseFromChecker(t *testing.T) {
	e := newEnv(t)
	a := e.newUser("a", authz.RoleDriver)
	id := e.create(&a, "ABC123")
	e.checker.set(true)
	r := e.do(&a, http.MethodDelete, "/v1/vehicles/"+id, nil)
	if r.Code != http.StatusConflict || r.errCode() != "vehicle.in_use" {
		t.Fatalf("%d %s", r.Code, r.Body)
	}
	if n := e.count(`select count(*) from vehicles where public_id = $1 and deleted_at is null`, id); n != 1 {
		t.Errorf("row changed, live rows = %d", n)
	}
	e.checker.set(false)
	if r := e.do(&a, http.MethodDelete, "/v1/vehicles/"+id, nil); r.Code != http.StatusNoContent {
		t.Errorf("delete after release: %d", r.Code)
	}
}

func TestAdminGets403OnVehicles(t *testing.T) {
	e := newEnv(t)
	adm := e.newUser("adm", authz.RoleAdmin)
	for _, c := range []struct{ method, path string }{
		{http.MethodGet, "/v1/vehicles"}, {http.MethodPost, "/v1/vehicles"},
		{http.MethodGet, "/v1/vehicles/" + uuid.NewString()}, {http.MethodDelete, "/v1/vehicles/" + uuid.NewString()},
	} {
		var body any
		if c.method == http.MethodPost {
			body = vehicleBody("ABC123")
		}
		r := e.do(&adm, c.method, c.path, body)
		if r.Code != http.StatusForbidden || r.errCode() != "auth.forbidden" {
			t.Errorf("%s %s: %d %s", c.method, c.path, r.Code, r.Body)
		}
	}
	if r := e.do(nil, http.MethodGet, "/v1/vehicles", nil); r.Code != http.StatusUnauthorized {
		t.Errorf("anonymous: %d", r.Code)
	}
}

func TestList_EnvelopePage(t *testing.T) {
	e := newEnv(t)
	a := e.newUser("a", authz.RoleDriver)
	e.create(&a, "ABC123")
	r := e.do(&a, http.MethodGet, "/v1/vehicles", nil)
	meta := r.json()["meta"].(map[string]any)
	if !reflect.DeepEqual(meta["page"], map[string]any{"limit": float64(5), "next_cursor": nil}) {
		t.Errorf("meta.page = %#v", meta["page"])
	}
	if len(r.list()) != 1 {
		t.Errorf("data = %s", r.Body)
	}
}
