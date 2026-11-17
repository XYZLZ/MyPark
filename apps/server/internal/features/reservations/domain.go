// Package reservations holds the reservation domain: arrival rules, the hold
// window, slot loads, the reservable cap and the status state machine.
package reservations

import (
	"errors"
	"fmt"
	"time"

	"github.com/XYZLZ/MyPark/apps/server/internal/domain/errcodes"
)

const slotStep = 5 * time.Minute

// ErrPolicyUnsupported is returned when the active policy uses values the
// Phase 1 implementation cannot honour (M3B-48).
var ErrPolicyUnsupported = errors.New("reservation policy unsupported in phase 1")

// Policy is one version of the reservation policy (spec §5.3).
type Policy struct {
	VersionID          int64
	MaxReservablePct   int
	BookingHorizonDays int
	ArrivalGraceMin    int
	EarlyArrivalMin    int
	PaymentTimeoutMin  int
	MaxActivePerUser   int
	SlotMin            int
	NoShowThreshold    int
	NoShowWindowDays   int
	NoShowBlockDays    int
}

// Ceil5 returns the next 5-minute UTC epoch boundary, or t when it is already
// aligned. Sub-second parts count as unaligned. The result is in UTC.
func Ceil5(t time.Time) time.Time {
	t = t.UTC()
	floor := t.Truncate(slotStep)
	if floor.Equal(t) {
		return floor
	}
	return floor.Add(slotStep)
}

// NormalizeArrival rounds raw up to the 5-minute grid and validates it against
// now and the booking horizon. The returned code is empty when it is valid.
func NormalizeArrival(raw, now time.Time, horizonDays int) (time.Time, errcodes.Code) {
	arrival := Ceil5(raw)
	if arrival.Before(Ceil5(now)) {
		return arrival, errcodes.ReservationArrivalInPast
	}
	if arrival.After(now.Add(time.Duration(horizonDays) * 24 * time.Hour)) {
		return arrival, errcodes.ReservationBeyondHorizon
	}
	return arrival, ""
}

// Window is a half-open time range [Start, End).
type Window struct{ Start, End time.Time }

// Contains reports whether Start <= t < End.
func (w Window) Contains(t time.Time) bool {
	return !t.Before(w.Start) && t.Before(w.End)
}

// HoldWindow is [arrival - early, arrival + grace).
func HoldWindow(arrival time.Time, p Policy) Window {
	return Window{
		Start: arrival.Add(-time.Duration(p.EarlyArrivalMin) * time.Minute),
		End:   arrival.Add(time.Duration(p.ArrivalGraceMin) * time.Minute),
	}
}

// Slots lists the 5-minute slot starts in [Start, End).
func Slots(w Window) []time.Time {
	var out []time.Time
	for s := w.Start; s.Before(w.End); s = s.Add(slotStep) {
		out = append(out, s)
	}
	return out
}

// Cap is floor(pct * effectiveCapacity / 100), or 0 for a non-positive capacity.
func Cap(maxReservablePct, effectiveCapacity int) int {
	if effectiveCapacity <= 0 || maxReservablePct <= 0 {
		return 0
	}
	return maxReservablePct * effectiveCapacity / 100
}

// CapReached reports whether any slot already holds cap reservations.
func CapReached(held []int, cap int) bool {
	for _, h := range held {
		if h >= cap {
			return true
		}
	}
	return false
}

// CheckPhase1 rejects policies the Phase 1 implementation cannot honour (M3B-48).
func CheckPhase1(p Policy) error {
	switch {
	case p.MaxActivePerUser != 1:
		return fmt.Errorf("%w: MaxActivePerUser=%d, want 1", ErrPolicyUnsupported, p.MaxActivePerUser)
	case p.SlotMin != 5:
		return fmt.Errorf("%w: SlotMin=%d, want 5", ErrPolicyUnsupported, p.SlotMin)
	case p.EarlyArrivalMin%5 != 0:
		return fmt.Errorf("%w: EarlyArrivalMin=%d, want a multiple of 5", ErrPolicyUnsupported, p.EarlyArrivalMin)
	case p.ArrivalGraceMin%5 != 0:
		return fmt.Errorf("%w: ArrivalGraceMin=%d, want a multiple of 5", ErrPolicyUnsupported, p.ArrivalGraceMin)
	}
	return nil
}
