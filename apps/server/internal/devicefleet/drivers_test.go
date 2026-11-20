package devicefleet_test

import (
	"strings"
	"testing"

	"github.com/XYZLZ/MyPark/apps/server/internal/devicefleet"
)

func TestSimDriverPassword(t *testing.T) {
	email := devicefleet.SimDriverEmail(1)
	if email != "sim-driver-01@mypark.test" {
		t.Fatalf("SimDriverEmail(1) = %q", email)
	}
	got := devicefleet.SimDriverPassword(testFleet(), email).Reveal()
	// Frozen: HMAC-SHA256(fleet, "mypark/driver-password/v1/"+email), base64url without padding.
	if want := "PeXOi4tYZtD5ckALsWUIwYC7w1b3zWk-ML0uxwd87Nc"; got != want {
		t.Errorf("password = %q, want %q", got, want)
	}
	if len(got) != 43 || strings.ContainsAny(got, "+/=") {
		t.Errorf("password %q is not 43 chars of unpadded base64url", got)
	}
	if other := devicefleet.SimDriverPassword(testFleet(), devicefleet.SimDriverEmail(2)).Reveal(); other == got {
		t.Error("two drivers derive the same password")
	}
}

func TestSimDriverNames(t *testing.T) {
	if got := devicefleet.SimDriverEmail(20); got != "sim-driver-20@mypark.test" {
		t.Errorf("email = %q", got)
	}
	if got := devicefleet.SimDriverPlate(7); got != "SIM-007" {
		t.Errorf("plate = %q", got)
	}
	if devicefleet.SimDriverCount != 20 {
		t.Errorf("SimDriverCount = %d, want 20", devicefleet.SimDriverCount)
	}
}
