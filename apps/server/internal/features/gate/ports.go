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

	// StayForUpdate locks the lot's stay with this public id, or returns
	// ErrNotFound. Called after LockLotOccupancy (M2C-29).
	StayForUpdate(ctx context.Context, lotID int64, publicID uuid.UUID) (Stay, error)
	// ActivateStay marks an entry_pending or voided stay active, entered at now.
	ActivateStay(ctx context.Context, lotID, stayID int64, now time.Time) error
	// BumpOccupancy adds delta to the locked lot's occupancy and returns the
	// new version.
	BumpOccupancy(ctx context.Context, lotID int64, delta int, now time.Time) (int64, error)
	// NotifyLotChanged queues a lot_changed notification, sent on commit.
	NotifyLotChanged(ctx context.Context, lotID, version int64) error

	// StayByTicketHashForUpdate locks the lot's stay with this ticket code
	// hash, or returns ErrNotFound.
	StayByTicketHashForUpdate(ctx context.Context, lotID int64, hash []byte) (Stay, error)
	// OpenCardStayForUpdate locks the card's entry_pending, active or paid
	// stay in the lot, or returns ErrNotFound.
	OpenCardStayForUpdate(ctx context.Context, lotID, cardID int64) (Stay, error)
	// PaymentTotals sums the stay's succeeded payments and returns the time
	// of the latest one (zero when there is none).
	PaymentTotals(ctx context.Context, lotID, stayID int64) (paidCents int64, lastPaidAt time.Time, err error)
	// InsertPayment inserts a succeeded payment and returns its id. The stay
	// must come from a lot-scoped lock.
	InsertPayment(ctx context.Context, p NewPayment) (int64, error)
	// SetStayPaid moves the locked active stay to paid at now with its exit
	// deadline.
	SetStayPaid(ctx context.Context, lotID, stayID int64, now, exitDeadline time.Time) error
	// ReactivateStay moves the locked paid stay back to active, keeping its
	// exit deadline (M2C-35).
	ReactivateStay(ctx context.Context, lotID, stayID int64, now time.Time) error
	// MarkStayLost marks the locked active or paid ticket stay lost, replaces
	// its ticket code hash and makes it active.
	MarkStayLost(ctx context.Context, lotID, stayID int64, hash []byte, now time.Time) error
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

// Stay is a locked stay. EmployeeCardID is 0 for a ticket; a zero time is a
// null column.
type Stay struct {
	ID              int64
	PublicID        uuid.UUID
	LotID           int64
	CredentialKind  string
	Status          Status
	EmployeeCardID  int64
	TariffVersionID int64
	EnteredAt       time.Time
	ExitDeadlineAt  time.Time
	LostTicket      bool
}

// NewPayment is a succeeded payment of a stay (design §5.4). DeviceID is the
// pay station.
type NewPayment struct {
	StayID         int64
	Method         string
	AmountCents    int64
	IdempotencyKey string
	DeviceID       int64
	SucceededAt    time.Time
}
