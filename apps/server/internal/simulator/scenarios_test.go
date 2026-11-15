package simulator

import (
	"math/rand/v2"
	"slices"
	"testing"
	"time"
)

func TestArrivalsPerHour_Table(t *testing.T) {
	cases := []struct {
		s    Scenario
		hour int
		want float64
	}{
		{ScenarioWeekday, 0, 2}, {ScenarioWeekday, 6, 2}, {ScenarioWeekday, 7, 30}, {ScenarioWeekday, 8, 60},
		{ScenarioWeekday, 9, 45}, {ScenarioWeekday, 10, 30}, {ScenarioWeekday, 11, 30}, {ScenarioWeekday, 12, 40},
		{ScenarioWeekday, 13, 40}, {ScenarioWeekday, 14, 30}, {ScenarioWeekday, 16, 30}, {ScenarioWeekday, 17, 45},
		{ScenarioWeekday, 18, 30}, {ScenarioWeekday, 19, 10}, {ScenarioWeekday, 21, 10}, {ScenarioWeekday, 22, 4},
		{ScenarioWeekday, 23, 4},
		{ScenarioWeekendPeak, 0, 3}, {ScenarioWeekendPeak, 8, 3}, {ScenarioWeekendPeak, 9, 40},
		{ScenarioWeekendPeak, 10, 90}, {ScenarioWeekendPeak, 16, 90}, {ScenarioWeekendPeak, 17, 50},
		{ScenarioWeekendPeak, 19, 50}, {ScenarioWeekendPeak, 20, 15}, {ScenarioWeekendPeak, 23, 15},
		{ScenarioSensorDrift, 8, 60}, // as weekday
		{ScenarioFillToFull, 0, 600}, {ScenarioFillToFull, 13, 600}, {ScenarioFillToFull, 23, 600},
		{ScenarioIdle, 8, 0}, {ScenarioReservationArrivals, 8, 0}, {ScenarioNoShow, 8, 0},
	}
	for _, c := range cases {
		if got := ArrivalsPerHour(c.s, c.hour); got != c.want {
			t.Errorf("ArrivalsPerHour(%s, %d) = %v, want %v", c.s, c.hour, got, c.want)
		}
	}
}

func TestDwell_ClippedAndMedian(t *testing.T) {
	for _, c := range []struct {
		s      Scenario
		median time.Duration
	}{{ScenarioWeekday, 90 * time.Minute}, {ScenarioWeekendPeak, 120 * time.Minute}} {
		r := rand.New(rand.NewPCG(1, 1^pcgStream))
		draws := make([]time.Duration, 20000)
		for i := range draws {
			d := Dwell(r, c.s)
			if d < 3*time.Minute || d > 10*time.Hour {
				t.Fatalf("%s dwell %v outside [3m, 10h]", c.s, d)
			}
			draws[i] = d
		}
		slices.Sort(draws)
		if got := draws[len(draws)/2]; got < c.median-6*time.Minute || got > c.median+6*time.Minute {
			t.Errorf("%s median dwell = %v, want %v ± 6m", c.s, got, c.median)
		}
	}
}

func TestPoisson_RateSanity(t *testing.T) {
	r := rand.New(rand.NewPCG(7, 7^pcgStream))
	const hours = 200
	total := 0
	for day := range hours {
		from := time.Date(2026, 11, 1, 8, 0, 0, 0, lotZone).AddDate(0, 0, day)
		for at := nextArrival(r, ScenarioWeekday, from); at.Before(from.Add(time.Hour)); at = nextArrival(r, ScenarioWeekday, at) {
			total++
		}
	}
	if mean := float64(total) / hours; mean < 57 || mean > 63 {
		t.Errorf("mean arrivals in the 08:00 hour = %.1f, want 60 ± 3", mean)
	}
}

func TestNextArrival_CrossesHourBoundaries(t *testing.T) {
	// 06:59 weekday is a rate-2 hour; the next hour is rate 30, so arrivals
	// after 07:00 are far denser than before it.
	r := rand.New(rand.NewPCG(3, 3^pcgStream))
	from := time.Date(2026, 11, 2, 0, 0, 0, 0, lotZone)
	var before, after int
	for at := nextArrival(r, ScenarioWeekday, from); at.Before(from.Add(14 * time.Hour)); at = nextArrival(r, ScenarioWeekday, at) {
		if at.Before(from.Add(7 * time.Hour)) {
			before++
		} else {
			after++
		}
	}
	if before > 30 || after < 100 {
		t.Errorf("arrivals before 07:00 = %d, 07:00-14:00 = %d", before, after)
	}
}
