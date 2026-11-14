package gate

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

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

	// LockLotOccupancy locks the lot's occupancy row FOR UPDATE, the first
	// lock of every entry decision (M2C-29), and returns what it guards.
	LockLotOccupancy(ctx context.Context, lotID int64) (LotOccupancy, error)
	// CountOutOfService counts the lot's spaces out of service.
	CountOutOfService(ctx context.Context, lotID int64) (int, error)
	// CountFreshPending counts the lot's entry_pending stays accepted in the
	// 60 s before now.
	CountFreshPending(ctx context.Context, lotID int64, now time.Time) (int, error)
	// ActiveCardByHash returns the lot's active employee card with this UID
	// hash, or ErrNotFound.
	ActiveCardByHash(ctx context.Context, lotID int64, hash []byte) (Card, error)
	// CardHasOpenStay reports whether the card has an entry_pending, active
	// or paid stay in the lot.
	CardHasOpenStay(ctx context.Context, lotID, cardID int64) (bool, error)
	// InsertStay inserts an entry_pending stay accepted at now.
	InsertStay(ctx context.Context, s NewStay) (uuid.UUID, error)
}

// ErrNotFound means the lot has no matching row.
var ErrNotFound = errors.New("gate: not found")

// LotOccupancy is the locked occupancy row and its lot.
type LotOccupancy struct {
	Occupied, Capacity int
	Closed             bool
}

// Card is an active employee card.
type Card struct {
	ID       int64
	PublicID uuid.UUID
}

// NewStay is an accepted entry. TicketCodeHash is set for a ticket,
// EmployeeCardID for an employee card.
type NewStay struct {
	LotID           int64
	TicketCodeHash  []byte
	EmployeeCardID  int64
	TariffVersionID int64
	AcceptedAt      time.Time
}
