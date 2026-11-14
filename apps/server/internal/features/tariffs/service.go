package tariffs

import (
	"context"
	"errors"
	"time"
)

// ErrNoVersion means no tariff version matches: the lot has none yet in force.
var ErrNoVersion = errors.New("tariffs: no tariff version")

// Version is a stored tariff version.
type Version struct {
	ID, LotID int64
	Tariff    Tariff
}

// Current resolves tariff versions. Stays keep the ID of the version that
// priced them, so ByID returns it even after a newer one takes effect.
type Current interface {
	AtTime(ctx context.Context, lotID int64, t time.Time) (Version, error)
	ByID(ctx context.Context, id int64) (Version, error)
}

// Service implements Current over a Repo.
type Service struct{ repo Repo }

var _ Current = (*Service)(nil)

// NewService returns a Service reading from repo.
func NewService(repo Repo) *Service { return &Service{repo: repo} }

// AtTime implements Current.
func (s *Service) AtTime(ctx context.Context, lotID int64, t time.Time) (Version, error) {
	return s.repo.At(ctx, lotID, t)
}

// ByID implements Current.
func (s *Service) ByID(ctx context.Context, id int64) (Version, error) {
	return s.repo.ByID(ctx, id)
}
