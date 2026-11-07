package legal

import (
	"context"
	"sync"
	"time"

	"github.com/XYZLZ/MyPark/apps/server/internal/domain/apperr"
	"github.com/XYZLZ/MyPark/apps/server/internal/domain/errcodes"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/clock"
)

// Service implements Current. It caches the current versions for ttl,
// measured on the injected clock.
type Service struct {
	repo  Repo
	clock clock.Clock
	ttl   time.Duration

	mu      sync.Mutex
	cached  Versions
	expires time.Time
}

var _ Current = (*Service)(nil)

// NewService returns a Service caching repo results for ttl.
func NewService(repo Repo, c clock.Clock, ttl time.Duration) *Service {
	return &Service{repo: repo, clock: c, ttl: ttl}
}

// Versions returns the current documents, from the cache while it is fresh.
func (s *Service) Versions(ctx context.Context) (Versions, error) {
	now := s.clock.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	if now.Before(s.expires) {
		return s.cached, nil
	}
	v, err := s.repo.Current(ctx, now)
	if err != nil {
		return Versions{}, err
	}
	s.cached, s.expires = v, now.Add(s.ttl)
	return v, nil
}

// ValidateAcceptance implements Current.
func (s *Service) ValidateAcceptance(ctx context.Context, terms, privacy string) error {
	cur, err := s.Versions(ctx)
	if err != nil {
		return err
	}
	if terms == cur.Terms.Version && privacy == cur.Privacy.Version {
		return nil
	}
	return apperr.Conflict(errcodes.LegalVersionOutdated).WithDetails(map[string]any{
		"terms_version":   cur.Terms.Version,
		"privacy_version": cur.Privacy.Version,
	})
}
