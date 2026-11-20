package devicefleet

import (
	"encoding/base64"
	"fmt"

	"github.com/XYZLZ/MyPark/apps/server/internal/domain/secret"
)

const (
	// SimDriverCount is the number of seeded simulated drivers (M3B-44).
	SimDriverCount = 20

	driverPasswordLabel = "mypark/driver-password/v1/"
)

// SimDriverEmail is the login of simulated driver n (1..SimDriverCount).
func SimDriverEmail(n int) string { return fmt.Sprintf("sim-driver-%02d@mypark.test", n) }

// SimDriverPlate is the plate of simulated driver n's seeded vehicle.
func SimDriverPlate(n int) string { return fmt.Sprintf("SIM-%03d", n) }

// SimDriverPassword derives the password of a simulated driver:
// base64url (no padding) of HMAC-SHA256(fleetSecret, "mypark/driver-password/v1/" + email).
// The seed stores only its argon2id hash; the simulator re-derives it to log in.
func SimDriverPassword(fleetSecret secret.Bytes, email string) secret.String {
	return secret.String(base64.RawURLEncoding.EncodeToString(derive(fleetSecret, driverPasswordLabel+email)))
}
