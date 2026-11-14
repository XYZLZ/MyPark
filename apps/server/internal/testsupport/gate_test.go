package testsupport

import (
	"fmt"
	"runtime"
	"strings"
	"testing"
	"time"
)

// failRecorder captures failures instead of failing the real test.
type failRecorder struct {
	testing.TB
	failures []string
}

func (r *failRecorder) Helper() {}

func (r *failRecorder) Errorf(format string, args ...any) {
	r.failures = append(r.failures, fmt.Sprintf(format, args...))
}

// Fatalf is not captured: a fatal failure (a database read error) must reach
// the real test with its message.
func (r *failRecorder) Fatalf(format string, args ...any) {
	r.TB.Fatalf(format, args...)
}

// fatalSpy stands in for the real test: it keeps the message of a fatal
// failure and stops the goroutine as FailNow would.
type fatalSpy struct {
	testing.TB
	fatal string
}

func (s *fatalSpy) Helper() {}

func (s *fatalSpy) Fatalf(format string, args ...any) {
	s.fatal = fmt.Sprintf(format, args...)
	runtime.Goexit()
}

func (s *fatalSpy) FailNow() { runtime.Goexit() }

// A database read error inside the helper fails the test with its message,
// through failRecorder too.
func TestAssertOccupancyInvariant_ReadErrorIsReported(t *testing.T) {
	db := NewTestDB(t)
	spy := &fatalSpy{TB: t}
	done := make(chan struct{})
	go func() {
		defer close(done)
		AssertOccupancyInvariant(&failRecorder{TB: spy}, db.App, -1)
	}()
	<-done
	if !strings.Contains(spy.fatal, "read occupancy of lot -1") {
		t.Fatalf("fatal message = %q, want one naming the lot", spy.fatal)
	}
}

func TestAssertOccupancyInvariant(t *testing.T) {
	db := NewTestDB(t)
	lotID := SeedPilotLot(t, db)
	FillLot(t, db, lotID, 5, time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC))
	if _, err := db.App.Exec(t.Context(),
		`update stays set status = 'paid', paid_at = entered_at, exit_deadline_at = entered_at
		 where id = (select min(id) from stays where lot_id = $1)`, lotID); err != nil {
		t.Fatal(err)
	}

	ok := &failRecorder{TB: t}
	AssertOccupancyInvariant(ok, db.App, lotID)
	if len(ok.failures) != 0 {
		t.Fatalf("5 active/paid stays with occupied 5: %v", ok.failures)
	}

	if _, err := db.App.Exec(t.Context(), `update lot_occupancy set occupied = 6 where lot_id = $1`, lotID); err != nil {
		t.Fatal(err)
	}
	bad := &failRecorder{TB: t}
	AssertOccupancyInvariant(bad, db.App, lotID)
	if len(bad.failures) != 1 || !strings.Contains(bad.failures[0], "occupied = 6") {
		t.Fatalf("desynced counter: failures = %v, want one naming occupied = 6", bad.failures)
	}
}
