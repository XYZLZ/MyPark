package devicefleet_test

import (
	"encoding/hex"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/XYZLZ/MyPark/apps/server/internal/devicefleet"
	"github.com/XYZLZ/MyPark/apps/server/internal/domain/authz"
	"github.com/XYZLZ/MyPark/apps/server/internal/domain/secret"
)

// testFleet is the bytes 0x20..0x3f.
func testFleet() secret.Bytes {
	b := make([]byte, 32)
	for i := range b {
		b[i] = byte(0x20 + i)
	}
	return secret.Bytes(b)
}

func TestDevices_Manifest(t *testing.T) {
	want := []struct {
		name string
		kind authz.DeviceKind
	}{
		{"podium-entry-1", authz.DevicePodium},
		{"dispenser-entry-1", authz.DeviceDispenser},
		{"barrier-entry-1", authz.DeviceEntryBarrier},
		{"barrier-exit-1", authz.DeviceExitBarrier},
		{"paystation-1", authz.DevicePayStation},
		{"sensors-1", authz.DeviceSensorGateway},
	}
	got := devicefleet.Devices()
	if len(got) != len(want) {
		t.Fatalf("devices = %d, want %d", len(got), len(want))
	}
	for i, w := range want {
		id := uuid.MustParse(fmt.Sprintf("019a6d00-0000-7000-8000-00000000000%d", i+1))
		if got[i].Name != w.name || got[i].Kind != w.kind || got[i].PublicID != id {
			t.Errorf("device %d = %+v, want %s/%s/%s", i, got[i], w.name, w.kind, id)
		}
	}
	if d := devicefleet.ByKind(authz.DeviceDispenser); d.Name != "dispenser-entry-1" {
		t.Errorf("ByKind(dispenser) = %+v", d)
	}
}

func TestDevices_ReturnsACopy(t *testing.T) {
	devicefleet.Devices()[0].Name = "mutated"
	if devicefleet.Devices()[0].Name == "mutated" {
		t.Error("Devices exposes the manifest backing array")
	}
}

func TestDeviceSecret_GoldenVector(t *testing.T) {
	id := uuid.MustParse("019a6d00-0000-7000-8000-000000000002")
	got := hex.EncodeToString(devicefleet.DeviceSecret(testFleet(), id).Reveal())
	if want := "a1d15e3256a68cdee96b287f51a7e82632439e0770627a01a86cb1672461f005"; got != want {
		t.Errorf("DeviceSecret = %s, want %s", got, want)
	}
}

func TestDeviceSecret_DependsOnFleetAndID(t *testing.T) {
	a := devicefleet.DeviceSecret(testFleet(), devicefleet.Devices()[0].PublicID)
	b := devicefleet.DeviceSecret(testFleet(), devicefleet.Devices()[1].PublicID)
	c := devicefleet.DeviceSecret(secret.Bytes("another fleet secret"), devicefleet.Devices()[0].PublicID)
	if string(a) == string(b) || string(a) == string(c) {
		t.Error("derived secrets collide")
	}
	if got := fmt.Sprintf("%v %s %x", a, a, a); strings.Contains(got, hex.EncodeToString(a.Reveal())) {
		t.Errorf("derived secret leaks through fmt: %s", got)
	}
}

func TestCardUID_GoldenVector(t *testing.T) {
	if got, want := devicefleet.CardUID(testFleet(), "EMP-007"), "DFFE6EADBA0A4C"; got != want {
		t.Errorf("CardUID = %s, want %s", got, want)
	}
}

func TestCards_Manifest(t *testing.T) {
	cards := devicefleet.Cards()
	if len(cards) != 31 {
		t.Fatalf("cards = %d, want 31", len(cards))
	}
	for i, c := range cards[:30] {
		code := fmt.Sprintf("EMP-%03d", i+1)
		holder := fmt.Sprintf("Empleado %02d", i+1)
		if c.Code != code || c.Holder != holder || c.Revoked {
			t.Errorf("card %d = %+v, want %s/%s active", i, c, code, holder)
		}
	}
	if last := cards[30]; last.Code != "EMP-031" || last.Holder != "Empleado 31" || !last.Revoked {
		t.Errorf("last card = %+v", last)
	}
	if cards[6].Code != "EMP-007" || cards[6].Holder != "Empleado 07" {
		t.Errorf("card 7 = %+v", cards[6])
	}
}
