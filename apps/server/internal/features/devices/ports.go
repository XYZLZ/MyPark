// Package devices holds the device registry and the device event API: the
// HMAC authenticator, the strict event decode, the kind matrix, the
// idempotent event log, the fleet seed and the retention purge (design M2 §7).
package devices

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"github.com/XYZLZ/MyPark/apps/server/internal/domain/authz"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/gate"
)

// Status values of a device.
const (
	StatusActive  = "active"
	StatusRevoked = "revoked"
)

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

// StoredEvent is the part of a stored device event the idempotency check
// reads: the hash of the body it answered and the stored (sealed) answer.
type StoredEvent struct {
	BodySHA256 []byte
	Response   json.RawMessage
}

// NewEvent is one device_events row. received_at takes its database default.
type NewEvent struct {
	LotID, DeviceID int64
	EventID         uuid.UUID
	Type            gate.EventType
	OccurredAt      time.Time
	BodySHA256      []byte
	Payload         json.RawMessage // redacted
	Response        json.RawMessage // sealed
}

// Store is the append-only device event log (design §7.4).
type Store interface {
	// FindEvent returns the stored event of the device with eventID, and
	// whether there is one.
	FindEvent(ctx context.Context, deviceID int64, eventID uuid.UUID) (StoredEvent, bool, error)
	// InsertEvent appends e, on the transaction in ctx when there is one. A
	// stored (device, event id) pair is ErrDuplicateEvent.
	InsertEvent(ctx context.Context, e NewEvent) error
}
