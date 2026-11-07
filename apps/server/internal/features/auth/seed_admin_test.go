package auth_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/XYZLZ/MyPark/apps/server/internal/domain/secret"
	auditpg "github.com/XYZLZ/MyPark/apps/server/internal/features/audit/postgres"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/auth"
	authpg "github.com/XYZLZ/MyPark/apps/server/internal/features/auth/postgres"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/database"
)

const seedPassword = "Seed-admin-pass-2026"

func (e *env) seedAdmin(in auth.SeedAdmin) (created bool, err error) {
	e.t.Helper()
	d := auth.SeedDeps{Hasher: e.hasher, Audit: auditpg.NewRecorder(), Clock: e.clock}
	err = database.NewTxRunner(e.db.App).InTx(e.t.Context(), func(ctx context.Context) error {
		var err error
		created, err = auth.SeedFirstAdmin(ctx, d, in)
		return err
	})
	return created, err
}

func seedInput() auth.SeedAdmin {
	return auth.SeedAdmin{Email: "admin@mypark.local", FirstName: "MyPark", LastName: "Admin", Password: secret.String(seedPassword)}
}

func TestSeedFirstAdmin_CreatesOnce(t *testing.T) {
	e := newEnv(t)
	created, err := e.seedAdmin(seedInput())
	if err != nil || !created {
		t.Fatalf("first run = %v, %v", created, err)
	}
	hash := e.text(`select password_hash from users where email='admin@mypark.local'`)
	created, err = e.seedAdmin(seedInput())
	if err != nil || created {
		t.Fatalf("second run = %v, %v, want skipped", created, err)
	}
	if e.count(`select count(*) from users where role='admin'`) != 1 {
		t.Error("want exactly one admin")
	}
	if got := e.text(`select password_hash from users where email='admin@mypark.local'`); got != hash {
		t.Error("second run rewrote the hash")
	}
	row := e.text(`select status||'/'||(email_verified_at is not null)::text||'/'||coalesce(terms_version,'<null>')||'/'||
		coalesce(privacy_version,'<null>')||'/'||first_name||'/'||last_name from users where email='admin@mypark.local'`)
	if row != "active/true/<null>/<null>/MyPark/Admin" {
		t.Errorf("row = %q", row)
	}
	if e.count(`select count(*) from audit_log where action='admin.seeded' and actor_kind='system'`) != 1 {
		t.Error("want one admin.seeded audit by the system")
	}
	got := e.text(`select coalesce(after::text,'') || coalesce(before::text,'') from audit_log where action='admin.seeded'`)
	if strings.Contains(got, "admin@mypark.local") || strings.Contains(got, seedPassword) {
		t.Errorf("audit holds PII: %s", got)
	}
	if rec := e.post(pathLogin, loginBody("admin@mypark.local", seedPassword)); rec.Code != http.StatusOK {
		t.Errorf("seeded admin cannot log in: %d %s", rec.Code, rec.Body)
	}
}

func TestSeedFirstAdmin_SkipsWhenAnyAdminExists(t *testing.T) {
	for name, status := range map[string]string{"active": "active", "pending invitee": "pending_verification", "suspended": "suspended"} {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t)
			e.insertUser("someone@example.com", "admin", status, false)
			created, err := e.seedAdmin(seedInput())
			if err != nil || created {
				t.Fatalf("seed = %v, %v, want skipped", created, err)
			}
			if e.count(`select count(*) from users`) != 1 || e.count(`select count(*) from audit_log where action='admin.seeded'`) != 0 {
				t.Error("seed wrote something")
			}
		})
	}
}

func TestSeedFirstAdmin_DeletedAdminDoesNotCount(t *testing.T) {
	e := newEnv(t)
	e.insertUser("gone@example.com", "admin", "active", false)
	e.exec(`update users set status='deleted', deleted_at=now() where email='gone@example.com'`)
	if created, err := e.seedAdmin(seedInput()); err != nil || !created {
		t.Fatalf("seed = %v, %v", created, err)
	}
}

func TestSeedFirstAdmin_RejectsWeakPasswordWithoutEchoing(t *testing.T) {
	e := newEnv(t)
	for _, pw := range []string{"short", "1234567890", "qwerty123"} {
		in := seedInput()
		in.Password = secret.String(pw)
		created, err := e.seedAdmin(in)
		if err == nil || created {
			t.Errorf("password %q: created=%v err=%v, want an error", pw, created, err)
			continue
		}
		if len(pw) > 6 && strings.Contains(err.Error(), pw) {
			t.Errorf("error echoes the password: %v", err)
		}
	}
	if e.count(`select count(*) from users`) != 0 {
		t.Error("a weak password created a user")
	}
}

func TestSeedFirstAdmin_EmailHeldByDriverFails(t *testing.T) {
	e := newEnv(t)
	e.insertUser("admin@mypark.local", "driver", "active", true)
	if created, err := e.seedAdmin(seedInput()); err == nil || created {
		t.Fatalf("seed = %v, %v, want an error", created, err)
	}
	if e.count(`select count(*) from users where role='admin'`) != 0 {
		t.Error("an admin was created")
	}
}

// TestActivateUser_IgnoresAdminRows is defense in depth for OC-38: even if a
// public flow reached ActivateUser with an invitee, the query refuses it.
func TestActivateUser_IgnoresAdminRows(t *testing.T) {
	e := newEnv(t)
	inv := e.insertUser("nora@example.com", "admin", "pending_verification", false)
	if err := authpg.NewRepo(e.db.App).ActivateUser(t.Context(), inv.ID, e.clock.Now()); err != nil {
		t.Fatal(err)
	}
	if got := e.text(`select status from users where email='nora@example.com'`); got != "pending_verification" {
		t.Errorf("status = %q, want pending_verification", got)
	}
}
