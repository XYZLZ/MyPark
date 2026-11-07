package users

import (
	"github.com/google/uuid"

	"context"
	"errors"
	"time"

	"github.com/XYZLZ/MyPark/apps/server/internal/features/auth"
)

// ErrNotFound means the user does not exist or is already deleted.
var ErrNotFound = errors.New("users: not found")

// ProfileUpdate is a partial profile change. A nil pointer leaves the column
// as it is. Phone is written only when SetPhone is true, and a nil Phone then
// clears it.
type ProfileUpdate struct {
	FirstName, LastName, Locale *string
	SetPhone                    bool
	Phone                       *string
}

// Repo is the persistence port of the users feature. Every method runs in the
// transaction carried by ctx when there is one.
type Repo interface {
	AdminRepo

	// GetProfile returns the live user, or ErrNotFound.
	GetProfile(ctx context.Context, id int64) (auth.UserView, error)
	// UpdateProfile applies u and returns the updated user, or ErrNotFound.
	UpdateProfile(ctx context.Context, id int64, u ProfileUpdate, now time.Time) (auth.UserView, error)
	// AnonymizeProfile replaces the name with "deleted" and clears the phone
	// of a live user. It returns ErrNotFound when there is none.
	AnonymizeProfile(ctx context.Context, id int64, now time.Time) error
	// SetLegalAcceptance stores the accepted versions, stamped now.
	SetLegalAcceptance(ctx context.Context, id int64, terms, privacy string, now time.Time) error
}

// AdminUser is an admin account as the admin area shows it.
type AdminUser struct {
	ID        int64
	PublicID  uuid.UUID
	Email     string
	FirstName string
	LastName  string
	Status    string
	Locale    string
	CreatedAt time.Time
}

// AdminCursor is the position after the last admin of a page.
type AdminCursor struct {
	At time.Time
	ID int64
}

// AdminRepo is the admin-listing part of the persistence port.
type AdminRepo interface {
	// ListAdmins returns up to limit non-deleted admins, newest first
	// (created_at desc, id desc), strictly after after when it is not nil.
	ListAdmins(ctx context.Context, limit int, after *AdminCursor) ([]AdminUser, error)
}
