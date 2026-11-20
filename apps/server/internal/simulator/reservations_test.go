package simulator

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/XYZLZ/MyPark/apps/server/internal/domain/secret"
)

// fakeDrivers is the driver API of a test: it records "step:driver" calls and
// refuses Create per driver with createErr.
type fakeDrivers struct {
	mu        sync.Mutex
	calls     []string
	arrivals  []time.Time
	loginErr  error
	createErr map[int]error
	payStatus string
}

func (f *fakeDrivers) record(step string, n int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, fmt.Sprintf("%s:%d", step, n))
}

func (f *fakeDrivers) trace() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls)
}

func (f *fakeDrivers) Login(_ context.Context, n int) error { f.record("login", n); return f.loginErr }

func (f *fakeDrivers) Vehicle(_ context.Context, n int) (uuid.UUID, error) {
	f.record("vehicle", n)
	return uuid.UUID{0: 0xAA, 15: byte(n)}, nil
}

func (f *fakeDrivers) Quote(_ context.Context, n int, at time.Time) (Quote, error) {
	f.record("quote", n)
	return Quote{ArrivalAt: at}, nil
}

func (f *fakeDrivers) Create(_ context.Context, n int, at time.Time, _ uuid.UUID) (Reservation, error) {
	f.record("create", n)
	f.mu.Lock()
	f.arrivals = append(f.arrivals, at)
	f.mu.Unlock()
	if err := f.createErr[n]; err != nil {
		return Reservation{}, err
	}
	return Reservation{ID: uuid.UUID{15: byte(n)}, Status: "pending_payment"}, nil
}

func (f *fakeDrivers) Pay(_ context.Context, n int, _ uuid.UUID, key string) (string, error) {
	f.record("pay", n)
	if key == "" {
		return "", errors.New("no idempotency key")
	}
	if f.payStatus != "" {
		return f.payStatus, nil
	}
	return "confirmed", nil
}

func (f *fakeDrivers) QR(_ context.Context, n int, _ uuid.UUID) (secret.String, error) {
	f.record("qr", n)
	return secret.String(strings.Repeat(string(rune('a'+n)), 86)), nil
}

func refused(code string) error { return &DriverAPIError{Status: 409, Code: code} }

func TestReserver_BooksAndPays(t *testing.T) {
	now := time.Date(2026, 12, 1, 15, 2, 30, 500, time.UTC)
	api := &fakeDrivers{createErr: map[int]error{1: refused("reservation.blocked_no_shows")}}
	res := NewReserver(api, func() time.Time { return now }, nil)

	// Driver 1 is blocked: it is marked and the booking moves on to driver 2.
	car, ok := res.Book(t.Context(), ScenarioReservationArrivals)
	if !ok || car.Cred != CredReservationQR || car.QR.Reveal() != strings.Repeat("c", 86) {
		t.Fatalf("Book = %+v, %v; want a QR car of driver 2", car, ok)
	}
	want := []string{"vehicle:1", "quote:1", "create:1", "vehicle:2", "quote:2", "create:2", "pay:2", "qr:2"}
	if got := api.trace(); !slices.Equal(got, want) {
		t.Errorf("calls\n got  %v\n want %v", got, want)
	}
	for _, at := range api.arrivals {
		if !at.Equal(time.Date(2026, 12, 1, 15, 5, 0, 0, time.UTC)) {
			t.Errorf("arrival_at = %v, want ceil5 of the server's now (15:05)", at)
		}
	}

	// Next Book: round-robin gives driver 3; blocked driver 1 is not retried.
	api.calls = nil
	if _, ok := res.Book(t.Context(), ScenarioReservationArrivals); !ok {
		t.Fatal("second Book failed")
	}
	if got := api.trace(); got[0] != "vehicle:3" {
		t.Errorf("second Book started with %v, want driver 3", got)
	}
	// Past the block, driver 1 is tried again.
	now = now.Add(blockedFor + time.Minute)
	if res.isSkipped(1) {
		t.Error("driver 1 is still skipped after the block ended")
	}
}

func TestReserver_NoShowStopsAfterPayAndUsesItsPool(t *testing.T) {
	api := &fakeDrivers{}
	res := NewReserver(api, func() time.Time { return t0 }, nil)
	car, ok := res.Book(t.Context(), ScenarioNoShow)
	if !ok || car.QR != "" {
		t.Fatalf("Book = %+v, %v; want a car without a QR", car, ok)
	}
	want := []string{"vehicle:17", "quote:17", "create:17", "pay:17"}
	if got := api.trace(); !slices.Equal(got, want) {
		t.Errorf("calls\n got  %v\n want %v", got, want)
	}
}

func TestReserver_AllSkippedGivesUpWithoutSpinning(t *testing.T) {
	api := &fakeDrivers{createErr: map[int]error{}}
	for n := PoolNoShow[0]; n <= PoolNoShow[1]; n++ {
		api.createErr[n] = refused("reservation.active_exists")
	}
	res := NewReserver(api, func() time.Time { return t0 }, nil)
	if _, ok := res.Book(t.Context(), ScenarioNoShow); ok {
		t.Fatal("Book succeeded with every driver busy")
	}
	creates := 0
	for _, c := range api.trace() {
		if strings.HasPrefix(c, "create:") {
			creates++
		}
	}
	if creates != 4 {
		t.Errorf("%d creates, want one per driver of the pool (4)", creates)
	}
	api.calls = nil
	if _, ok := res.Book(t.Context(), ScenarioNoShow); ok || len(api.trace()) != 0 {
		t.Errorf("a second Book called %v while every driver is skipped", api.trace())
	}
}

func TestReserver_PendingPaymentGivesNoQRCar(t *testing.T) {
	api := &fakeDrivers{payStatus: "pending"}
	res := NewReserver(api, func() time.Time { return t0 }, nil)
	if car, ok := res.Book(t.Context(), ScenarioReservationArrivals); ok {
		t.Errorf("Book = %+v, want no car while the payment is pending", car)
	}
	if slices.Contains(api.trace(), "qr:1") {
		t.Error("the QR was fetched for an unconfirmed reservation")
	}
}
