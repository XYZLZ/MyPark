package gate

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/XYZLZ/MyPark/apps/server/internal/domain/authz"
	"github.com/XYZLZ/MyPark/apps/server/internal/domain/errcodes"
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
	// PaymentTotals sums the stay's succeeded payments.
	PaymentTotals(ctx context.Context, lotID, stayID int64) (paidCents int64, err error)
	// InsertPayment inserts a succeeded payment and returns its id. The stay
	// must come from a lot-scoped lock.
	InsertPayment(ctx context.Context, p NewPayment) (int64, error)
	// SetStayPaid moves the locked active stay to paid at now with its exit
	// deadline.
	SetStayPaid(ctx context.Context, lotID, stayID int64, now, exitDeadline time.Time) error
	// ReactivateStay moves the locked paid stay back to active, keeping its
	// exit deadline (M2C-35) and cancelling any exit authorization.
	ReactivateStay(ctx context.Context, lotID, stayID int64, now time.Time) error
	// MarkStayLost marks the locked active or paid ticket stay lost, replaces
	// its ticket code hash and makes it active. It cancels any exit
	// authorization.
	MarkStayLost(ctx context.Context, lotID, stayID int64, hash []byte, now time.Time) error

	// CardByHash returns the lot's employee card with this UID hash whatever
	// its status, or ErrNotFound. Only the exit uses it: a revoked card may
	// still leave through its open stay.
	CardByHash(ctx context.Context, lotID int64, hash []byte) (Card, error)
	// AuthorizeExit records at now that the locked active or paid stay may
	// pass the exit barrier (M2C-32).
	AuthorizeExit(ctx context.Context, lotID, stayID int64, now time.Time) error
	// SetStayExited moves the locked active or paid stay to exited at now,
	// setting paid_at to now only when it has none.
	SetStayExited(ctx context.Context, lotID, stayID int64, now time.Time) error
	// ClearExitAuthorization cancels the locked active or paid stay's exit
	// authorization.
	ClearExitAuthorization(ctx context.Context, lotID, stayID int64, now time.Time) error

	// ReservationStay reads the reservation's stay without a row lock, or
	// returns ErrNotFound. The QR entry decides from it under the lot and
	// reservation locks (design 8.1 puts stays after slot loads).
	ReservationStay(ctx context.Context, lotID, reservationID int64) (Stay, error)
	// ReservationStayForUpdate locks the reservation's stay, or returns
	// ErrNotFound.
	ReservationStayForUpdate(ctx context.Context, lotID, reservationID int64) (Stay, error)
	// ReopenReservationStay moves the locked voided reservation stay back to
	// entry_pending accepted at now, keeping voided_at (M3B-24).
	ReopenReservationStay(ctx context.Context, lotID, stayID int64, now time.Time) error
	// RefreshAccepted sets the locked entry_pending stay's accepted_at to now.
	RefreshAccepted(ctx context.Context, lotID, stayID int64, now time.Time) error

	// StayForUpdateByID locks the lot's stay with this id, or returns
	// ErrNotFound.
	StayForUpdateByID(ctx context.Context, lotID, stayID int64) (Stay, error)
	// VoidStay moves the locked entry_pending stay to voided at now (M3B-60).
	VoidStay(ctx context.Context, lotID, stayID int64, now time.Time) error
}

// ErrNotFound means the lot has no matching row.
var ErrNotFound = errors.New("gate: not found")

// LotOccupancy is the locked occupancy row and its lot.
type LotOccupancy struct {
	Occupied, Capacity int
	Closed             bool
}

// Card is an employee card. Revoked is set only by CardByHash: the other
// lookups return active cards.
type Card struct {
	ID       int64
	PublicID uuid.UUID
	Revoked  bool
}

// NewStay is an accepted entry. TicketCodeHash is set for a ticket,
// EmployeeCardID for an employee card, ReservationID for a reservation QR.
type NewStay struct {
	LotID           int64
	TicketCodeHash  []byte
	EmployeeCardID  int64
	ReservationID   int64
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
	AcceptedAt      time.Time
	EnteredAt       time.Time
	// PaidAt is the latest payment time: every payment moves it (design
	// §10.3), and an overstay is billed from it (M2C-35).
	PaidAt           time.Time
	ExitDeadlineAt   time.Time
	ExitAuthorizedAt time.Time
	LostTicket       bool
	// ReservationID is 0 unless the stay belongs to a reservation;
	// ReservationUserID is set only by the reservation stay lookups.
	ReservationID     int64
	ReservationUserID int64
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

// ReservationRef is a reservation resolved from a QR token before any lock.
// Nonce is the token's, re-checked under the reservation lock.
type ReservationRef struct {
	ID       int64
	PublicID uuid.UUID
	Nonce    [16]byte
}

// ReservationEntry is a reservation locked for an entry decision.
type ReservationEntry struct {
	ID, UserID, LotID, TariffVersionID int64
	PublicID                           uuid.UUID
	Status                             string
	WindowStart, WindowEnd             time.Time
}

// QRAction is what the entry barrier does with a reservation QR (design 11.1).
type QRAction int

// The QR actions; they mirror the reservations decision.
const (
	QRDeny QRAction = iota
	QRCheckIn
	QRReopen
	QRRefresh
	QRPhysicallyFull
)

// QRDecisionInput is what the QR decision reads under the locks. StayStatus
// is "" when the reservation has no stay.
type QRDecisionInput struct {
	Reservation                          ReservationEntry
	Now                                  time.Time
	Occupied, Pending, EffectiveCapacity int
	StayStatus                           Status
}

// QRDecision is the QR decision; Reason is set only for QRDeny.
type QRDecision struct {
	Action QRAction
	Reason errcodes.Code
}

// Reservations is the gate's view of reservations (design 5.1), implemented
// by reservations.GateAccess. Every method but Resolve and Decide runs on the
// device transaction in ctx, under the lot lock.
type Reservations interface {
	// Resolve verifies the token and finds the reservation in the lot,
	// without locks. Bad token, other lot, unknown or nonce mismatch →
	// ErrNotFound. The token never reaches an error or a log.
	Resolve(ctx context.Context, lotID int64, token string) (ReservationRef, error)
	// LockForEntry locks the reservation FOR UPDATE (after LockLotOccupancy)
	// and re-checks the nonce; a mismatch is ErrNotFound.
	LockForEntry(ctx context.Context, lotID int64, ref ReservationRef) (ReservationEntry, error)
	// Decide is the design 11.1 table.
	Decide(in QRDecisionInput) QRDecision
	// LockSlots locks the reservation's slot loads (design 8.1: before stays).
	LockSlots(ctx context.Context, r ReservationEntry) error
	CheckIn(ctx context.Context, r ReservationEntry, now time.Time) error                   // confirmed → checked_in, slots −1
	CancelUnadmitted(ctx context.Context, r ReservationEntry, now time.Time) error          // design 11.5 steps 1–5
	RestoreAfterVoid(ctx context.Context, reservationID int64, now time.Time) (bool, error) // S6.2
	LateEntryPass(ctx context.Context, reservationID int64, now time.Time) (bool, error)    // S6.2
	Complete(ctx context.Context, reservationID int64, now time.Time) error                 // S6.3; no-op unless checked_in
}
