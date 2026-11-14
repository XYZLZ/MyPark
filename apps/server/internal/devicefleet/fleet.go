// Package devicefleet is the committed simulator fleet manifest (M2C-25): the
// devices with fixed public ids, the employee cards, and the derivation of
// every device secret and card UID from one env-provided fleet secret. No
// secret is stored here; the demo fleet is derived, never persisted in plaintext.
package devicefleet

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"

	"github.com/XYZLZ/MyPark/apps/server/internal/domain/authz"
	"github.com/XYZLZ/MyPark/apps/server/internal/domain/secret"
)

// Device is one fleet device.
type Device struct {
	Name     string
	Kind     authz.DeviceKind
	PublicID uuid.UUID
}

// Card is one employee card. Code is the printed label; the UID is derived.
type Card struct {
	Code    string
	Holder  string
	Revoked bool
}

const (
	deviceSecretLabel = "mypark/device-secret/v1/"
	cardUIDLabel      = "mypark/card-uid/v1/"
	// cardUIDBytes is the NFC-style 7-byte UID length (14 hex characters).
	cardUIDBytes = 7
)

func fleetID(n byte) uuid.UUID {
	return uuid.UUID{0x01, 0x9a, 0x6d, 0x00, 0, 0, 0x70, 0, 0x80, 0, 0, 0, 0, 0, 0, n}
}

var devices = []Device{
	{"podium-entry-1", authz.DevicePodium, fleetID(1)},
	{"dispenser-entry-1", authz.DeviceDispenser, fleetID(2)},
	{"barrier-entry-1", authz.DeviceEntryBarrier, fleetID(3)},
	{"barrier-exit-1", authz.DeviceExitBarrier, fleetID(4)},
	{"paystation-1", authz.DevicePayStation, fleetID(5)},
	{"sensors-1", authz.DeviceSensorGateway, fleetID(6)},
}

// Devices returns the six fleet devices (design §6.2). The slice is a copy.
func Devices() []Device { return slices.Clone(devices) }

// ByKind returns the fleet device of kind k, or the zero Device when the fleet
// has none. Each kind appears exactly once.
func ByKind(k authz.DeviceKind) Device {
	for _, d := range devices {
		if d.Kind == k {
			return d
		}
	}
	return Device{}
}

// Cards returns EMP-001..EMP-030 (active) and EMP-031 (revoked, to demo the
// denial). Holder names are fictitious.
func Cards() []Card {
	cards := make([]Card, 0, 31)
	for i := 1; i <= 31; i++ {
		cards = append(cards, Card{
			Code:    fmt.Sprintf("EMP-%03d", i),
			Holder:  fmt.Sprintf("Empleado %02d", i),
			Revoked: i == 31,
		})
	}
	return cards
}

// DeviceSecret derives a device's HMAC secret:
// HMAC-SHA256(fleetSecret, "mypark/device-secret/v1/" + id).
func DeviceSecret(fleetSecret secret.Bytes, id uuid.UUID) secret.Bytes {
	return secret.Bytes(derive(fleetSecret, deviceSecretLabel+id.String()))
}

// CardUID derives an NFC-style 14-character (7-byte) uppercase hex UID:
// the first 7 bytes of HMAC-SHA256(fleetSecret, "mypark/card-uid/v1/" + code).
func CardUID(fleetSecret secret.Bytes, code string) string {
	return strings.ToUpper(hex.EncodeToString(derive(fleetSecret, cardUIDLabel+code)[:cardUIDBytes]))
}

// derive panics on an empty fleet secret: that is a programming error (the
// seed and the simulator validate the variable at load time), and an HMAC
// keyed with nothing would silently yield guessable device secrets.
func derive(fleetSecret secret.Bytes, msg string) []byte {
	if len(fleetSecret) == 0 {
		panic("devicefleet: empty fleet secret")
	}
	mac := hmac.New(sha256.New, fleetSecret.Reveal())
	mac.Write([]byte(msg))
	return mac.Sum(nil)
}
