package vehicles_test

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/XYZLZ/MyPark/apps/server/internal/domain/apperr"
	"github.com/XYZLZ/MyPark/apps/server/internal/domain/authz"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/vehicles"
	vehiclespg "github.com/XYZLZ/MyPark/apps/server/internal/features/vehicles/postgres"
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
	gone := e.create(&a, "GONE99")
	if r := e.do(&a, http.MethodDelete, "/v1/vehicles/"+gone, nil); r.Code != http.StatusNoContent {
		t.Fatalf("delete: %d", r.Code)
	}
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
	if n != 3 {
		t.Errorf("scrubbed %d rows, want 3 (including the soft-deleted one)", n)
	}
	// The plate is reusable and the other driver is untouched.
	if r := e.do(&b, http.MethodPost, "/v1/vehicles", vehicleBody("ABC123")); r.Code != http.StatusCreated {
		t.Errorf("reuse after erase: %d %s", r.Code, r.Body)
	}
	if r := e.do(&b, http.MethodGet, "/v1/vehicles/"+keep, nil); r.Code != 200 || r.data()["make"] != "Toyota" {
		t.Errorf("other driver's vehicle changed: %d %s", r.Code, r.Body)
	}
}

func TestEraseOwner_RequiresTransaction(t *testing.T) {
	e := newEnv(t)
	a := e.newUser("a", authz.RoleDriver)
	e.create(&a, "ABC123")
	if err := e.svc.EraseOwner(t.Context(), a.ID); !errors.Is(err, database.ErrNoTx) {
		t.Fatalf("err = %v, want database.ErrNoTx", err)
	}
	if n := e.count(`select count(*) from vehicles where user_id = $1 and plate_normalized = 'ABC123'`, a.ID); n != 1 {
		t.Errorf("vehicle was scrubbed outside a transaction")
	}
}

// barrierRepo holds every UpdateOwned until two have arrived or a short
// timeout passes, so two updates that read the row before either writes are
// forced to interleave.
type barrierRepo struct {
	vehicles.Repo
	arrived atomic.Int32
	both    chan struct{}
}

func (b *barrierRepo) UpdateOwned(ctx context.Context, ownerID int64, id uuid.UUID, f vehicles.Fields, now time.Time) (vehicles.Record, error) {
	if b.arrived.Add(1) == 2 {
		close(b.both)
	}
	select {
	case <-b.both:
	case <-time.After(300 * time.Millisecond):
	}
	return b.Repo.UpdateOwned(ctx, ownerID, id, f, now)
}

// Two concurrent partial updates on different fields both persist: the
// second reads the row only after the first committed.
func TestUpdate_ConcurrentPatchesDoNotLoseFields(t *testing.T) {
	e := newEnv(t)
	a := e.newUser("a", authz.RoleDriver)
	id := uuid.MustParse(e.create(&a, "ABC123"))
	repo := &barrierRepo{Repo: vehiclespg.NewRepo(e.db.App), both: make(chan struct{})}
	svc := vehicles.NewService(database.NewTxRunner(e.db.App), repo, vehicles.NoReservations{}, defaultMax, e.clock)

	mk, color := "Honda", "Azul"
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i, in := range []vehicles.UpdateInput{{Make: &mk}, {Color: &color}} {
		wg.Go(func() { _, errs[i] = svc.Update(t.Context(), a.P, id, in) })
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("update %d: %v", i, err)
		}
	}
	got, err := svc.Get(t.Context(), a.P, id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Make != mk || got.Color != color {
		t.Errorf("make=%q color=%q, want %q and %q (lost update)", got.Make, got.Color, mk, color)
	}
}
