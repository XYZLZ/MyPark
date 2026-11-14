package availability

import (
	"context"
	"fmt"
	"time"

	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/clock"
)

// upcomingWindow is how far ahead reservations count as upcoming (FR-AVAIL).
const upcomingWindow = 2 * time.Hour

// SpaceState is one space of the public spaces payload: code and state only
// (no device or stay data, M2C-7).
type SpaceState struct {
	Code  string `json:"code"`
	State string `json:"state"`
}

// Repo reads the lot state. Counts fills every SnapshotInput field except
// Holds and UpcomingArrivals2h, which come from the reservations port.
type Repo interface {
	Counts(ctx context.Context, lotID int64) (SnapshotInput, error)
	Spaces(ctx context.Context, lotID int64) ([]SpaceState, error)
}

// Service computes the availability of a lot.
type Service struct {
	repo  Repo
	holds ReservationHolds
	clk   clock.Clock
}

// NewService returns a Service. holds supplies the reservation numbers.
func NewService(repo Repo, holds ReservationHolds, clk clock.Clock) *Service {
	return &Service{repo: repo, holds: holds, clk: clk}
}

// Snapshot returns the current availability of the lot: one query for the lot,
// its occupancy and the out-of-service count, plus the reservation numbers.
func (s *Service) Snapshot(ctx context.Context, lotID int64) (Snapshot, error) {
	in, err := s.repo.Counts(ctx, lotID)
	if err != nil {
		return Snapshot{}, err
	}
	now := s.clk.Now()
	if in.Holds, err = s.holds.EffectiveHolds(ctx, lotID, now); err != nil {
		return Snapshot{}, fmt.Errorf("availability: holds of lot %d: %w", lotID, err)
	}
	if in.UpcomingArrivals2h, err = s.holds.UpcomingArrivals(ctx, lotID, now, upcomingWindow); err != nil {
		return Snapshot{}, fmt.Errorf("availability: upcoming arrivals of lot %d: %w", lotID, err)
	}
	return Compute(in), nil
}

// LotStatus returns the public status of the lot (open, full or closed).
func (s *Service) LotStatus(ctx context.Context, lotID int64) (string, error) {
	snap, err := s.Snapshot(ctx, lotID)
	return string(snap.Status), err
}

// Spaces returns every space of the lot, ordered by code.
func (s *Service) Spaces(ctx context.Context, lotID int64) ([]SpaceState, error) {
	return s.repo.Spaces(ctx, lotID)
}
