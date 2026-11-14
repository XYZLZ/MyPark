package postgres_test

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/XYZLZ/MyPark/apps/server/internal/devicefleet"
	"github.com/XYZLZ/MyPark/apps/server/internal/domain/authz"
	"github.com/XYZLZ/MyPark/apps/server/internal/domain/secret"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/devices"
	devpg "github.com/XYZLZ/MyPark/apps/server/internal/features/devices/postgres"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/database/dbgen"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/secretbox"
	"github.com/XYZLZ/MyPark/apps/server/internal/testsupport"
)

// A seeded fleet device comes back sealed, and its sealed secret opens to the
// secret derived from the fleet secret. The touch is throttled in SQL too.
func TestRepo_SeededDeviceOpensToDerivedSecretAndTouchThrottles(t *testing.T) {
	db := testsupport.NewTestDB(t)
	lotID := testsupport.SeedPilotLot(t, db)
	s, err := secretbox.NewSealer(secretbox.Key{ID: "k1", Secret: [32]byte(bytes.Repeat([]byte{1}, 32))})
	if err != nil {
		t.Fatal(err)
	}
	fleet := secret.Bytes(bytes.Repeat([]byte{7}, 32))
	if _, err := devices.SeedFleet(t.Context(), dbgen.New(db.App), s, lotID, fleet); err != nil {
		t.Fatal(err)
	}
	repo := devpg.NewRepo(db.App)
	pod := devicefleet.ByKind(authz.DevicePodium)

	got, err := repo.DeviceByPublicID(t.Context(), pod.PublicID)
	if err != nil {
		t.Fatalf("DeviceByPublicID: %v", err)
	}
	if got.Kind != authz.DevicePodium || got.LotID != lotID || got.Revoked {
		t.Errorf("record = %+v", got)
	}
	derived := devicefleet.DeviceSecret(fleet, pod.PublicID).Reveal()
	if bytes.Contains(got.SecretCiphertext, derived) {
		t.Error("record carries the plain secret")
	}
	if plain, err := s.Open(got.SecretCiphertext, got.SecretKeyID); err != nil || !bytes.Equal(plain, derived) {
		t.Errorf("sealed secret does not open to the derived one: %v", err)
	}

	t0 := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	seen := func() time.Time {
		var ts time.Time
		if err := db.App.QueryRow(t.Context(), "select last_seen_at from devices where id = $1", got.ID).Scan(&ts); err != nil {
			t.Fatal(err)
		}
		return ts
	}
	for _, step := range []struct {
		at   time.Duration
		want time.Duration
	}{{0, 0}, {10 * time.Second, 0}, {31 * time.Second, 31 * time.Second}} {
		if err := repo.TouchLastSeen(t.Context(), got.ID, t0.Add(step.at)); err != nil {
			t.Fatal(err)
		}
		if ts := seen(); !ts.Equal(t0.Add(step.want)) {
			t.Errorf("after touch at +%v last_seen_at = %v, want +%v", step.at, ts, step.want)
		}
	}
}

func TestRepo_DeviceByPublicIDReturnsSealedRecord(t *testing.T) {
	db := testsupport.NewTestDB(t)
	lotID := testsupport.SeedPilotLot(t, db)
	s := testsupport.TestSealer(t)
	dev := testsupport.NewTestDevice(t, db, s, lotID, authz.DeviceExitBarrier)
	repo := devpg.NewRepo(db.App)

	var reg devices.Registry = repo
	rec, err := reg.DeviceByPublicID(t.Context(), dev.PublicID)
	if err != nil {
		t.Fatalf("DeviceByPublicID: %v", err)
	}
	if rec.ID != dev.ID || rec.LotID != lotID || rec.Kind != authz.DeviceExitBarrier || rec.Revoked || !rec.LastSeenAt.IsZero() {
		t.Errorf("record = %+v", rec)
	}
	if bytes.Contains(rec.SecretCiphertext, dev.Secret) {
		t.Error("record carries the plain secret")
	}
	plain, err := s.Open(rec.SecretCiphertext, rec.SecretKeyID)
	if err != nil || !bytes.Equal(plain, dev.Secret) {
		t.Errorf("sealed secret does not open to the device secret: %v", err)
	}

	at := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	if err := reg.TouchLastSeen(t.Context(), dev.ID, at); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Super.Exec(t.Context(),
		"update devices set status = 'revoked', revoked_at = now() where id = $1", dev.ID); err != nil {
		t.Fatal(err)
	}
	if rec, err = reg.DeviceByPublicID(t.Context(), dev.PublicID); err != nil || !rec.Revoked || !rec.LastSeenAt.Equal(at) {
		t.Errorf("after revoke: %+v, %v", rec, err)
	}
	if _, err := reg.DeviceByPublicID(t.Context(), uuid.New()); !errors.Is(err, devices.ErrDeviceNotFound) {
		t.Errorf("unknown id error = %v, want ErrDeviceNotFound", err)
	}
}
