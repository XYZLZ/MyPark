package gate_test

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/XYZLZ/MyPark/apps/server/internal/devicefleet"
	"github.com/XYZLZ/MyPark/apps/server/internal/domain/secret"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/gate"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/database/dbgen"
	"github.com/XYZLZ/MyPark/apps/server/internal/testsupport"
)

var (
	testFleet  = secret.Bytes(bytes.Repeat([]byte{7}, 32))
	testPepper = bytes.Repeat([]byte{9}, 32)
)

func TestHashCardUID(t *testing.T) {
	mac := hmac.New(sha256.New, testPepper)
	mac.Write([]byte("04A1B2C3D4E5F6"))
	want := mac.Sum(nil)
	for _, in := range []string{"04A1B2C3D4E5F6", "04:a1:b2:c3:d4:e5:f6", "04-A1-B2-C3-D4-E5-F6", "04a1b2c3d4e5f6"} {
		if got := gate.HashCardUID(testPepper, in); !bytes.Equal(got, want) {
			t.Errorf("HashCardUID(%q) = %x, want %x", in, got, want)
		}
	}
	if bytes.Equal(gate.HashCardUID([]byte("another pepper, 32 bytes long!!!"), "04A1B2C3D4E5F6"), want) {
		t.Error("the pepper does not change the hash")
	}
}

func TestSeedCards_IdempotentAndRevoked(t *testing.T) {
	db := testsupport.NewTestDB(t)
	lotID := testsupport.SeedPilotLot(t, db)
	q := dbgen.New(db.App)

	n, err := gate.SeedCards(t.Context(), q, lotID, testFleet, testPepper)
	if err != nil || n != 31 {
		t.Fatalf("SeedCards = %d, %v, want 31, nil", n, err)
	}
	var total, revoked int
	if err := db.App.QueryRow(t.Context(), `select count(*), count(*) filter (where status = 'revoked') from employee_cards`).Scan(&total, &revoked); err != nil || total != 31 || revoked != 1 {
		t.Fatalf("cards = %d, revoked = %d, %v, want 31 and 1", total, revoked, err)
	}
	var status string
	var revokedAt time.Time
	if err := db.App.QueryRow(t.Context(), `select status, revoked_at from employee_cards where employee_code = 'EMP-031'`).Scan(&status, &revokedAt); err != nil {
		t.Fatal(err)
	}
	if want := time.Date(2026, 11, 1, 0, 0, 0, 0, time.FixedZone("", -4*3600)); status != "revoked" || !revokedAt.Equal(want) {
		t.Errorf("EMP-031 = %q, %v, want revoked at %v", status, revokedAt, want)
	}
	if n, err := gate.SeedCards(t.Context(), q, lotID, testFleet, testPepper); err != nil || n != 0 {
		t.Errorf("second SeedCards = %d, %v, want 0, nil", n, err)
	}
	var stored []byte
	if err := db.App.QueryRow(t.Context(), `select card_uid_hash from employee_cards where employee_code = 'EMP-007'`).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if want := gate.HashCardUID(testPepper, devicefleet.CardUID(testFleet, "EMP-007")); !bytes.Equal(stored, want) {
		t.Errorf("EMP-007 hash = %x, want %x", stored, want)
	}
	// Only hashes are stored: no column holds a card UID in the clear.
	var leaked int
	for _, c := range devicefleet.Cards() {
		uid := devicefleet.CardUID(testFleet, c.Code)
		var n int
		if err := db.App.QueryRow(t.Context(), `select count(*) from employee_cards where employee_cards::text ilike '%' || $1 || '%'`, uid).Scan(&n); err != nil {
			t.Fatal(err)
		}
		leaked += n
	}
	if leaked != 0 {
		t.Errorf("%d rows contain a plaintext card UID", leaked)
	}
}

func TestSeedCards_RejectsEmptySecrets(t *testing.T) {
	q := dbgen.New(nil)
	if _, err := gate.SeedCards(t.Context(), q, 1, nil, testPepper); err == nil {
		t.Error("empty fleet secret accepted")
	}
	if _, err := gate.SeedCards(t.Context(), q, 1, testFleet, nil); err == nil {
		t.Error("empty pepper accepted")
	}
}

// TestSeedCards_ErrorsNeverCarryUIDs covers the failure path: a seed that
// cannot write must not echo a card UID, its hash or the pepper.
func TestSeedCards_ErrorsNeverCarryUIDs(t *testing.T) {
	db := testsupport.NewTestDB(t)
	lotID := testsupport.SeedPilotLot(t, db)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := gate.SeedCards(ctx, dbgen.New(db.App), lotID+999, testFleet, testPepper)
	if err == nil {
		t.Fatal("SeedCards on a cancelled context succeeded")
	}
	msg := fmt.Sprintf("%v|%+v|%#v", err, err, err)
	for _, c := range devicefleet.Cards() {
		uid := devicefleet.CardUID(testFleet, c.Code)
		hash := hex.EncodeToString(gate.HashCardUID(testPepper, uid))
		for _, s := range []string{uid, hash} {
			if strings.Contains(strings.ToLower(msg), strings.ToLower(s)) {
				t.Fatalf("error carries a card UID or hash: %s", s)
			}
		}
	}
	if strings.Contains(msg, hex.EncodeToString(testPepper)) {
		t.Error("error carries the pepper")
	}
}
