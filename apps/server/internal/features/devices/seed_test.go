package devices_test

import (
	"bytes"
	"crypto/subtle"
	"testing"

	"github.com/google/uuid"

	"github.com/XYZLZ/MyPark/apps/server/internal/devicefleet"
	"github.com/XYZLZ/MyPark/apps/server/internal/domain/secret"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/devices"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/database/dbgen"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/secretbox"
	"github.com/XYZLZ/MyPark/apps/server/internal/testsupport"
)

func key(id string, fill byte) secretbox.Key {
	return secretbox.Key{ID: id, Secret: [32]byte(bytes.Repeat([]byte{fill}, 32))}
}

func sealer(t *testing.T, current secretbox.Key, old ...secretbox.Key) *secretbox.Sealer {
	t.Helper()
	s, err := secretbox.NewSealer(current, old...)
	if err != nil {
		t.Fatalf("NewSealer: %v", err)
	}
	return s
}

func fleetSecret(fill byte) secret.Bytes { return secret.Bytes(bytes.Repeat([]byte{fill}, 32)) }

type row struct {
	publicID   uuid.UUID
	ciphertext []byte
	keyID      string
}

func rows(t *testing.T, db *testsupport.TestDB) []row {
	t.Helper()
	res, err := db.App.Query(t.Context(), "select public_id, secret_ciphertext, secret_key_id from devices order by id")
	if err != nil {
		t.Fatalf("query devices: %v", err)
	}
	defer res.Close()
	var out []row
	for res.Next() {
		var r row
		if err := res.Scan(&r.publicID, &r.ciphertext, &r.keyID); err != nil {
			t.Fatalf("scan: %v", err)
		}
		out = append(out, r)
	}
	return out
}

func TestSeedFleet_InsertsSixDevicesSealed(t *testing.T) {
	db := testsupport.NewTestDB(t)
	lotID := testsupport.SeedPilotLot(t, db)
	s, fleet := sealer(t, key("k1", 1)), fleetSecret(7)

	res, err := devices.SeedFleet(t.Context(), dbgen.New(db.App), s, lotID, fleet)
	if err != nil || res != (devices.SeedResult{Inserted: 6}) {
		t.Fatalf("SeedFleet = %+v, %v, want 6 inserted", res, err)
	}
	got := rows(t, db)
	if len(got) != 6 {
		t.Fatalf("rows = %d, want 6", len(got))
	}
	for _, r := range got {
		if r.keyID != "k1" {
			t.Errorf("%s secret_key_id = %q, want k1", r.publicID, r.keyID)
		}
		plain, err := s.Open(r.ciphertext, r.keyID)
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		want := devicefleet.DeviceSecret(fleet, r.publicID).Reveal()
		if subtle.ConstantTimeCompare(plain, want) != 1 {
			t.Errorf("%s: stored secret is not the derived one", r.publicID)
		}
		if bytes.Contains(r.ciphertext, want) {
			t.Errorf("%s: ciphertext contains the plaintext secret", r.publicID)
		}
	}
}

func TestSeedFleet_IdempotentAndReseals(t *testing.T) {
	db := testsupport.NewTestDB(t)
	lotID := testsupport.SeedPilotLot(t, db)
	q := dbgen.New(db.App)
	k1, k2 := key("k1", 1), key("k2", 2)
	fleet := fleetSecret(7)

	if _, err := devices.SeedFleet(t.Context(), q, sealer(t, k1), lotID, fleet); err != nil {
		t.Fatal(err)
	}
	if res, err := devices.SeedFleet(t.Context(), q, sealer(t, k1), lotID, fleet); err != nil || res != (devices.SeedResult{}) {
		t.Fatalf("second run = %+v, %v, want {0 0}", res, err)
	}

	// Key rotation: k2 becomes current, k1 stays as an old key.
	s2 := sealer(t, k2, k1)
	if res, err := devices.SeedFleet(t.Context(), q, s2, lotID, fleet); err != nil || res != (devices.SeedResult{Resealed: 6}) {
		t.Fatalf("rotation run = %+v, %v, want 6 resealed", res, err)
	}
	for _, r := range rows(t, db) {
		if r.keyID != "k2" {
			t.Errorf("%s secret_key_id = %q after rotation, want k2", r.publicID, r.keyID)
		}
	}
	if res, _ := devices.SeedFleet(t.Context(), q, s2, lotID, fleet); res != (devices.SeedResult{}) {
		t.Errorf("run after rotation = %+v, want {0 0}", res)
	}

	// A new fleet secret changes every derived secret.
	other := fleetSecret(9)
	if res, err := devices.SeedFleet(t.Context(), q, s2, lotID, other); err != nil || res != (devices.SeedResult{Resealed: 6}) {
		t.Fatalf("new fleet secret run = %+v, %v, want 6 resealed", res, err)
	}
	for _, r := range rows(t, db) {
		plain, err := s2.Open(r.ciphertext, r.keyID)
		if err != nil || subtle.ConstantTimeCompare(plain, devicefleet.DeviceSecret(other, r.publicID).Reveal()) != 1 {
			t.Errorf("%s: secret not re-derived from the new fleet secret (%v)", r.publicID, err)
		}
	}
}

func TestSeedFleet_UnopenableStoredSecretIsResealed(t *testing.T) {
	db := testsupport.NewTestDB(t)
	lotID := testsupport.SeedPilotLot(t, db)
	q := dbgen.New(db.App)
	fleet := fleetSecret(7)
	if _, err := devices.SeedFleet(t.Context(), q, sealer(t, key("k1", 1)), lotID, fleet); err != nil {
		t.Fatal(err)
	}
	// k1 is gone from the keyring: the derived secret is regenerable, so heal it.
	res, err := devices.SeedFleet(t.Context(), q, sealer(t, key("k2", 2)), lotID, fleet)
	if err != nil || res.Resealed != 6 {
		t.Fatalf("SeedFleet = %+v, %v, want 6 resealed", res, err)
	}
}

func TestSeedFleet_RejectsEmptyFleetSecret(t *testing.T) {
	db := testsupport.NewTestDB(t)
	lotID := testsupport.SeedPilotLot(t, db)
	_, err := devices.SeedFleet(t.Context(), dbgen.New(db.App), sealer(t, key("k1", 1)), lotID, nil)
	if err == nil {
		t.Fatal("SeedFleet with an empty fleet secret succeeded")
	}
}
