package gate

import (
	"context"
	"fmt"
	"io"
	"log/slog"

	"github.com/XYZLZ/MyPark/apps/server/internal/domain/authz"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/audit"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/availability"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/tariffs"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/clock"
)

// Deps are the Service's dependencies. Every field is required.
type Deps struct {
	Repo    Repo
	Clock   clock.Clock
	Log     *slog.Logger
	Tariffs tariffs.Current               // version captured at entry (M2C-2)
	Holds   availability.ReservationHolds // spaces held for reservations
	Audit   audit.Recorder
	// Reservations answers reservation QRs (design 11.1).
	Reservations Reservations
	// CardPepper keys HashCardUID (M2C-26).
	CardPepper []byte
	// Rand is crypto/rand.Reader in production: ticket codes are secrets.
	Rand io.Reader
}

// Service is the gate use cases behind the device API.
type Service struct {
	repo         Repo
	clock        clock.Clock
	log          *slog.Logger
	tariffs      tariffs.Current
	holds        availability.ReservationHolds
	audit        audit.Recorder
	reservations Reservations
	cardPepper   []byte
	rand         io.Reader
}

var _ Handler = (*Service)(nil)

// NewService returns a Service over d.
func NewService(d Deps) *Service {
	return &Service{
		repo: d.Repo, clock: d.Clock, log: d.Log, tariffs: d.Tariffs, holds: d.Holds,
		audit: d.Audit, reservations: d.Reservations, cardPepper: d.CardPepper, rand: d.Rand,
	}
}

type handleFunc func(ctx context.Context, d authz.Device, ev Event) (Result, error)

// Handle implements Handler: it dispatches ev by type.
func (s *Service) Handle(ctx context.Context, d authz.Device, ev Event) (Result, error) {
	h := s.handlerFor(ev.Type)
	if h == nil {
		return Result{}, fmt.Errorf("gate: no handler for event type %q", ev.Type)
	}
	return h(ctx, d, ev)
}

func (s *Service) handlerFor(t EventType) handleFunc {
	switch t {
	case EventHeartbeat, EventPodiumDetected:
		return ack
	case EventEntryCredential:
		return s.entry
	case EventBarrierPassed:
		return s.pass
	case EventPayScan:
		return s.payScan
	case EventLostTicket:
		return s.lostTicket
	case EventCashPaid:
		return s.cashPaid
	case EventExitCredential:
		return s.exitCredential
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
