package reservations

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/XYZLZ/MyPark/apps/server/internal/features/availability"
)

// Repository errors the service maps to API errors.
var (
	// ErrNotFound means the lot or the vehicle does not exist for the caller.
	ErrNotFound = errors.New("reservations: not found")
	// ErrNoPolicy means the lot has no policy version in force.
	ErrNoPolicy = errors.New("reservations: no policy version")
	// ErrActiveExists means the driver already has an active reservation.
	ErrActiveExists = errors.New("reservations: active reservation exists")
)

// Lot is the lot as the create and quote checks read it (unlocked).
type Lot struct {
	ID           int64
	PublicID     uuid.UUID
	Closed       bool
	Capacity     int
	OutOfService int
}

// VehicleRef is a live vehicle of the caller.
type VehicleRef struct {
	ID           int64
	PublicID     uuid.UUID
	PlateDisplay string
	Make         string
	Model        string
	Color        string
}

// NewReservation is a reservation to insert as pending_payment.
type NewReservation struct {
	UserID, VehicleID, LotID         int64
	Arrival                          time.Time
	Window                           Window
	FeeCents                         int64
	TariffVersionID, PolicyVersionID int64
	PaymentDueAt, Now                time.Time
}

// Row is a stored reservation as the insert returns it.
type Row struct {
	ID           int64
	PublicID     uuid.UUID
	Arrival      time.Time
	Status       Status
	FeeCents     int64
	PaymentDueAt time.Time
	CreatedAt    time.Time
}

// Repo is the storage port. Every method runs on the transaction carried by
// the context, or on the pool when there is none.
type Repo interface {
	ReadRepo
	// SetTxLimits applies the driver-API lock and statement timeouts (M3B-55)
	// to the current transaction.
	SetTxLimits(ctx context.Context) error
	// LotForReservation is an unlocked read; ErrNotFound when unknown.
	LotForReservation(ctx context.Context, lotPublicID uuid.UUID) (Lot, error)
	// PolicyAt returns the policy in force at now, or ErrNoPolicy.
	PolicyAt(ctx context.Context, lotID int64, now time.Time) (Policy, error)
	// VehicleForShare reads a live vehicle of the user with FOR SHARE;
	// ErrNotFound when it is not theirs or is deleted.
	VehicleForShare(ctx context.Context, userID int64, vehiclePublicID uuid.UUID) (VehicleRef, error)
	// BlockedUntil is the end of the no-show block, or the zero time.
	BlockedUntil(ctx context.Context, userID int64) (time.Time, error)
	// HasActive reports a pending_payment or confirmed reservation.
	HasActive(ctx context.Context, userID int64) (bool, error)
	// Insert maps the one-active-per-user index to ErrActiveExists.
	Insert(ctx context.Context, n NewReservation) (Row, error)
	// EnsureAndLockSlots creates the missing slot rows of the window and
	// locks them in ascending slot_start order (design 8.3, M3B-14). It
	// returns each slot's held count.
	EnsureAndLockSlots(ctx context.Context, lotID int64, w Window) ([]int, error)
	// IncSlots and DecSlots move held by one over the window's slots.
	IncSlots(ctx context.Context, lotID int64, w Window) error
	DecSlots(ctx context.Context, lotID int64, w Window) error
	// SlotLoads is an unlocked read for the quote; missing slots are absent.
	SlotLoads(ctx context.Context, lotID int64, w Window) ([]int, error)
}

// Availability is the public availability the no-space-now check reads
// (M3B-20).
type Availability interface {
	Snapshot(ctx context.Context, lotID int64) (availability.Snapshot, error)
}

// Record is a stored reservation with its vehicle and lot, as the read and
// change paths load it.
type Record struct {
	RowID     int64
	VehicleID int64
	View
}

// Cursor is the keyset position after the last reservation of a page.
type Cursor struct {
	ArrivalAt time.Time
	PublicID  uuid.UUID
}

// ListFilter selects one page of a driver's reservations, newest arrival first.
type ListFilter struct {
	// Scope is "" (every status), "active" or "past".
	Scope string
	After *Cursor
	Limit int
}

// ReadRepo is the part of the storage port behind the read and change use
// cases. Like Repo, every method runs on the context transaction.
type ReadRepo interface {
	// Get reads one reservation of the user, or ErrNotFound.
	Get(ctx context.Context, userID int64, id uuid.UUID) (Record, error)
	// List returns at most f.Limit reservations of the user.
	List(ctx context.Context, userID int64, f ListFilter) ([]Record, error)
}
