package postgres_test

import (
	"bytes"
	"errors"
	"fmt"
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

func TestRepo_GetByPublicIDOpensSecretAndTouchThrottles(t *testing.T) {
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
	repo := devpg.NewRepo(db.App, s)
	pod := devicefleet.ByKind(authz.DevicePodium)

	got, err := repo.GetByPublicID(t.Context(), pod.PublicID)
	if err != nil {
		t.Fatalf("GetByPublicID: %v", err)
	}
	if got.Kind != authz.DevicePodium || got.LotID != lotID || !got.Active() {
		t.Errorf("device = %+v", got)
	}
	if !bytes.Equal(got.Secret.Reveal(), devicefleet.DeviceSecret(fleet, pod.PublicID).Reveal()) {
		t.Error("opened secret differs from the derived one")
	}
	if out := fmt.Sprintf("%v %+v %#v", got, got, got); bytes.Contains([]byte(out), got.Secret.Reveal()) {
		t.Error("formatting a Device leaks the secret")
	}
	if _, err := repo.GetByPublicID(t.Context(), uuid.New()); !errors.Is(err, devices.ErrNotFound) {
		t.Errorf("unknown id error = %v, want ErrNotFound", err)
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

func TestRepo_UnopenableSecretIsAnError(t *testing.T) {
	db := testsupport.NewTestDB(t)
	lotID := testsupport.SeedPilotLot(t, db)
	k := func(id string, b byte) secretbox.Key {
		return secretbox.Key{ID: id, Secret: [32]byte(bytes.Repeat([]byte{b}, 32))}
	}
	sealed, _ := secretbox.NewSealer(k("k1", 1))
	fleet := secret.Bytes(bytes.Repeat([]byte{7}, 32))
	if _, err := devices.SeedFleet(t.Context(), dbgen.New(db.App), sealed, lotID, fleet); err != nil {
		t.Fatal(err)
	}
	other, _ := secretbox.NewSealer(k("k2", 2))
	_, err := devpg.NewRepo(db.App, other).GetByPublicID(t.Context(), devicefleet.Devices()[0].PublicID)
	if err == nil {
		t.Fatal("GetByPublicID with the wrong keyring succeeded")
	}
}
