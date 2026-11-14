package tariffs_test

import (
	"testing"
	"time"

	"github.com/XYZLZ/MyPark/apps/server/internal/features/tariffs"
)

// t0 is the entry time every table row offsets from.
var t0 = time.Date(2026, 11, 24, 8, 0, 0, 0, time.FixedZone("AST", -4*60*60))

func off(t *testing.T, d string) time.Time {
	t.Helper()
	dur, err := time.ParseDuration(d)
	if err != nil {
		t.Fatalf("bad duration %q: %v", d, err)
	}
	return t0.Add(dur)
}

func ticket() tariffs.Stay { return tariffs.Stay{Kind: tariffs.KindTicket, EnteredAt: t0} }

func lost() tariffs.Stay {
	s := ticket()
	s.LostTicket = true
	return s
}

func emp() tariffs.Stay { return tariffs.Stay{Kind: tariffs.KindEmployeeCard, EnteredAt: t0} }

// paid marks the stay as paid at t0+at with the 15 min exit window.
func paid(s tariffs.Stay, at string, cents int64) tariffs.Stay {
	dur, err := time.ParseDuration(at)
	if err != nil {
		panic(err)
	}
	s.Paid = true
	s.PaidAt = t0.Add(dur)
	s.ExitDeadline = s.PaidAt.Add(15 * time.Minute)
	s.PaidCents = cents
	return s
}

// lapsed models the lazy paid -> active transition (or a stay marked lost):
// status is no longer paid but PaidAt and ExitDeadline are kept.
func lapsed(s tariffs.Stay) tariffs.Stay {
	s.Paid = false
	return s
}

type row struct {
	id   string
	s    tariffs.Stay
	at   string
	want int64
	kind tariffs.BreakdownKind
}

// Every row of design §12.4, verbatim.
var table = []row{
	{"T1", ticket(), "0s", 0, tariffs.BreakdownGrace},
	{"T2", ticket(), "9m59s", 0, tariffs.BreakdownGrace},
	{"T3", ticket(), "10m0s", 0, tariffs.BreakdownGrace}, // grace boundary is inclusive (≤)
	{"T4", ticket(), "10m1s", 5000, tariffs.BreakdownTime},
	{"T5", ticket(), "59m59s", 5000, tariffs.BreakdownTime},
	{"T6", ticket(), "1h0m0s", 5000, tariffs.BreakdownTime}, // ceil(0) = 0
	{"T7", ticket(), "1h0m1s", 7500, tariffs.BreakdownTime},
	{"T8", ticket(), "2h0m0s", 7500, tariffs.BreakdownTime},
	{"T9", ticket(), "2h0m1s", 10000, tariffs.BreakdownTime},
	{"T10", ticket(), "14h0m0s", 37500, tariffs.BreakdownTime}, // 5000 + 13 × 2500
	{"T11", ticket(), "14h0m1s", 40000, tariffs.BreakdownTime}, // reaches the cap exactly
	{"T12", ticket(), "15h0m1s", 40000, tariffs.BreakdownTime}, // capped (42500 → 40000)
	{"T13", ticket(), "23h59m59s", 40000, tariffs.BreakdownTime},
	{"T14", ticket(), "24h0m0s", 40000, tariffs.BreakdownTime}, // one full block, remainder 0
	{"T15", ticket(), "24h0m1s", 45000, tariffs.BreakdownTime}, // remainder of 1 s pays a first hour
	{"T16", ticket(), "24h9m0s", 45000, tariffs.BreakdownTime}, // no grace on a remainder
	{"T17", ticket(), "25h0m1s", 47500, tariffs.BreakdownTime},
	{"T18", ticket(), "48h0m0s", 80000, tariffs.BreakdownTime},                               // two blocks
	{"T19", ticket(), "75h0m0s", 130000, tariffs.BreakdownTime},                              // 3 × 40000 + (5000 + 2 × 2500)
	{"L1", lost(), "5m", 50000, tariffs.BreakdownLostTicket},                                 // lost fee ignores grace
	{"L2", lost(), "3h", 50000, tariffs.BreakdownLostTicket},                                 // replaces the time charge
	{"L3", lost(), "23h59m59s", 50000, tariffs.BreakdownLostTicket},                          // above the cap by design
	{"L4", lost(), "24h0m0s", 50000, tariffs.BreakdownLostTicket},                            // first block replaced, remainder 0
	{"L5", lost(), "26h0m0s", 57500, tariffs.BreakdownLostTicket},                            // 50000 + blockFee(2 h)
	{"L6", lost(), "50h0m0s", 97500, tariffs.BreakdownLostTicket},                            // 50000 + 40000 + 7500
	{"L7", lapsed(paid(lost(), "1h30m", 7500)), "1h40m", 42500, tariffs.BreakdownLostTicket}, // fee_lost − paid, inside the window
	{"E1", emp(), "30h", 0, tariffs.BreakdownEmployee},
	{"O1", paid(ticket(), "1h30m", 7500), "1h50m", 5000, tariffs.BreakdownOverstay}, // segment 20 m: blockFee(1200) = 5000 (no grace)
	{"O2", paid(ticket(), "1h30m", 7500), "3h10m", 7500, tariffs.BreakdownOverstay}, // segment 1 h 40 m: 5000 + 1 × 2500 (RD$75)
	{"O3", paid(ticket(), "20h", 40000), "23h", 10000, tariffs.BreakdownOverstay},   // segment 3 h: 5000 + 2 × 2500
	{"O4", paid(ticket(), "1h30m", 7500), "1h44m", 0, tariffs.BreakdownPaid},
	{"O5", paid(ticket(), "1h30m", 7500), "1h45m0s", 0, tariffs.BreakdownPaid},              // deadline is inclusive (≤)
	{"O6", paid(lost(), "2h", 50000), "2h20m", 5000, tariffs.BreakdownOverstay},             // segment 20 m; lostFee − paid = 0
	{"O7", paid(ticket(), "1h50m", 12500), "2h20m", 5000, tariffs.BreakdownOverstay},        // payments 7500 @1h30 + 5000 @1h50; segment 30 m
	{"O8", lapsed(paid(ticket(), "1h30m", 7500)), "1h50m", 5000, tariffs.BreakdownOverstay}, // calculator half: active, deadline 1h45 kept (flow half in S9.2)
	{"O9", paid(ticket(), "1h30m", 7500), "1h45m1s", 5000, tariffs.BreakdownOverstay},       // segment 15 m 01 s; floor 2500 does not bind
	{"O10", paid(ticket(), "20h", 40000), "40h", 40000, tariffs.BreakdownOverstay},          // segment 20 h: 52500 → cap 40000
	{"O11", paid(ticket(), "20h", 40000), "46h", 47500, tariffs.BreakdownOverstay},          // segment 26 h: 40000 + blockFee(2 h)
	{"O12", paid(ticket(), "1h50m", 12500), "2h45m", 5000, tariffs.BreakdownOverstay},       // segment 55 m from the second payment
	{"O13", lapsed(paid(lost(), "1h30m", 7500)), "2h", 42500, tariffs.BreakdownLostTicket},  // max(2500, 5000, 50000 − 7500)
}

func TestAmountDue_Table(t *testing.T) {
	const designRows = 40 // T1–T19, L1–L7, E1, O1–O13
	if len(table) != designRows {
		t.Fatalf("table has %d rows, design §12.4 has %d", len(table), designRows)
	}
	tf := tariffs.DefaultTariff()
	for _, c := range table {
		t.Run(c.id, func(t *testing.T) {
			got, bd := tariffs.AmountDue(tf, c.s, off(t, c.at))
			if got != c.want {
				t.Errorf("amount = %d, want %d", got, c.want)
			}
			if bd.Kind != c.kind {
				t.Errorf("kind = %q, want %q", bd.Kind, c.kind)
			}
			if bd.PaidCents != c.s.PaidCents {
				t.Errorf("paid_cents = %d, want %d", bd.PaidCents, c.s.PaidCents)
			}
			if !bd.TariffEffectiveFrom.Equal(tf.EffectiveFrom) {
				t.Errorf("tariff_effective_from = %v, want %v", bd.TariffEffectiveFrom, tf.EffectiveFrom)
			}
		})
	}
}

func find(t *testing.T, id string) row {
	t.Helper()
	for _, c := range table {
		if c.id == id {
			return c
		}
	}
	t.Fatalf("no row %s", id)
	return row{}
}

func TestAmountDue_OverstayBilledFrom(t *testing.T) {
	tf := tariffs.DefaultTariff()
	cases := []struct {
		id      string
		from    string // "" means billed_from must be nil
		segSecs int64  // checked when > 0
	}{
		{"O1", "1h30m", 1200},
		{"O7", "1h50m", 1800},
		{"O12", "1h50m", 3300},
		{"O13", "", 0},
		{"T7", "", 0},
	}
	for _, c := range cases {
		t.Run(c.id, func(t *testing.T) {
			r := find(t, c.id)
			_, bd := tariffs.AmountDue(tf, r.s, off(t, r.at))
			if c.from == "" {
				if bd.BilledFrom != nil {
					t.Fatalf("billed_from = %v, want nil", *bd.BilledFrom)
				}
				return
			}
			if bd.BilledFrom == nil {
				t.Fatal("billed_from = nil, want set")
			}
			if want := off(t, c.from); !bd.BilledFrom.Equal(want) {
				t.Errorf("billed_from = %v, want %v", *bd.BilledFrom, want)
			}
			if bd.DurationSeconds != c.segSecs {
				t.Errorf("duration_seconds = %d, want %d", bd.DurationSeconds, c.segSecs)
			}
		})
	}
}

func TestAmountDue_BreakdownBlocks(t *testing.T) {
	tf := tariffs.DefaultTariff()
	cases := []struct {
		id                   string
		dur, full, remainder int64
	}{
		{"T19", 270000, 3, 10800},
		{"O11", 93600, 1, 7200},
		{"O13", 7200, 0, 7200}, // lost term wins: durations describe the whole stay
	}
	for _, c := range cases {
		t.Run(c.id, func(t *testing.T) {
			r := find(t, c.id)
			_, bd := tariffs.AmountDue(tf, r.s, off(t, r.at))
			if bd.DurationSeconds != c.dur || bd.FullBlocks != c.full || bd.RemainderSeconds != c.remainder {
				t.Errorf("blocks = (%d, %d, %d), want (%d, %d, %d)",
					bd.DurationSeconds, bd.FullBlocks, bd.RemainderSeconds, c.dur, c.full, c.remainder)
			}
		})
	}
}

func TestAmountDue_WholeSecondsFloor(t *testing.T) {
	// 10 m 00.999 s still floors to 600 s: inside the grace.
	got, _ := tariffs.AmountDue(tariffs.DefaultTariff(), ticket(), t0.Add(10*time.Minute+999*time.Millisecond))
	if got != 0 {
		t.Fatalf("amount = %d, want 0", got)
	}
}

func TestDefaultTariff(t *testing.T) {
	tf := tariffs.DefaultTariff()
	want := tariffs.Tariff{
		EffectiveFrom:   time.Date(2026, 11, 1, 0, 0, 0, 0, time.FixedZone("", -4*60*60)),
		FirstHour:       5000,
		AdditionalHour:  2500,
		DailyCap:        40000,
		LostTicket:      50000,
		ReservationFee:  15000,
		ExitGrace:       10 * time.Minute,
		PostPaymentExit: 15 * time.Minute,
	}
	if !tf.EffectiveFrom.Equal(want.EffectiveFrom) {
		t.Errorf("effective_from = %v, want %v", tf.EffectiveFrom, want.EffectiveFrom)
	}
	tf.EffectiveFrom = want.EffectiveFrom
	if tf != want {
		t.Errorf("DefaultTariff() = %+v, want %+v", tf, want)
	}
}

func TestAmountDue_Properties(t *testing.T) {
	tf := tariffs.DefaultTariff()
	const step = 37 * time.Second
	const until = 80 * time.Hour
	var prev int64
	for d := time.Duration(0); d <= until; d += step {
		now := t0.Add(d)
		for _, s := range []tariffs.Stay{ticket(), lost(), emp()} {
			if got, _ := tariffs.AmountDue(tf, s, now); got < 0 {
				t.Fatalf("kind %s lost=%v at %v: amount %d < 0", s.Kind, s.LostTicket, d, got)
			}
		}
		got, _ := tariffs.AmountDue(tf, ticket(), now)
		if got < prev {
			t.Fatalf("ticket fee decreased at %v: %d < %d", d, got, prev)
		}
		prev = got
		secs := int64(d / time.Second)
		if secs > int64(tf.ExitGrace/time.Second) {
			days := (secs + 86399) / 86400
			if limit := days * tf.DailyCap; got > limit {
				t.Fatalf("ticket fee at %v = %d > ceil(d/24h) × cap = %d", d, got, limit)
			}
		}
	}

	// Every overstay is billed at least one additional-hour unit (never RD$0).
	for _, base := range []tariffs.Stay{ticket(), lost()} {
		s := paid(base, "1h30m", 7500)
		for _, st := range []tariffs.Stay{s, lapsed(s)} {
			for d := time.Second; d <= 30*time.Hour; d += 61 * time.Second {
				now := st.ExitDeadline.Add(d)
				if got, _ := tariffs.AmountDue(tf, st, now); got < tf.AdditionalHour {
					t.Fatalf("overstay paid=%v lost=%v at deadline+%v: amount %d < %d",
						st.Paid, st.LostTicket, d, got, tf.AdditionalHour)
				}
			}
		}
	}
}

func TestAmountDue_OverstayFloorBinds(t *testing.T) {
	// With a free first hour the segment fee can be 0; the additional-hour
	// floor must still bill the overstay.
	tf := tariffs.DefaultTariff()
	tf.FirstHour = 0
	s := paid(ticket(), "1h30m", 7500)
	got, bd := tariffs.AmountDue(tf, s, off(t, "1h50m"))
	if got != tf.AdditionalHour || bd.Kind != tariffs.BreakdownOverstay {
		t.Fatalf("amount = %d kind %q, want %d overstay", got, bd.Kind, tf.AdditionalHour)
	}
}

func TestAmountDue_ClockSkewClampsToZero(t *testing.T) {
	// A scan stamped before the entry must not produce a negative duration.
	got, bd := tariffs.AmountDue(tariffs.DefaultTariff(), ticket(), t0.Add(-time.Minute))
	if got != 0 || bd.DurationSeconds != 0 {
		t.Fatalf("amount = %d duration = %d, want 0, 0", got, bd.DurationSeconds)
	}
}
