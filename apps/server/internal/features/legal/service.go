package legal

import (
	"context"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"

	"github.com/XYZLZ/MyPark/apps/server/internal/domain/apperr"
	"github.com/XYZLZ/MyPark/apps/server/internal/domain/errcodes"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/clock"
)

// refreshTimeout bounds the shared database read of a cache refresh.
const refreshTimeout = 5 * time.Second

// Service implements Current. It caches the current versions for ttl,
// measured on the injected clock.
type Service struct {
	repo  Repo
	clock clock.Clock
	ttl   time.Duration

	mu      sync.Mutex
	cached  Versions
	expires time.Time

	flight singleflight.Group
}

var _ Current = (*Service)(nil)

// NewService returns a Service caching repo results for ttl.
func NewService(repo Repo, c clock.Clock, ttl time.Duration) *Service {
	return &Service{repo: repo, clock: c, ttl: ttl}
}

// Versions returns the current documents, from the cache while it is fresh.
// Concurrent callers of a stale cache share one database read, and the lock
// is never held across it: a caller whose ctx ends stops waiting at once and
// never queues the others behind a slow query.
func (s *Service) Versions(ctx context.Context) (Versions, error) {
	now := s.clock.Now()
	if v, ok := s.fresh(now); ok {
		return v, nil
	}
	ch := s.flight.DoChan("current", func() (any, error) {
		// A flight that finished after our first check already refreshed the
		// cache: use it instead of reading again.
		if v, ok := s.fresh(now); ok {
			return v, nil
		}
		// The read is shared, so it must not die with the first caller's ctx.
		rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), refreshTimeout)
		defer cancel()
		v, err := s.repo.Current(rctx, now)
		if err != nil {
			return nil, err
		}
		s.mu.Lock()
		s.cached, s.expires = v, now.Add(s.ttl)
		s.mu.Unlock()
		return v, nil
	})
	select {
	case <-ctx.Done():
		return Versions{}, ctx.Err()
	case res := <-ch:
		if res.Err != nil {
			return Versions{}, res.Err
		}
		return res.Val.(Versions), nil
	}
}

func (s *Service) fresh(now time.Time) (Versions, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cached, now.Before(s.expires)
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
