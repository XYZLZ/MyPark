package vehicles_test

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/XYZLZ/MyPark/apps/server/internal/domain/apperr"
	"github.com/XYZLZ/MyPark/apps/server/internal/domain/authz"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/vehicles"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/database"
)

func TestService_AdminForbiddenDirectly(t *testing.T) {
	e := newEnv(t)
	adm := e.newUser("adm", authz.RoleAdmin)
	ctx := t.Context()
	in := vehicles.CreateInput{Plate: "ABC123", Make: "T", Model: "C", Color: "G"}

	var errs []error
	_, err := e.svc.List(ctx, adm.P)
	errs = append(errs, err)
	_, err = e.svc.Create(ctx, adm.P, in)
	errs = append(errs, err)
	_, err = e.svc.Get(ctx, adm.P, adm.PublicID)
	errs = append(errs, err)
	_, err = e.svc.Update(ctx, adm.P, adm.PublicID, vehicles.UpdateInput{})
	errs = append(errs, err)
	errs = append(errs, e.svc.Delete(ctx, adm.P, adm.PublicID))

	for i, err := range errs {
		ae, ok := errors.AsType[*apperr.Error](err)
		if !ok || ae.Kind != apperr.KindForbidden || ae.Code != "auth.forbidden" {
			t.Errorf("method %d: err = %v, want Forbidden auth.forbidden", i, err)
		}
	}
}

func TestEraseOwner_FreesPlatesAndScrubs(t *testing.T) {
	e := newEnv(t)
	a, b := e.newUser("a", authz.RoleDriver), e.newUser("b", authz.RoleDriver)
	e.create(&a, "ABC123")
	e.create(&a, "DEF456")
	keep := e.create(&b, "KEEP11")

	err := database.NewTxRunner(e.db.App).InTx(t.Context(), func(ctx context.Context) error {
		return e.svc.EraseOwner(ctx, a.ID)
	})
	if err != nil {
		t.Fatalf("EraseOwner: %v", err)
	}
	rows, err := e.db.Super.Query(t.Context(),
		`select plate_normalized, plate_display, make, model, color, deleted_at is not null
		 from vehicles where user_id = $1`, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		var norm, disp, mk, mo, co string
		var deleted bool
		if err := rows.Scan(&norm, &disp, &mk, &mo, &co, &deleted); err != nil {
			t.Fatal(err)
		}
		n++
		if len(norm) != 9 || norm[0] != 'X' || norm != disp || mk != "-" || mo != "-" || co != "-" || !deleted {
			t.Errorf("row not scrubbed: %q %q %q %q %q deleted=%v", norm, disp, mk, mo, co, deleted)
		}
		for _, c := range norm[1:] {
			if (c < '0' || c > '9') && (c < 'A' || c > 'F') {
				t.Errorf("plate %q is not X+upper hex", norm)
			}
		}
	}
	if n != 2 {
		t.Errorf("scrubbed %d rows, want 2", n)
	}
	// The plate is reusable and the other driver is untouched.
	if r := e.do(&b, http.MethodPost, "/v1/vehicles", vehicleBody("ABC123")); r.Code != http.StatusCreated {
		t.Errorf("reuse after erase: %d %s", r.Code, r.Body)
	}
	if r := e.do(&b, http.MethodGet, "/v1/vehicles/"+keep, nil); r.Code != 200 || r.data()["make"] != "Toyota" {
		t.Errorf("other driver's vehicle changed: %d %s", r.Code, r.Body)
	}
}
