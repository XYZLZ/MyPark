// Package availability computes and serves lot availability. This file holds
// its ports; domain.go holds the admission rule. Compute and the service
// arrive with the snapshot.
package availability

import (
	"context"
	"time"
)

// ReservationHolds is what the availability rules need from reservations
// (design §4.1, M2C-1). M3 swaps the adapter.
type ReservationHolds interface {
	// EffectiveHolds is the number of spaces held for reservations at now.
	EffectiveHolds(ctx context.Context, lotID int64, now time.Time) (int, error)
	// UpcomingArrivals is the number of reservations arriving within the
	// window starting at now.
	UpcomingArrivals(ctx context.Context, lotID int64, now time.Time, within time.Duration) (int, error)
}

// NoReservations is the M2 ReservationHolds: nothing is ever reserved.
type NoReservations struct{}

// EffectiveHolds returns 0.
func (NoReservations) EffectiveHolds(context.Context, int64, time.Time) (int, error) { return 0, nil }

// UpcomingArrivals returns 0.
func (NoReservations) UpcomingArrivals(context.Context, int64, time.Time, time.Duration) (int, error) {
	return 0, nil
}
