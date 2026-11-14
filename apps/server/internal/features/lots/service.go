package lots

import (
	"context"

	"github.com/google/uuid"
)

// Summary is one entry of the lot list (design §11.1 LotSummary).
type Summary struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
	Slug string    `json:"slug"`
}

// OpeningHours is the M2 opening-hours shape: only always_open exists.
type OpeningHours struct {
	AlwaysOpen bool `json:"always_open"`
}

// Lot is the design §11.1 Lot. Status is not stored with the lot: it is the
// availability status (open, full or closed), set by the caller. InternalID
// never leaves the process.
type Lot struct {
	ID           uuid.UUID    `json:"id"`
	Slug         string       `json:"slug"`
	Name         string       `json:"name"`
	Address      string       `json:"address"`
	Latitude     *float64     `json:"latitude"`
	Longitude    *float64     `json:"longitude"`
	MapURL       string       `json:"map_url"`
	Timezone     string       `json:"timezone"`
	Capacity     int          `json:"capacity"`
	OpeningHours OpeningHours `json:"opening_hours"`
	Status       string       `json:"status"`
	InternalID   int64        `json:"-"`
}

// Repo reads lots. ByPublicID returns ErrNotFound for an unknown id.
type Repo interface {
	List(ctx context.Context) ([]Summary, error)
	ByPublicID(ctx context.Context, id uuid.UUID) (Lot, error)
}

// Service serves the lot catalogue.
type Service struct{ repo Repo }

// NewService returns a Service reading from repo.
func NewService(repo Repo) *Service { return &Service{repo: repo} }

// List returns every lot. M2 has one, so there is no paging.
func (s *Service) List(ctx context.Context) ([]Summary, error) { return s.repo.List(ctx) }

// Get returns the lot with the public id, with MapURL filled in.
func (s *Service) Get(ctx context.Context, id uuid.UUID) (Lot, error) {
	l, err := s.repo.ByPublicID(ctx, id)
	if err != nil {
		return Lot{}, err
	}
	l.MapURL = MapURL(l.Latitude, l.Longitude, l.Address)
	return l, nil
}
