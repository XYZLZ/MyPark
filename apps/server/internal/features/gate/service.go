package gate

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/XYZLZ/MyPark/apps/server/internal/domain/authz"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/clock"
)

// ErrUnhandledEvent means no handler exists yet for the event's type or
// direction. The device API answers 500 and stores nothing, so the device
// retries once the slice that handles it has landed.
var ErrUnhandledEvent = errors.New("gate: event type not handled")

// Deps are the Service's dependencies.
type Deps struct {
	Repo  Repo
	Clock clock.Clock
	Log   *slog.Logger
}

// Service is the gate use cases behind the device API.
type Service struct {
	repo  Repo
	clock clock.Clock
	log   *slog.Logger
}

var _ Handler = (*Service)(nil)

// NewService returns a Service over d.
func NewService(d Deps) *Service {
	return &Service{repo: d.Repo, clock: d.Clock, log: d.Log}
}

type handleFunc func(ctx context.Context, d authz.Device, ev Event) (Result, error)

// Handle implements Handler: it dispatches ev by type.
func (s *Service) Handle(ctx context.Context, d authz.Device, ev Event) (Result, error) {
	h := s.handlerFor(ev.Type)
	if h == nil {
		return Result{}, fmt.Errorf("%w: %s", ErrUnhandledEvent, ev.Type)
	}
	return h(ctx, d, ev)
}

func (s *Service) handlerFor(t EventType) handleFunc {
	switch t {
	case EventHeartbeat, EventPodiumDetected:
		return ack
	case EventSensorChanged:
		return s.sensor
	default:
		return nil
	}
}

// ack answers an event that changes nothing: a heartbeat, or a podium
// detection (the podium only wakes the dispenser).
func ack(context.Context, authz.Device, Event) (Result, error) {
	return Result{Result: OutcomeAck}, nil
}
