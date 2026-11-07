package auth

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

// Role and status values as stored in users.role and users.status.
const (
	RoleDriver = "driver"

	StatusPending = "pending_verification"
)

// ErrEmailTaken is returned by Repo.InsertPendingDriver when a concurrent
// registration already holds the address.
var ErrEmailTaken = errors.New("auth: email already registered")

// UserRow is the part of a users row the use cases need.
type UserRow struct {
	ID       int64
	PublicID uuid.UUID
	Role     string
	Status   string
	Locale   string
}

// NewDriver holds the values of a pending driver. Phone is nil for none.
type NewDriver struct {
	Email, PasswordHash, FirstName, LastName, Locale string
	Phone                                            *string
	TermsVersion, PrivacyVersion                     string
	Now                                              time.Time
}

// CodeAttempt is the live code after one attempt was counted.
type CodeAttempt struct {
	ID       int64
	CodeHash []byte
}

// Repo is the persistence port of the auth feature. Every method runs in the
// transaction carried by ctx when there is one.
type Repo interface {
	// LockUserByEmail returns the non-deleted user with this email, locked.
	LockUserByEmail(ctx context.Context, email string) (UserRow, bool, error)
	// LockPendingDriverByEmail returns the non-deleted driver in
	// pending_verification with this email, locked. Admin invitees are never
	// returned.
	LockPendingDriverByEmail(ctx context.Context, email string) (UserRow, bool, error)
	// InsertPendingDriver creates the driver, or returns ErrEmailTaken.
	InsertPendingDriver(ctx context.Context, d NewDriver) (UserRow, error)
	// OverwritePendingDriver replaces the credentials and profile of a pending driver.
	OverwritePendingDriver(ctx context.Context, id int64, d NewDriver) error
	// ActivateUser marks a pending user active and verified.
	ActivateUser(ctx context.Context, id int64, now time.Time) error
	// ConsumeLiveCodes consumes the live code of a user and purpose, if any.
	ConsumeLiveCodes(ctx context.Context, userID int64, p Purpose, now time.Time) error
	// InsertCode stores a new live code and returns its id.
	InsertCode(ctx context.Context, userID int64, p Purpose, hash []byte, expiresAt, now time.Time) (int64, error)
	// AttemptCode counts one attempt on the live, unexpired code that has
	// attempts left. It reports false when there is none.
	AttemptCode(ctx context.Context, userID int64, p Purpose, now time.Time, maxAttempts int) (CodeAttempt, bool, error)
	// ConsumeCode marks a code used.
	ConsumeCode(ctx context.Context, id int64, now time.Time) error
}
