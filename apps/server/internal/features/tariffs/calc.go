package tariffs

import "time"

const daySeconds = 86400

// AmountDue returns what the stay owes at now, in centavos, and the
// breakdown that explains it. It is design §12.2 amountDue: whole seconds by
// floor, ceilDiv for additional hours, and the overstay branch keyed off
// ExitDeadline rather than Paid (M2C-35).
func AmountDue(t Tariff, s Stay, now time.Time) (int64, Breakdown) {
	d := wholeSeconds(now.Sub(s.EnteredAt))
	bd := Breakdown{PaidCents: s.PaidCents, TariffEffectiveFrom: t.EffectiveFrom}
	bd.setDuration(d)

	switch {
	case s.Kind == KindEmployeeCard:
		bd.Kind = BreakdownEmployee
		return 0, bd

	case s.Paid && !s.ExitDeadline.IsZero() && !now.After(s.ExitDeadline):
		bd.Kind = BreakdownPaid
		return 0, bd

	case !s.ExitDeadline.IsZero() && now.After(s.ExitDeadline):
		return overstayDue(t, s, now, d, bd)
	}

	var fee int64
	switch {
	case s.LostTicket:
		bd.Kind = BreakdownLostTicket
		fee = lostFee(t, d)
	case d <= graceSeconds(t):
		bd.Kind = BreakdownGrace
	default:
		bd.Kind = BreakdownTime
		fee = timeFee(t, d)
	}
	return max(0, fee-s.PaidCents), bd
}

// overstayDue bills the segment since the latest payment with no grace,
// floored at one additional-hour unit; an uncovered lost penalty wins when it
// is larger (O13).
func overstayDue(t Tariff, s Stay, now time.Time, d int64, bd Breakdown) (int64, Breakdown) {
	seg := wholeSeconds(now.Sub(s.PaidAt))
	due := max(t.AdditionalHour, segmentFee(t, seg))

	if s.LostTicket {
		if lostDue := lostFee(t, d) - s.PaidCents; lostDue > due {
			bd.Kind = BreakdownLostTicket
			return lostDue, bd
		}
	}

	billedFrom := s.PaidAt
	bd.Kind = BreakdownOverstay
	bd.BilledFrom = &billedFrom
	bd.setDuration(seg)
	return due, bd
}

func (b *Breakdown) setDuration(secs int64) {
	b.DurationSeconds = secs
	b.FullBlocks = secs / daySeconds
	b.RemainderSeconds = secs - b.FullBlocks*daySeconds
}

// wholeSeconds floors a duration to whole seconds, clamping a negative one
// (clock skew) to zero.
func wholeSeconds(d time.Duration) int64 {
	return max(0, int64(d/time.Second))
}

func graceSeconds(t Tariff) int64 { return int64(t.ExitGrace / time.Second) }

func ceilDiv(a, b int64) int64 { return (a + b - 1) / b }

// blockFee prices a partial day of r seconds, capped at the daily cap.
func blockFee(t Tariff, r int64) int64 {
	if r == 0 {
		return 0
	}
	return min(t.DailyCap, t.FirstHour+ceilDiv(max(r-3600, 0), 3600)*t.AdditionalHour)
}

// timeFee is the ticket charge; the grace covers the total duration only.
func timeFee(t Tariff, d int64) int64 {
	if d <= graceSeconds(t) {
		return 0
	}
	return segmentFee(t, d)
}

// lostFee replaces the first 24 h block with the lost-ticket fee and ignores
// the grace (M2C-33).
func lostFee(t Tariff, d int64) int64 {
	if d < daySeconds {
		return t.LostTicket
	}
	full := d / daySeconds
	return t.LostTicket + (full-1)*t.DailyCap + blockFee(t, d-full*daySeconds)
}

// segmentFee prices s seconds in 24 h blocks with no grace (M2C-35).
func segmentFee(t Tariff, s int64) int64 {
	full := s / daySeconds
	return full*t.DailyCap + blockFee(t, s-full*daySeconds)
}
