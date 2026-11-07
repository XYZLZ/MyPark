package auth

import (
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"time"

	"github.com/google/uuid"

	"github.com/XYZLZ/MyPark/apps/server/internal/features/audit"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/legal"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/notifications"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/clock"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/database"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/hashing"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/jwt"
)

// Deps are the collaborators of Service. Rand defaults to crypto/rand.Reader.
type Deps struct {
	Tx     database.TxRunnerPort
	Repo   Repo
	Hasher hashing.PasswordHasher
	Codes  hashing.CodeHasher
	Legal  legal.Current
	Audit  audit.Recorder
	Notify notifications.Notifier
	Clock  clock.Clock
	Rand   io.Reader
	// Signer is used by the login use cases of later slices.
	Signer *jwt.Signer
}

// Service implements the auth use cases.
type Service struct {
	d Deps
}

// NewService returns a Service over d.
func NewService(d Deps) *Service {
	if d.Rand == nil {
		d.Rand = rand.Reader
	}
	return &Service{d: d}
}

// issueCode consumes the live code of user for p, stores a new one and
// returns its plaintext and id. It must run inside a transaction. The caller
// enqueues the email, so the plaintext never leaves the transaction.
func (s *Service) issueCode(ctx context.Context, user UserRow, p Purpose, ttl time.Duration) (string, int64, error) {
	code, err := NewCode(s.d.Rand)
	if err != nil {
		return "", 0, err
	}
	now := s.d.Clock.Now()
	if err := s.d.Repo.ConsumeLiveCodes(ctx, user.ID, p, now); err != nil {
		return "", 0, err
	}
	hash := s.d.Codes.Hash(string(p), user.PublicID, code)
	id, err := s.d.Repo.InsertCode(ctx, user.ID, p, hash, now.Add(ttl), now)
	if err != nil {
		return "", 0, err
	}
	return code, id, nil
}

// checkCode counts an attempt on the live code and compares it in constant
// time. On a match it consumes the code, in the caller's transaction. On a
// mismatch it returns false with no error: the caller must commit so the
// attempt persists, then answer auth.code_invalid. For an unknown account
// the caller passes userID 0 and uuid.Nil, which does the same work against
// no row.
func (s *Service) checkCode(ctx context.Context, userID int64, publicID uuid.UUID, p Purpose, code string) (bool, error) {
	now := s.d.Clock.Now()
	live, found, err := s.d.Repo.AttemptCode(ctx, userID, p, now, MaxCodeAttempts)
	if err != nil {
		return false, err
	}
	want := live.CodeHash
	if !found {
		want = make([]byte, 32)
	}
	match := hashing.Equal(s.d.Codes.Hash(string(p), publicID, code), want)
	if !found || !match {
		return false, nil
	}
	if err := s.d.Repo.ConsumeCode(ctx, live.ID, now); err != nil {
		return false, err
	}
	return true, nil
}

// enqueueVerifyEmail queues the verification code email.
func (s *Service) enqueueVerifyEmail(ctx context.Context, to string, u UserRow, code string, codeID int64) error {
	uid := u.ID
	err := s.d.Notify.Enqueue(ctx, notifications.Email{
		Template:  notifications.TplVerifyEmail,
		Locale:    u.Locale,
		To:        to,
		UserID:    &uid,
		Data:      map[string]string{"code": code},
		DedupeKey: fmt.Sprintf("verify_email:%d", codeID),
	})
	if err != nil {
		return fmt.Errorf("auth: enqueue verify email: %w", err)
	}
	return nil
}
