package gate

import (
	"context"
	"time"

	"github.com/XYZLZ/MyPark/apps/server/internal/domain/authz"
)

// Handler handles one decoded device event. It must run inside the
// transaction carried by ctx: the caller stores the event in the same
// transaction, after Handle returns (design §7.4).
type Handler interface {
	Handle(ctx context.Context, d authz.Device, ev Event) (Result, error)
}

// Repo is the gate's data access. Every method takes the device's lot id
// (M2C-21) and runs on the transaction carried by ctx.
type Repo interface {
	// UpdateSpaceFromSensor sets the space to state at now unless it is
	// out_of_service or already in state, and reports whether it changed.
	UpdateSpaceFromSensor(ctx context.Context, lotID int64, code, state string, now time.Time) (bool, error)
	// SpaceExists reports whether the lot has a space with this code.
	SpaceExists(ctx context.Context, lotID int64, code string) (bool, error)
	// NotifySpaceChanged queues a space_changed notification, sent on commit.
	NotifySpaceChanged(ctx context.Context, lotID int64, code, state string) error
}
