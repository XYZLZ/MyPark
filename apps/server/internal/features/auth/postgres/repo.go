// Package postgres is the PostgreSQL adapter of the auth feature.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/XYZLZ/MyPark/apps/server/internal/domain/authz"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/auth"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/database"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/database/dbgen"
)

// emailActiveKey is the unique index on live emails.
const emailActiveKey = "users_email_active_key"

// Repo implements auth.Repo over dbgen. It reads and writes through the
// transaction carried by the context, or through the pool when there is none.
type Repo struct{ pool *pgxpool.Pool }

var _ auth.Repo = (*Repo)(nil)

// NewRepo returns a Repo over pool.
func NewRepo(pool *pgxpool.Pool) *Repo { return &Repo{pool: pool} }

func (r *Repo) q(ctx context.Context) *dbgen.Queries {
	return dbgen.New(database.Conn(ctx, r.pool))
}

func ts(t time.Time) pgtype.Timestamptz { return pgtype.Timestamptz{Time: t, Valid: true} }

func text(s string) pgtype.Text { return pgtype.Text{String: s, Valid: true} }

func optText(s *string) pgtype.Text {
	if s == nil {
		return pgtype.Text{}
	}
	return text(*s)
}

// LockUserByEmail implements auth.Repo.
func (r *Repo) LockUserByEmail(ctx context.Context, email string) (auth.UserRow, bool, error) {
	row, err := r.q(ctx).LockUserByEmail(ctx, email)
	if errors.Is(err, pgx.ErrNoRows) {
		return auth.UserRow{}, false, nil
	}
	if err != nil {
		return auth.UserRow{}, false, safeError("auth: lock user", err)
	}
	return auth.UserRow{ID: row.ID, PublicID: row.PublicID, Role: row.Role, Status: row.Status, Locale: row.Locale}, true, nil
}

// LockPendingDriverByEmail implements auth.Repo.
func (r *Repo) LockPendingDriverByEmail(ctx context.Context, email string) (auth.UserRow, bool, error) {
	row, err := r.q(ctx).LockPendingDriverByEmail(ctx, email)
	if errors.Is(err, pgx.ErrNoRows) {
		return auth.UserRow{}, false, nil
	}
	if err != nil {
		return auth.UserRow{}, false, safeError("auth: lock pending driver", err)
	}
	return auth.UserRow{ID: row.ID, PublicID: row.PublicID, Role: auth.RoleDriver, Status: auth.StatusPending, Locale: row.Locale}, true, nil
}

// InsertPendingDriver implements auth.Repo.
func (r *Repo) InsertPendingDriver(ctx context.Context, d auth.NewDriver) (auth.UserRow, error) {
	row, err := r.q(ctx).InsertPendingDriver(ctx, dbgen.InsertPendingDriverParams{
		Email: d.Email, PasswordHash: text(d.PasswordHash), Now: ts(d.Now),
		FirstName: d.FirstName, LastName: d.LastName, Phone: optText(d.Phone), Locale: d.Locale,
		TermsVersion: text(d.TermsVersion), PrivacyVersion: text(d.PrivacyVersion),
	})
	if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok && pgErr.Code == "23505" && pgErr.ConstraintName == emailActiveKey {
		return auth.UserRow{}, auth.ErrEmailTaken
	}
	if err != nil {
		return auth.UserRow{}, safeError("auth: insert driver", err)
	}
	return auth.UserRow{ID: row.ID, PublicID: row.PublicID, Role: auth.RoleDriver, Status: auth.StatusPending, Locale: d.Locale}, nil
}

// OverwritePendingDriver implements auth.Repo.
func (r *Repo) OverwritePendingDriver(ctx context.Context, id int64, d auth.NewDriver) error {
	err := r.q(ctx).OverwritePendingDriver(ctx, dbgen.OverwritePendingDriverParams{
		ID: id, PasswordHash: text(d.PasswordHash), Now: ts(d.Now),
		FirstName: d.FirstName, LastName: d.LastName, Phone: optText(d.Phone), Locale: d.Locale,
		TermsVersion: text(d.TermsVersion), PrivacyVersion: text(d.PrivacyVersion),
	})
	return safeError("auth: overwrite pending driver", err)
}

// ActivateUser implements auth.Repo.
func (r *Repo) ActivateUser(ctx context.Context, id int64, now time.Time) error {
	return safeError("auth: activate user", r.q(ctx).ActivateUser(ctx, dbgen.ActivateUserParams{ID: id, Now: ts(now)}))
}

// SetPassword implements auth.Repo.
func (r *Repo) SetPassword(ctx context.Context, userID int64, hash string, now time.Time) error {
	err := r.q(ctx).SetPassword(ctx, dbgen.SetPasswordParams{ID: userID, PasswordHash: text(hash), Now: ts(now)})
	return safeError("auth: set password", err)
}

// ConsumeLiveCodes implements auth.Repo.
func (r *Repo) ConsumeLiveCodes(ctx context.Context, userID int64, p auth.Purpose, now time.Time) error {
	err := r.q(ctx).ConsumeLiveAuthCodes(ctx, dbgen.ConsumeLiveAuthCodesParams{UserID: userID, Purpose: string(p), Now: ts(now)})
	return safeError("auth: consume live codes", err)
}

// GetPasswordHash implements auth.Repo.
func (r *Repo) GetPasswordHash(ctx context.Context, userID int64) (string, bool, error) {
	h, err := r.q(ctx).GetPasswordHash(ctx, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, safeError("auth: get password hash", err)
	}
	return h.String, h.Valid, nil
}

// EraseUser implements auth.Repo.
func (r *Repo) EraseUser(ctx context.Context, userID int64, now time.Time) (int64, error) {
	n, err := r.q(ctx).EraseAuthUser(ctx, dbgen.EraseAuthUserParams{ID: userID, Now: ts(now)})
	if err != nil {
		return 0, safeError("auth: erase user", err)
	}
	return n, nil
}

// InsertCode implements auth.Repo.
func (r *Repo) InsertCode(ctx context.Context, userID int64, p auth.Purpose, hash []byte, expiresAt, now time.Time) (int64, error) {
	id, err := r.q(ctx).InsertAuthCode(ctx, dbgen.InsertAuthCodeParams{
		UserID: userID, Purpose: string(p), CodeHash: hash, ExpiresAt: ts(expiresAt), Now: ts(now),
	})
	if err != nil {
		return 0, safeError("auth: insert code", err)
	}
	return id, nil
}

// AttemptCode implements auth.Repo.
func (r *Repo) AttemptCode(ctx context.Context, userID int64, p auth.Purpose, now time.Time, maxAttempts int) (auth.CodeAttempt, bool, error) {
	row, err := r.q(ctx).AttemptAuthCode(ctx, dbgen.AttemptAuthCodeParams{
		UserID: userID, Purpose: string(p), Now: ts(now), MaxAttempts: int16(maxAttempts),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return auth.CodeAttempt{}, false, nil
	}
	if err != nil {
		return auth.CodeAttempt{}, false, safeError("auth: attempt code", err)
	}
	return auth.CodeAttempt{ID: row.ID, CodeHash: row.CodeHash}, true, nil
}

// ConsumeCode implements auth.Repo.
func (r *Repo) ConsumeCode(ctx context.Context, id int64, now time.Time) error {
	return safeError("auth: consume code", r.q(ctx).ConsumeAuthCode(ctx, dbgen.ConsumeAuthCodeParams{ID: id, Now: ts(now)}))
}

// GetSessionPrincipal implements auth.Repo.
func (r *Repo) GetSessionPrincipal(ctx context.Context, sessionPublicID, userPublicID uuid.UUID, now time.Time) (auth.SessionPrincipal, bool, error) {
	row, err := r.q(ctx).GetSessionPrincipal(ctx, dbgen.GetSessionPrincipalParams{
		Now: ts(now), SessionPublicID: sessionPublicID, UserPublicID: userPublicID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return auth.SessionPrincipal{}, false, nil
	}
	if err != nil {
		return auth.SessionPrincipal{}, false, safeError("auth: get session principal", err)
	}
	return auth.SessionPrincipal{
		SessionID: row.SessionID, Revoked: row.RevokedAt.Valid, Expired: row.Expired,
		UserID: row.UserID, Role: row.Role, Status: row.Status,
		TermsVersion: textPtr(row.TermsVersion), PrivacyVersion: textPtr(row.PrivacyVersion),
	}, true, nil
}

// RevokeSession implements auth.Repo.
func (r *Repo) RevokeSession(ctx context.Context, sessionID int64, reason string, now time.Time) (bool, error) {
	n, err := r.q(ctx).RevokeSession(ctx, dbgen.RevokeSessionParams{ID: sessionID, Reason: text(reason), Now: ts(now)})
	if err != nil {
		return false, safeError("auth: revoke session", err)
	}
	return n > 0, nil
}

// RevokeUserSessions implements auth.Repo.
func (r *Repo) RevokeUserSessions(ctx context.Context, userID int64, reason string, now time.Time) (int, error) {
	n, err := r.q(ctx).RevokeUserSessions(ctx, dbgen.RevokeUserSessionsParams{UserID: userID, Reason: text(reason), Now: ts(now)})
	if err != nil {
		return 0, safeError("auth: revoke user sessions", err)
	}
	return int(n), nil
}

func textPtr(t pgtype.Text) *string {
	if !t.Valid {
		return nil
	}
	return &t.String
}

// safeError drops the server message and detail of a Postgres error: a
// unique or check violation detail echoes the failing row, which holds the
// email and names. A nil err stays nil.
func safeError(op string, err error) error {
	if err == nil {
		return nil
	}
	if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok {
		return fmt.Errorf("%s: sqlstate %s constraint %q", op, pgErr.Code, pgErr.ConstraintName)
	}
	return fmt.Errorf("%s: %w", op, err)
}

// FindUserForLogin implements auth.Repo.
func (r *Repo) FindUserForLogin(ctx context.Context, email string) (auth.LoginUser, bool, error) {
	row, err := r.q(ctx).FindUserForLogin(ctx, email)
	if errors.Is(err, pgx.ErrNoRows) {
		return auth.LoginUser{}, false, nil
	}
	if err != nil {
		return auth.LoginUser{}, false, safeError("auth: find user for login", err)
	}
	role, _ := authz.ParseRole(row.Role)
	return auth.LoginUser{
		UserRow:      auth.UserRow{ID: row.ID, PublicID: row.PublicID, Role: row.Role, Status: row.Status, Locale: row.Locale},
		PasswordHash: row.PasswordHash.String,
		HasPassword:  row.PasswordHash.Valid,
		View: auth.UserView{
			PublicID: row.PublicID, Email: row.Email, FirstName: row.FirstName, LastName: row.LastName,
			Phone: strPtr(row.Phone), Role: role, Status: row.Status, Locale: row.Locale,
			EmailVerifiedAt: timePtr(row.EmailVerifiedAt),
			TermsVersion:    strPtr(row.TermsVersion), TermsAcceptedAt: timePtr(row.TermsAcceptedAt),
			PrivacyVersion: strPtr(row.PrivacyVersion), PrivacyAcceptedAt: timePtr(row.PrivacyAcceptedAt),
			CreatedAt: row.CreatedAt.Time,
		},
	}, true, nil
}

// RehashPassword implements auth.Repo.
func (r *Repo) RehashPassword(ctx context.Context, userID int64, oldHash, newHash string, now time.Time) error {
	err := r.q(ctx).RehashPassword(ctx, dbgen.RehashPasswordParams{
		ID: userID, OldHash: text(oldHash), NewHash: text(newHash), Now: ts(now),
	})
	return safeError("auth: rehash password", err)
}

// InsertSession implements auth.Repo.
func (r *Repo) InsertSession(ctx context.Context, s auth.NewSession) (auth.SessionRow, error) {
	arg := dbgen.InsertAuthSessionParams{
		UserID: s.UserID, ClientKind: string(s.ClientKind), Now: ts(s.Now), AbsoluteExpiresAt: ts(s.Absolute),
	}
	if s.IP.IsValid() {
		ip := s.IP.Unmap()
		arg.CreatedIp = &ip
	}
	if s.UserAgent != "" {
		arg.UserAgent = text(s.UserAgent)
	}
	row, err := r.q(ctx).InsertAuthSession(ctx, arg)
	if err != nil {
		return auth.SessionRow{}, safeError("auth: insert session", err)
	}
	return auth.SessionRow{ID: row.ID, PublicID: row.PublicID}, nil
}

// InsertRefreshToken implements auth.Repo.
func (r *Repo) InsertRefreshToken(ctx context.Context, sessionID int64, hash []byte, expiresAt, now time.Time) error {
	err := r.q(ctx).InsertRefreshToken(ctx, dbgen.InsertRefreshTokenParams{
		SessionID: sessionID, TokenHash: hash, Now: ts(now), ExpiresAt: ts(expiresAt),
	})
	return safeError("auth: insert refresh token", err)
}

func strPtr(t pgtype.Text) *string {
	if !t.Valid {
		return nil
	}
	return &t.String
}

func timePtr(t pgtype.Timestamptz) *time.Time {
	if !t.Valid {
		return nil
	}
	return &t.Time
}

// LockRefreshToken implements auth.Repo.
func (r *Repo) LockRefreshToken(ctx context.Context, hash []byte) (auth.RefreshRow, bool, error) {
	row, err := r.q(ctx).LockRefreshToken(ctx, hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return auth.RefreshRow{}, false, nil
	}
	if err != nil {
		return auth.RefreshRow{}, false, safeError("auth: lock refresh token", err)
	}
	role, _ := authz.ParseRole(row.Role)
	return auth.RefreshRow{
		Snapshot: auth.RefreshSnapshot{
			TokenRotatedAt: timePtr(row.RotatedAt), SessionRevokedAt: timePtr(row.RevokedAt),
			TokenExpiresAt: row.TokenExpiresAt.Time, SessionAbsoluteExpiresAt: row.AbsoluteExpiresAt.Time,
			UserStatus: row.Status,
		},
		TokenID: row.TokenID, SessionID: row.SessionID, SessionPublicID: row.SessionPublicID,
		User: auth.UserRow{ID: row.UserID, PublicID: row.UserPublicID, Role: row.Role, Status: row.Status, Locale: row.Locale},
		View: auth.UserView{
			PublicID: row.UserPublicID, Email: row.Email, FirstName: row.FirstName, LastName: row.LastName,
			Phone: strPtr(row.Phone), Role: role, Status: row.Status, Locale: row.Locale,
			EmailVerifiedAt: timePtr(row.EmailVerifiedAt),
			TermsVersion:    strPtr(row.TermsVersion), TermsAcceptedAt: timePtr(row.TermsAcceptedAt),
			PrivacyVersion: strPtr(row.PrivacyVersion), PrivacyAcceptedAt: timePtr(row.PrivacyAcceptedAt),
			CreatedAt: row.UserCreatedAt.Time,
		},
	}, true, nil
}

// RotateRefreshToken implements auth.Repo.
func (r *Repo) RotateRefreshToken(ctx context.Context, tokenID int64, now time.Time) error {
	err := r.q(ctx).RotateRefreshToken(ctx, dbgen.RotateRefreshTokenParams{ID: tokenID, Now: ts(now)})
	return safeError("auth: rotate refresh token", err)
}

// TouchSession implements auth.Repo.
func (r *Repo) TouchSession(ctx context.Context, sessionID int64, now time.Time) error {
	err := r.q(ctx).TouchAuthSession(ctx, dbgen.TouchAuthSessionParams{ID: sessionID, Now: ts(now)})
	return safeError("auth: touch session", err)
}
