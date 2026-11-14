// Package tariffs holds the parking tariff and the pure fee calculator
// (design §12). All money is integer centavos (DOP); durations are whole
// seconds measured between injected times.
package tariffs

import "time"

// Tariff is one tariff version of a lot (design §5.1).
type Tariff struct {
	EffectiveFrom   time.Time
	FirstHour       int64
	AdditionalHour  int64
	DailyCap        int64
	LostTicket      int64
	ReservationFee  int64
	ExitGrace       time.Duration
	PostPaymentExit time.Duration
}

// DefaultTariff returns the seeded pilot tariff (design §5.1 and §12.3).
func DefaultTariff() Tariff {
	return Tariff{
		EffectiveFrom:   time.Date(2026, 11, 1, 0, 0, 0, 0, time.FixedZone("", -4*60*60)),
		FirstHour:       5000,
		AdditionalHour:  2500,
		DailyCap:        40000,
		LostTicket:      50000,
		ReservationFee:  15000,
		ExitGrace:       10 * time.Minute,
		PostPaymentExit: 15 * time.Minute,
	}
}

// StayKind is how the stay entered the lot.
type StayKind string

// Stay kinds.
const (
	KindTicket       StayKind = "ticket"
	KindEmployeeCard StayKind = "employee_card"
	KindReservation  StayKind = "reservation"
)

// Stay is the calculator's view of a stay.
type Stay struct {
	Kind       StayKind
	EnteredAt  time.Time
	Paid       bool // status is paid
	LostTicket bool
	PaidCents  int64     // total succeeded payments
	PaidAt     time.Time // latest succeeded payment; zero means none
	// ExitDeadline stays set after the lazy paid -> active transition;
	// zero means never paid.
	ExitDeadline time.Time
}

// BreakdownKind names the rule that decided the amount.
type BreakdownKind string

// Breakdown kinds (design §7.2).
const (
	BreakdownTime       BreakdownKind = "time"
	BreakdownGrace      BreakdownKind = "grace"
	BreakdownLostTicket BreakdownKind = "lost_ticket"
	BreakdownEmployee   BreakdownKind = "employee"
	BreakdownOverstay   BreakdownKind = "overstay"
	BreakdownPaid       BreakdownKind = "paid"
)

// Breakdown explains an amount (design §7.2). For an overstay the durations
// describe the segment since BilledFrom (the latest payment); for every other
// kind BilledFrom is nil and the durations describe the whole stay.
type Breakdown struct {
	Kind                BreakdownKind `json:"kind"`
	DurationSeconds     int64         `json:"duration_seconds"`
	FullBlocks          int64         `json:"full_blocks"`
	RemainderSeconds    int64         `json:"remainder_seconds"`
	BilledFrom          *time.Time    `json:"billed_from"`
	PaidCents           int64         `json:"paid_cents"`
	TariffEffectiveFrom time.Time     `json:"tariff_effective_from"`
}
