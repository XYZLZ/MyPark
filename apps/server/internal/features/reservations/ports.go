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
	ExpiryRepo
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

	// ForApply is an unlocked read by internal id; ErrNotFound when unknown.
	ForApply(ctx context.Context, id int64) (ApplyRow, error)
	// LockForApply reads the reservation with FOR UPDATE.
	LockForApply(ctx context.Context, id int64) (ApplyRow, error)
	// Confirm moves a pending_payment reservation to confirmed with a new QR nonce.
	Confirm(ctx context.Context, id int64, nonce [16]byte, now time.Time) error
	// ExpireUnpaidLate moves a pending_payment reservation to expired_unpaid
	// with the given refund reason (late success, design 9.4); "" stores null.
	ExpireUnpaidLate(ctx context.Context, id int64, refundReason string, now time.Time) error
	// Cancel moves a pending_payment or confirmed reservation to cancelled by
	// the given actor; refundReason "" stores null. Fenced on those statuses.
	Cancel(ctx context.Context, id int64, cancelledBy, refundReason string, now time.Time) error
	// QRData reads what the QR token is built from; ErrNotFound when it is not the user's.
	QRData(ctx context.Context, userID int64, id uuid.UUID) (QRData, error)
	// LockLot takes the lot_occupancy row FOR UPDATE (first in the lock order).
	LockLot(ctx context.Context, lotID int64) error
	// BumpLot raises the lot version and notifies lot_changed (design 8.2). The
	// caller holds the lot lock.
	BumpLot(ctx context.Context, lotID int64, now time.Time) error
	// UserContact is the recipient of the user's emails.
	UserContact(ctx context.Context, userID int64) (Contact, error)

	// ForQR is the QR entry's unlocked lookup of the lot's reservation
	// (ID, PublicID and Nonce only); ErrNotFound when the lot has none.
	ForQR(ctx context.Context, lotID int64, publicID uuid.UUID) (EntryRow, error)
	// LockForEntry locks the lot's reservation FOR UPDATE; ErrNotFound when unknown.
	LockForEntry(ctx context.Context, lotID, id int64) (EntryRow, error)
	// CheckIn moves a confirmed reservation to checked_in at now. Fenced on confirmed.
	CheckIn(ctx context.Context, id int64, now time.Time) error
	// UndoCheckIn moves a checked_in reservation back to confirmed, clearing
	// checked_in_at (M3B-60). Fenced on checked_in.
	UndoCheckIn(ctx context.Context, id int64, now time.Time) error
	// Complete moves a checked_in reservation to completed at now (the exit
	// pass of its stay). Fenced on checked_in.
	Complete(ctx context.Context, id int64, now time.Time) error
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
	// Lock is Get with the reservation locked for update.
	Lock(ctx context.Context, userID int64, id uuid.UUID) (Record, error)
	// List returns at most f.Limit reservations of the user.
	List(ctx context.Context, userID int64, f ListFilter) ([]Record, error)
	// SetVehicle points the reservation at another vehicle.
	SetVehicle(ctx context.Context, id, vehicleID int64, now time.Time) error
}

// ApplyRow is the part of a reservation Apply decides from, keyed by its
// internal id (the payment knows the reservation by id, not by owner).
type ApplyRow struct {
	ID       int64
	PublicID uuid.UUID
	UserID   int64
	LotID    int64
	Status   Status
	Window   Window

	// Set by the due-row locks of the expiry jobs only.
	PolicyVersionID int64

	// Set by LockForApply (and the due-row locks): what the emails show.
	ArrivalAt    time.Time
	FeeCents     int64
	PlateDisplay string
}

// QRData is what a reservation's QR token is built from.
type QRData struct {
	PublicID uuid.UUID
	Status   Status
	Nonce    []byte
	Window   Window
}

// Contact is the recipient of a user's emails.
type Contact struct{ Email, Locale string }

// EntryRow is a reservation as the QR entry reads it. Nonce is nil before
// confirmation.
type EntryRow struct {
	ID, UserID, LotID, TariffVersionID int64
	PublicID                           uuid.UUID
	Status                             Status
	Nonce                              []byte
	Window                             Window
}

// ExpiryRepo is the part of the storage port behind the worker's expiry
// jobs (design 12).
type ExpiryRepo interface {
	// ListDueUnpaid and ListDueNoShows are unlocked reads of the ids still to
	// expire at now, ascending, after afterID (a keyset, so rows a run skips
	// never hold the next page back).
	ListDueUnpaid(ctx context.Context, now time.Time, afterID int64, limit int) ([]int64, error)
	ListDueNoShows(ctx context.Context, now time.Time, afterID int64, limit int) ([]int64, error)
	// LockDueUnpaid and LockDueNoShow lock one candidate FOR UPDATE SKIP
	// LOCKED and re-check its predicates; ok is false when it is gone, no
	// longer due, or held by someone else. The row carries PolicyVersionID.
	LockDueUnpaid(ctx context.Context, id int64, now time.Time) (ApplyRow, bool, error)
	LockDueNoShow(ctx context.Context, id int64, now time.Time) (ApplyRow, bool, error)
	// ExpireNoShow moves a confirmed reservation to expired_no_show. Fenced.
	ExpireNoShow(ctx context.Context, id int64, now time.Time) error
	// CountNoShows counts the user's expired_no_show reservations whose
	// expired_at is within windowDays of now.
	CountNoShows(ctx context.Context, userID int64, now time.Time, windowDays int) (int, error)
	// BlockUser sets reservation_blocked_until to the later of its current
	// value and now + blockDays, and returns the result.
	BlockUser(ctx context.Context, userID int64, now time.Time, blockDays int) (time.Time, error)
	// PolicyByID reads a captured policy version; only the no-show fields are set.
	PolicyByID(ctx context.Context, id int64) (Policy, error)
	// ListDueReminders lists the confirmed reservations to remind at now, ascending
	// by id after afterID: arrival within 30 minutes, booked at least 30 minutes
	// ahead, and no reminder in the outbox yet (M3B-41).
	ListDueReminders(ctx context.Context, now time.Time, afterID int64, limit int) ([]ReminderRow, error)
}

// ReminderRow is a reservation due a reminder email.
type ReminderRow struct {
	ID, UserID int64
	PublicID   uuid.UUID
	ArrivalAt  time.Time
	Window     Window
}
