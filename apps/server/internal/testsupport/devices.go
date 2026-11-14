package testsupport

import (
	"bytes"
	"crypto/rand"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/XYZLZ/MyPark/apps/server/internal/devicesig"
	"github.com/XYZLZ/MyPark/apps/server/internal/domain/authz"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/database/dbgen"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/secretbox"
)

// TestSealer returns a secretbox keyring with one fixed test key.
func TestSealer(t testing.TB) *secretbox.Sealer {
	t.Helper()
	s, err := secretbox.NewSealer(secretbox.Key{ID: "test", Secret: [32]byte(bytes.Repeat([]byte{0x5a}, 32))})
	if err != nil {
		t.Fatalf("testsupport: sealer: %v", err)
	}
	return s
}

// TestDevice is a device inserted by NewTestDevice, with its plain secret so
// tests can sign requests.
type TestDevice struct {
	ID, LotID int64
	PublicID  uuid.UUID
	Kind      authz.DeviceKind
	Secret    []byte
}

// NewTestDevice inserts an active device of kind in lotID with a random
// 32-byte secret sealed by s.
func NewTestDevice(t testing.TB, db *TestDB, s *secretbox.Sealer, lotID int64, kind authz.DeviceKind) TestDevice {
	t.Helper()
	secret := make([]byte, 32)
	_, _ = rand.Read(secret)
	sealed, keyID, err := s.Seal(secret)
	if err != nil {
		t.Fatalf("testsupport: seal device secret: %v", err)
	}
	id, err := uuid.NewV7()
	if err != nil {
		t.Fatal(err)
	}
	q := dbgen.New(db.App)
	if _, err := q.InsertFleetDevice(t.Context(), dbgen.InsertFleetDeviceParams{
		PublicID: id, LotID: lotID, Kind: string(kind), Name: "test " + string(kind) + " " + id.String()[24:],
		SecretCiphertext: sealed, SecretKeyID: keyID,
	}); err != nil {
		t.Fatalf("testsupport: insert device: %v", err)
	}
	row, err := q.GetDeviceByPublicID(t.Context(), id)
	if err != nil {
		t.Fatalf("testsupport: read device: %v", err)
	}
	return TestDevice{ID: row.ID, LotID: lotID, PublicID: id, Kind: kind, Secret: secret}
}

// Request returns a POST to uri (path plus raw query) carrying body, signed by
// the device with X-Timestamp at.
func (d TestDevice) Request(t testing.TB, uri string, body []byte, at time.Time) *http.Request {
	t.Helper()
	ts := at.UTC().Format(devicesig.TimestampLayout)
	r := httptest.NewRequest(http.MethodPost, uri, bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Device-Id", d.PublicID.String())
	r.Header.Set("X-Timestamp", ts)
	r.Header.Set("X-Signature", devicesig.Sign(d.Secret, devicesig.Canonical(http.MethodPost, uri, ts, body)))
	return r
}
