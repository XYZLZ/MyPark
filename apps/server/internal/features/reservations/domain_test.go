package reservations

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/XYZLZ/MyPark/apps/server/internal/domain/errcodes"
)

func mustParse(t *testing.T, s string) time.Time {
	t.Helper()
	v, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func defaultPolicy() Policy {
	return Policy{
		VersionID: 1, MaxReservablePct: 30, BookingHorizonDays: 7, ArrivalGraceMin: 20,
		EarlyArrivalMin: 15, PaymentTimeoutMin: 10, MaxActivePerUser: 1, SlotMin: 5,
		NoShowThreshold: 3, NoShowWindowDays: 30, NoShowBlockDays: 7,
	}
}

func TestCeil5(t *testing.T) {
	tests := []struct{ name, in, want string }{
		{"aligned", "2026-12-01T15:00:00Z", "2026-12-01T15:00:00Z"},
		{"sub-second", "2026-12-01T15:00:00.001Z", "2026-12-01T15:05:00Z"},
		{"seconds", "2026-12-01T15:04:59Z", "2026-12-01T15:05:00Z"},
		{"offset", "2026-12-01T11:02:30-04:00", "2026-12-01T15:05:00Z"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Ceil5(mustParse(t, tc.in))
			if !got.Equal(mustParse(t, tc.want)) {
				t.Fatalf("got %v want %s", got, tc.want)
			}
			if got.Location() != time.UTC {
				t.Fatalf("location %v, want UTC", got.Location())
			}
		})
	}
}

func TestNormalizeArrival(t *testing.T) {
	now := mustParse(t, "2026-12-01T14:58:10Z")
	tests := []struct {
		name, raw, want string
		code            errcodes.Code
	}{
		{"rounds up", "2026-12-01T14:58:00Z", "2026-12-01T15:00:00Z", ""},
		{"past", "2026-12-01T14:55:00Z", "", errcodes.ReservationArrivalInPast},
		{"exactly ceil5(now)", "2026-12-01T15:00:00Z", "2026-12-01T15:00:00Z", ""},
		{"horizon edge ok", "2026-12-08T14:55:00Z", "2026-12-08T14:55:00Z", ""},
		{"beyond horizon", "2026-12-08T15:00:00Z", "", errcodes.ReservationBeyondHorizon},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, code := NormalizeArrival(mustParse(t, tc.raw), now, 7)
			if code != tc.code {
				t.Fatalf("code %q want %q", code, tc.code)
			}
			if tc.code == "" && !got.Equal(mustParse(t, tc.want)) {
				t.Fatalf("got %v want %s", got, tc.want)
			}
		})
	}
}

func TestNormalizeArrival_OffsetsAndFractions(t *testing.T) {
	now := mustParse(t, "2026-12-01T10:00:00Z")
	tests := []struct{ raw, want string }{
		{"2026-12-01T11:02:30.5-04:00", "2026-12-01T15:05:00Z"},
		{"2026-12-01T15:00:00.000Z", "2026-12-01T15:00:00Z"},
		{"2026-12-01T15:00:00.000001Z", "2026-12-01T15:05:00Z"},
		{"2026-12-01T17:02:59+02:00", "2026-12-01T15:05:00Z"},
	}
	for _, tc := range tests {
		got, code := NormalizeArrival(mustParse(t, tc.raw), now, 7)
		if code != "" {
			t.Fatalf("%s: unexpected code %q", tc.raw, code)
		}
		if !got.Equal(mustParse(t, tc.want)) || got.Location() != time.UTC {
			t.Fatalf("%s: got %v want %s UTC", tc.raw, got, tc.want)
		}
	}
}

func TestHoldWindow(t *testing.T) {
	arrival := mustParse(t, "2026-12-01T15:00:00Z")
	w := HoldWindow(arrival, defaultPolicy())
	if !w.Start.Equal(mustParse(t, "2026-12-01T14:45:00Z")) || !w.End.Equal(mustParse(t, "2026-12-01T15:20:00Z")) {
		t.Fatalf("window %v", w)
	}
	for in, want := range map[string]bool{
		"2026-12-01T14:45:00Z":     true,
		"2026-12-01T15:19:59.999Z": true,
		"2026-12-01T15:20:00Z":     false,
		"2026-12-01T14:44:59Z":     false,
	} {
		if got := w.Contains(mustParse(t, in)); got != want {
			t.Errorf("Contains(%s) = %v want %v", in, got, want)
		}
	}
}

func TestSlots(t *testing.T) {
	w := HoldWindow(mustParse(t, "2026-12-01T15:00:00Z"), defaultPolicy())
	slots := Slots(w)
	if len(slots) != 7 {
		t.Fatalf("got %d slots", len(slots))
	}
	for i, s := range slots {
		want := w.Start.Add(time.Duration(i) * 5 * time.Minute)
		if !s.Equal(want) || !s.Equal(Ceil5(s)) {
			t.Errorf("slot %d = %v", i, s)
		}
	}
	if !slots[6].Equal(mustParse(t, "2026-12-01T15:15:00Z")) {
		t.Errorf("last slot %v", slots[6])
	}
	if got := Slots(Window{Start: w.End, End: w.Start}); len(got) != 0 {
		t.Errorf("inverted window gave %d slots", len(got))
	}
}

func TestCap(t *testing.T) {
	tests := []struct{ pct, capacity, want int }{
		{30, 280, 84}, {30, 279, 83}, {30, 0, 0}, {0, 280, 0}, {100, 280, 280}, {30, -5, 0},
	}
	for _, tc := range tests {
		if got := Cap(tc.pct, tc.capacity); got != tc.want {
			t.Errorf("Cap(%d,%d) = %d want %d", tc.pct, tc.capacity, got, tc.want)
		}
	}
}

func TestCapReached(t *testing.T) {
	tests := []struct {
		held []int
		want bool
	}{{[]int{83, 83}, false}, {[]int{83, 84}, true}, {[]int{}, false}, {nil, false}}
	for _, tc := range tests {
		if got := CapReached(tc.held, 84); got != tc.want {
			t.Errorf("CapReached(%v) = %v want %v", tc.held, got, tc.want)
		}
	}
}

func TestCheckPhase1(t *testing.T) {
	if err := CheckPhase1(defaultPolicy()); err != nil {
		t.Fatalf("seeded policy: %v", err)
	}
	for name, mut := range map[string]func(*Policy){
		"MaxActivePerUser": func(p *Policy) { p.MaxActivePerUser = 2 },
		"SlotMin":          func(p *Policy) { p.SlotMin = 10 },
		"EarlyArrivalMin":  func(p *Policy) { p.EarlyArrivalMin = 12 },
		"ArrivalGraceMin":  func(p *Policy) { p.ArrivalGraceMin = 22 },
	} {
		p := defaultPolicy()
		mut(&p)
		err := CheckPhase1(p)
		if !errors.Is(err, ErrPolicyUnsupported) {
			t.Fatalf("%s: err = %v", name, err)
		}
		if got := err.Error(); !strings.Contains(got, name) {
			t.Errorf("%s: error %q does not name the field", name, got)
		}
	}
}
