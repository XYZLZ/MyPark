package vehicles

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

// Vehicle is a driver's vehicle as exposed to clients.
type Vehicle struct {
	PublicID        uuid.UUID
	Plate           string
	PlateNormalized string
	Make            string
	Model           string
	Color           string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// Record is a stored vehicle with its internal id, which never leaves the
// service (it feeds the ReservationChecker only).
type Record struct {
	ID int64
	Vehicle
}

// Fields are the editable columns of a vehicle, already validated and
// normalized.
type Fields struct {
	PlateNormalized string
	PlateDisplay    string
	Make            string
	Model           string
	Color           string
}

// Repository errors the service maps to API errors.
var (
	// ErrNotFound means no live vehicle with that id belongs to the owner.
	ErrNotFound = errors.New("vehicles: not found")
	// ErrPlateTaken means the plate is active on another vehicle.
	ErrPlateTaken = errors.New("vehicles: plate taken")
)

// Repo is the storage port. Every method takes the owner id and filters by
// it, so another driver's vehicle is indistinguishable from a missing one.
// There is deliberately no lookup without an owner.
type Repo interface {
	// LockOwner takes a row lock on the owner (ErrNotFound when absent) so
	// that one user's vehicle writes are serialized.
	LockOwner(ctx context.Context, ownerID int64) error
	CountActiveOwned(ctx context.Context, ownerID int64) (int, error)
	// Insert returns ErrPlateTaken on a plate collision.
	Insert(ctx context.Context, ownerID int64, f Fields, now time.Time) (Record, error)
	ListOwned(ctx context.Context, ownerID int64) ([]Record, error)
	GetOwned(ctx context.Context, ownerID int64, id uuid.UUID) (Record, error)
	// GetOwnedForUpdate is GetOwned with the row locked until the
	// transaction ends.
	GetOwnedForUpdate(ctx context.Context, ownerID int64, id uuid.UUID) (Record, error)
	// UpdateOwned returns ErrNotFound or ErrPlateTaken.
	UpdateOwned(ctx context.Context, ownerID int64, id uuid.UUID, f Fields, now time.Time) (Record, error)
	SoftDeleteOwned(ctx context.Context, ownerID int64, id uuid.UUID, now time.Time) error
	// EraseOwner soft-deletes and scrubs every live vehicle of the owner.
	EraseOwner(ctx context.Context, ownerID int64, now time.Time) error
}

// ReservationChecker tells whether a vehicle has an active reservation
// (FR-VEH-4). The M1 adapter is NoReservations; M3 replaces it.
type ReservationChecker interface {
	HasActive(ctx context.Context, vehicleID int64) (bool, error)
}

// NoReservations is the M1 ReservationChecker: nothing is ever reserved
// (OC-29).
type NoReservations struct{}

// HasActive implements ReservationChecker.
func (NoReservations) HasActive(context.Context, int64) (bool, error) { return false, nil }

// OwnerEraser is the port account deletion consumes to anonymize a user's
// vehicles inside its own transaction.
type OwnerEraser interface {
	EraseOwner(ctx context.Context, ownerID int64) error
}

var _ OwnerEraser = (*Service)(nil)
