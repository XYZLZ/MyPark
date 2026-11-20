package simulator

import (
	"math"
	"math/rand/v2"
	"time"
)

// Scenario is a named load profile (design §13.2).
type Scenario string

const (
	ScenarioIdle                Scenario = "idle"
	ScenarioWeekday             Scenario = "weekday"
	ScenarioWeekendPeak         Scenario = "weekend_peak"
	ScenarioFillToFull          Scenario = "fill_to_full"
	ScenarioSensorDrift         Scenario = "sensor_drift"
	ScenarioReservationArrivals Scenario = "reservation_arrivals"
	ScenarioNoShow              Scenario = "no_show"
)

// lotZone is the pilot lot's time zone. Santo Domingo has no daylight saving,
// so a fixed offset is exact and needs no tz database.
var lotZone = time.FixedZone("America/Santo_Domingo", -4*60*60)

const (
	minDwell = 3 * time.Minute
	maxDwell = 10 * time.Hour
	// Employees: cards EMP-001..EMP-030 arrive between 07:30 and 08:30 and
	// stay 8-9 hours.
	employeeCount = 30
	// loseTicketP and payLateP are the weekday odds per car.
	loseTicketP = 0.01
	payLateP    = 0.03
)

// weekdayRates and weekendRates are the Poisson arrivals per virtual hour,
// indexed by hour of day (design §13.2).
var weekdayRates = [24]float64{
	2, 2, 2, 2, 2, 2, 2, 30, 60, 45, 30, 30, 40, 40, 30, 30, 30, 45, 30, 10, 10, 10, 4, 4,
}

var weekendRates = [24]float64{
	3, 3, 3, 3, 3, 3, 3, 3, 3, 40, 90, 90, 90, 90, 90, 90, 90, 50, 50, 50, 15, 15, 15, 15,
}

// followsWeekday reports whether s shares the weekday profile.
func (s Scenario) followsWeekday() bool {
	switch s {
	case ScenarioWeekday, ScenarioSensorDrift, ScenarioReservationArrivals, ScenarioNoShow:
		return true
	}
	return false
}

// ReservedPerHour is the expected reserved cars per virtual hour (M3B-58),
// on top of the drive-up traffic. A reserved car books against the server.
func ReservedPerHour(s Scenario) float64 {
	switch s {
	case ScenarioReservationArrivals:
		return 8
	case ScenarioNoShow:
		return 4
	}
	return 0
}

// ArrivalsPerHour is the expected arrivals in the given hour of day (0-23).
func ArrivalsPerHour(s Scenario, hour int) float64 {
	switch {
	case s.followsWeekday():
		return weekdayRates[hour]
	case s == ScenarioWeekendPeak:
		return weekendRates[hour]
	case s == ScenarioFillToFull:
		return 600
	}
	return 0
}

// Dwell draws how long a car stays: log-normal, clipped to [3 min, 10 h]. The
// weekend profile has a longer median and a tighter spread.
func Dwell(r *rand.Rand, s Scenario) time.Duration {
	median, sigma := 90*time.Minute, 0.8
	if s == ScenarioWeekendPeak {
		median, sigma = 120*time.Minute, 0.7
	}
	d := time.Duration(float64(median) * math.Exp(sigma*r.NormFloat64()))
	return min(max(d, minDwell), maxDwell)
}

// nextArrival returns the next arrival after from: exponential gaps at the
// rate of the hour they start in, redrawn at each hour boundary (the rate is
// piecewise constant, so thinning at the boundary is exact). It must only be
// called for a scenario that has arrivals.
func nextArrival(r *rand.Rand, s Scenario, from time.Time) time.Time {
	return nextAt(r, func(hour int) float64 { return ArrivalsPerHour(s, hour) }, from)
}

// nextReserved is nextArrival for the scenario's reserved cars.
func nextReserved(r *rand.Rand, s Scenario, from time.Time) time.Time {
	return nextAt(r, func(int) float64 { return ReservedPerHour(s) }, from)
}

func nextAt(r *rand.Rand, perHour func(hour int) float64, from time.Time) time.Time {
	for {
		local := from.In(lotZone)
		boundary := time.Date(local.Year(), local.Month(), local.Day(), local.Hour()+1, 0, 0, 0, lotZone)
		rate := perHour(local.Hour())
		if rate > 0 {
			gap := time.Duration(r.ExpFloat64() / rate * float64(time.Hour))
			if at := from.Add(gap); at.Before(boundary) {
				return at
			}
		}
		from = boundary
	}
}
