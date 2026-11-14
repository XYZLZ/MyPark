// Package devices holds the device registry: the fleet seed, the retention
// purge and the lookup the device API authenticates against (design M2 5.2).
package devices

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/XYZLZ/MyPark/apps/server/internal/domain/authz"
	"github.com/XYZLZ/MyPark/apps/server/internal/domain/secret"
)

// Status values of a device.
const (
	StatusActive  = "active"
	StatusRevoked = "revoked"
)

// Device is a registered device with its decrypted HMAC secret. Secret is a
// secret.Bytes so the struct cannot leak it through a log line or a format verb.
type Device struct {
	ID       int64
	PublicID uuid.UUID
	LotID    int64
	Kind     authz.DeviceKind
	Name     string
	Status   string
	Secret   secret.Bytes
}

// Active reports whether the device may authenticate.
func (d Device) Active() bool { return d.Status == StatusActive }

// Repository is the port the device API uses to authenticate devices.
type Repository interface {
	// GetByPublicID returns the device and its decrypted secret, or
	// ErrNotFound. A revoked device is returned with Status revoked.
	GetByPublicID(ctx context.Context, publicID uuid.UUID) (Device, error)
	// TouchLastSeen records activity at now, at most once per 30 s per device.
	TouchLastSeen(ctx context.Context, id int64, now time.Time) error
}

// MaxEventBytes caps a device request body (design §7.1).
const MaxEventBytes = 16 << 10

// Record is a device row with its secret still sealed. The authenticator opens
// the secret only for an active device (design §7.3 step 4).
type Record struct {
	ID, LotID        int64
	PublicID         uuid.UUID
	Kind             authz.DeviceKind
	Revoked          bool
	SecretCiphertext []byte
	SecretKeyID      string
	// LastSeenAt is zero when the device was never seen.
	LastSeenAt time.Time
}

// Registry is the port the device HMAC middleware authenticates against.
type Registry interface {
	// DeviceByPublicID returns the sealed device row, or ErrDeviceNotFound.
	DeviceByPublicID(ctx context.Context, id uuid.UUID) (Record, error)
	// TouchLastSeen records activity at now, at most once per 30 s per device.
	TouchLastSeen(ctx context.Context, id int64, now time.Time) error
}
