package auth

import (
	"context"
	"errors"
	"net/netip"
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
	LoginRepo
	SessionRepo
	RefreshRepo

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

// LoginUser is the account a login attempt names. HasPassword is false for
// an invited admin that has not accepted yet.
type LoginUser struct {
	UserRow
	PasswordHash string
	HasPassword  bool
	View         UserView
}

// NewSession holds the values of a login session.
type NewSession struct {
	UserID     int64
	ClientKind ClientKind
	Now        time.Time
	Absolute   time.Time
	IP         netip.Addr
	UserAgent  string
}

// SessionRow is a stored session.
type SessionRow struct {
	ID       int64
	PublicID uuid.UUID
}

// LoginRepo is the part of the persistence port used by login.
type LoginRepo interface {
	// FindUserForLogin returns the non-deleted user with this email, without
	// locking it.
	FindUserForLogin(ctx context.Context, email string) (LoginUser, bool, error)
	// RehashPassword replaces oldHash with newHash, only while oldHash is
	// still the stored hash.
	RehashPassword(ctx context.Context, userID int64, oldHash, newHash string, now time.Time) error
	// InsertSession creates a login session.
	InsertSession(ctx context.Context, s NewSession) (SessionRow, error)
	// InsertRefreshToken stores the SHA-256 of a refresh token.
	InsertRefreshToken(ctx context.Context, sessionID int64, hash []byte, expiresAt, now time.Time) error
}

// Revocation reasons, as stored in auth_sessions.revoked_reason.
const (
	ReasonLogout         = "logout"
	ReasonLogoutAll      = "logout_all"
	ReasonPasswordReset  = "password_reset"
	ReasonReuseDetected  = "reuse_detected"
	ReasonAccountDeleted = "account_deleted"
)

// SessionPrincipal is a session and its user, as the per-request check reads
// them. Expired is judged by the database against the caller's clock.
type SessionPrincipal struct {
	SessionID                    int64
	Revoked, Expired             bool
	UserID                       int64
	Role, Status                 string
	TermsVersion, PrivacyVersion *string
}

// SessionRepo is the part of the persistence port used by the session check
// and by logout.
type SessionRepo interface {
	// GetSessionPrincipal returns the session with this public id and the user
	// with this public id, only when the session belongs to the user.
	GetSessionPrincipal(ctx context.Context, sessionPublicID, userPublicID uuid.UUID, now time.Time) (SessionPrincipal, bool, error)
	// RevokeSession revokes a live session and reports whether it did.
	RevokeSession(ctx context.Context, sessionID int64, reason string, now time.Time) (bool, error)
	// RevokeUserSessions revokes every live session of a user and returns how
	// many it revoked.
	RevokeUserSessions(ctx context.Context, userID int64, reason string, now time.Time) (int, error)
}

// RefreshRow is a refresh token with its session and user, as the refresh
// transaction reads them. Snapshot.Found is set by the service.
type RefreshRow struct {
	Snapshot        RefreshSnapshot
	TokenID         int64
	SessionID       int64
	SessionPublicID uuid.UUID
	User            UserRow
	View            UserView
}

// RefreshRepo is the part of the persistence port used by refresh rotation.
type RefreshRepo interface {
	// LockRefreshToken returns the token with this hash with its session and
	// user. The token and session rows stay locked until the transaction
	// ends, so concurrent presentations of one token serialize. found is
	// false for an unknown hash.
	LockRefreshToken(ctx context.Context, hash []byte) (row RefreshRow, found bool, err error)
	// RotateRefreshToken marks a token used.
	RotateRefreshToken(ctx context.Context, tokenID int64, now time.Time) error
	// TouchSession records the session as used.
	TouchSession(ctx context.Context, sessionID int64, now time.Time) error
}
