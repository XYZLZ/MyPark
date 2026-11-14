package testsupport

import (
	"time"

	"github.com/XYZLZ/MyPark/apps/server/internal/testsupport/fakeclock"
)

// FakeClock is the manually advanced test clock; see package fakeclock. Tests
// in packages that testsupport itself imports must use fakeclock directly.
type FakeClock = fakeclock.Clock

// NewFakeClock returns a clock fixed at t.
func NewFakeClock(t time.Time) *FakeClock { return fakeclock.New(t) }
