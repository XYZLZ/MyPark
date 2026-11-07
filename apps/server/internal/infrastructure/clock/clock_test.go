package clock_test

import (
	"testing"
	"time"

	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/clock"
)

func TestSystem_NowIsUTC(t *testing.T) {
	var c clock.Clock = clock.System{}
	n := c.Now()
	if n.Location() != time.UTC {
		t.Errorf("location = %v", n.Location())
	}
	if time.Since(n) > time.Minute {
		t.Error("not near now")
	}
}
