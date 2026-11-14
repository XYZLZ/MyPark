package devices

import (
	"github.com/XYZLZ/MyPark/apps/server/internal/domain/authz"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/gate"
)

// kindMatrix is the device kind to event type table (design §7.2, M2C-21).
// It is deliberately one table: a too-strict row blocks a legitimate flow and
// TestKindMatrix is where that is caught.
var kindMatrix = map[authz.DeviceKind]map[gate.EventType]bool{
	authz.DevicePodium:        {gate.EventPodiumDetected: true, gate.EventHeartbeat: true},
	authz.DeviceDispenser:     {gate.EventEntryCredential: true, gate.EventHeartbeat: true},
	authz.DeviceEntryBarrier:  {gate.EventEntryCredential: true, gate.EventBarrierPassed: true, gate.EventHeartbeat: true},
	authz.DeviceExitBarrier:   {gate.EventBarrierPassed: true, gate.EventExitCredential: true, gate.EventHeartbeat: true},
	authz.DevicePayStation:    {gate.EventPayScan: true, gate.EventLostTicket: true, gate.EventCashPaid: true, gate.EventHeartbeat: true},
	authz.DeviceSensorGateway: {gate.EventSensorChanged: true, gate.EventHeartbeat: true},
}

// Allowed reports whether a device of the given kind may send ev (design
// §7.5): the kind must be allowed the type, and the sub-rules hold. A
// dispenser sends only ticket_request; an entry barrier only employee_card or
// reservation_qr and direction entry; an exit barrier only direction exit.
// An unknown kind, type or a payload that does not match the type is refused.
func Allowed(kind authz.DeviceKind, ev gate.Event) bool {
	if !kindMatrix[kind][ev.Type] {
		return false
	}
	switch ev.Type {
	case gate.EventEntryCredential:
		p, ok := ev.Payload.(gate.EntryCredential)
		if !ok {
			return false
		}
		switch kind {
		case authz.DeviceDispenser:
			return p.Kind == gate.KindTicketRequest
		case authz.DeviceEntryBarrier:
			return p.Kind == gate.KindEmployeeCard || p.Kind == gate.KindReservationQR
		}
	case gate.EventBarrierPassed:
		p, ok := ev.Payload.(gate.BarrierPassed)
		if !ok {
			return false
		}
		switch kind {
		case authz.DeviceEntryBarrier:
			return p.Direction == gate.DirectionEntry
		case authz.DeviceExitBarrier:
			return p.Direction == gate.DirectionExit
		}
	}
	return true
}
