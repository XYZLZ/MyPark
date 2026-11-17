// Package gate holds the parking-lot gate domain: device event types and
// payloads, the answers devices receive and credential normalization. The
// strict decoding of a request body into these types lives in the devices
// package; a payload carries no lot id, which always comes from the
// authenticated device.
package gate

import (
	"time"

	"github.com/google/uuid"
)

// EventType is the type of a device event (design §7.2).
type EventType string

// The nine device event types.
const (
	EventPodiumDetected  EventType = "podium.vehicle_detected"
	EventEntryCredential EventType = "entry.credential_presented"
	EventBarrierPassed   EventType = "barrier.passed"
	EventPayScan         EventType = "paystation.scan"
	EventLostTicket      EventType = "paystation.lost_ticket"
	EventCashPaid        EventType = "paystation.cash_paid"
	EventExitCredential  EventType = "exit.credential_presented"
	EventSensorChanged   EventType = "sensor.space_changed"
	EventHeartbeat       EventType = "heartbeat"
)

// AllEventTypes returns the nine event types in design order.
func AllEventTypes() []EventType {
	return []EventType{
		EventPodiumDetected, EventEntryCredential, EventBarrierPassed,
		EventPayScan, EventLostTicket, EventCashPaid, EventExitCredential,
		EventSensorChanged, EventHeartbeat,
	}
}

// Credential kinds and barrier directions.
const (
	KindTicketRequest = "ticket_request"
	KindTicket        = "ticket"
	KindEmployeeCard  = "employee_card"
	KindReservationQR = "reservation_qr"
	// KindReservation is the credential_kind of a reservation stay.
	KindReservation = "reservation"

	DirectionEntry = "entry"
	DirectionExit  = "exit"
)

// EntryCredential is the payload of entry.credential_presented. Kind is
// ticket_request, employee_card or reservation_qr; Value is empty only for
// ticket_request.
type EntryCredential struct {
	Kind  string `json:"kind"`
	Value string `json:"value,omitempty"`
}

// Credential is a presented credential: ticket, employee_card or reservation_qr.
type Credential struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
}

// BarrierPassed is the payload of barrier.passed.
type BarrierPassed struct {
	Direction string    `json:"direction"`
	StayID    uuid.UUID `json:"stay_id"`
}

// PayScan is the payload of paystation.scan.
type PayScan struct {
	Credential Credential `json:"credential"`
}

// LostTicket is the payload of paystation.lost_ticket.
type LostTicket struct {
	StayID uuid.UUID `json:"stay_id"`
}

// CashPaid is the payload of paystation.cash_paid.
type CashPaid struct {
	StayID      uuid.UUID `json:"stay_id"`
	AmountCents int64     `json:"amount_cents"`
}

// ExitCredential is the payload of exit.credential_presented.
type ExitCredential struct {
	Credential Credential `json:"credential"`
}

// SensorChanged is the payload of sensor.space_changed.
type SensorChanged struct {
	SpaceCode string `json:"space_code"`
	Occupied  bool   `json:"occupied"`
}

// Empty is the payload of podium.vehicle_detected and heartbeat.
type Empty struct{}

// Event is one decoded device event. Payload holds the concrete payload
// struct of Type, by value.
type Event struct {
	ID         uuid.UUID
	Type       EventType
	OccurredAt time.Time
	Payload    any
}
