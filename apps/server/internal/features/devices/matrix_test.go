package devices_test

import (
	"slices"
	"testing"

	"github.com/google/uuid"

	"github.com/XYZLZ/MyPark/apps/server/internal/domain/authz"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/devices"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/gate"
)

func evOf(typ gate.EventType, payload any) gate.Event {
	return gate.Event{Type: typ, Payload: payload}
}

func payloadFor(typ gate.EventType) any {
	switch typ {
	case gate.EventEntryCredential:
		return gate.EntryCredential{Kind: "employee_card", Value: "AB"}
	case gate.EventBarrierPassed:
		return gate.BarrierPassed{Direction: "entry", StayID: uuid.New()}
	case gate.EventPayScan:
		return gate.PayScan{}
	case gate.EventLostTicket:
		return gate.LostTicket{}
	case gate.EventCashPaid:
		return gate.CashPaid{}
	case gate.EventExitCredential:
		return gate.ExitCredential{}
	case gate.EventSensorChanged:
		return gate.SensorChanged{}
	default:
		return gate.Empty{}
	}
}

func TestKindMatrix(t *testing.T) {
	kinds := []authz.DeviceKind{authz.DevicePodium, authz.DeviceDispenser, authz.DeviceEntryBarrier, authz.DeviceExitBarrier, authz.DevicePayStation, authz.DeviceSensorGateway}
	// Types a kind may send regardless of the credential kind or direction.
	// barrier.passed and entry.credential_presented are checked with the sub-rules below.
	allowed := map[authz.DeviceKind][]gate.EventType{
		authz.DevicePodium:        {gate.EventPodiumDetected, gate.EventHeartbeat},
		authz.DeviceDispenser:     {gate.EventHeartbeat},
		authz.DeviceEntryBarrier:  {gate.EventHeartbeat},
		authz.DeviceExitBarrier:   {gate.EventExitCredential, gate.EventHeartbeat},
		authz.DevicePayStation:    {gate.EventPayScan, gate.EventLostTicket, gate.EventCashPaid, gate.EventHeartbeat},
		authz.DeviceSensorGateway: {gate.EventSensorChanged, gate.EventHeartbeat},
	}
	for _, k := range kinds {
		for _, typ := range gate.AllEventTypes() {
			if typ == gate.EventBarrierPassed || typ == gate.EventEntryCredential {
				continue
			}
			want := false
			for _, a := range allowed[k] {
				want = want || a == typ
			}
			if got := devices.Allowed(k, evOf(typ, payloadFor(typ))); got != want {
				t.Errorf("Allowed(%s, %s) = %v, want %v", k, typ, got, want)
			}
		}
	}

	entry := func(kind string) gate.Event {
		return evOf(gate.EventEntryCredential, gate.EntryCredential{Kind: kind})
	}
	pass := func(dir string) gate.Event {
		return evOf(gate.EventBarrierPassed, gate.BarrierPassed{Direction: dir})
	}
	sub := []struct {
		name string
		kind authz.DeviceKind
		ev   gate.Event
		want bool
	}{
		{"dispenser ticket_request", authz.DeviceDispenser, entry("ticket_request"), true},
		{"dispenser employee_card", authz.DeviceDispenser, entry("employee_card"), false},
		{"dispenser reservation_qr", authz.DeviceDispenser, entry("reservation_qr"), false},
		{"entry barrier employee_card", authz.DeviceEntryBarrier, entry("employee_card"), true},
		{"entry barrier reservation_qr", authz.DeviceEntryBarrier, entry("reservation_qr"), true},
		{"entry barrier ticket_request", authz.DeviceEntryBarrier, entry("ticket_request"), false},
		{"podium entry credential", authz.DevicePodium, entry("employee_card"), false},
		{"pay station entry credential", authz.DevicePayStation, entry("ticket_request"), false},
		{"entry barrier pass entry", authz.DeviceEntryBarrier, pass("entry"), true},
		{"entry barrier pass exit", authz.DeviceEntryBarrier, pass("exit"), false},
		{"exit barrier pass exit", authz.DeviceExitBarrier, pass("exit"), true},
		{"exit barrier pass entry", authz.DeviceExitBarrier, pass("entry"), false},
		{"dispenser pass", authz.DeviceDispenser, pass("entry"), false},
		{"sensor gateway pass", authz.DeviceSensorGateway, pass("exit"), false},
		{"pay station pass", authz.DevicePayStation, pass("exit"), false},
		{"sensor gateway scan", authz.DeviceSensorGateway, evOf(gate.EventPayScan, gate.PayScan{}), false},
		{"pay station heartbeat", authz.DevicePayStation, evOf(gate.EventHeartbeat, gate.Empty{}), true},
		{"unknown kind heartbeat", authz.DeviceKind("toaster"), evOf(gate.EventHeartbeat, gate.Empty{}), false},
		{"payload not matching type", authz.DeviceEntryBarrier, evOf(gate.EventBarrierPassed, gate.Empty{}), false},
		{"unknown type", authz.DevicePodium, evOf(gate.EventType("door.opened"), gate.Empty{}), false},
	}
	for _, tc := range sub {
		if got := devices.Allowed(tc.kind, tc.ev); got != tc.want {
			t.Errorf("%s: Allowed = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// TestKindMatrixExhaustive checks every kind x type x credential kind x
// direction against an allow-set written out by hand from design §7.5.
func TestKindMatrixExhaustive(t *testing.T) {
	kinds := []authz.DeviceKind{authz.DevicePodium, authz.DeviceDispenser, authz.DeviceEntryBarrier, authz.DeviceExitBarrier, authz.DevicePayStation, authz.DeviceSensorGateway, authz.DeviceKind("toaster"), authz.DeviceKind("")}
	// Plain types: allowed kinds.
	plain := map[gate.EventType][]authz.DeviceKind{
		gate.EventPodiumDetected: {authz.DevicePodium},
		gate.EventPayScan:        {authz.DevicePayStation},
		gate.EventLostTicket:     {authz.DevicePayStation},
		gate.EventCashPaid:       {authz.DevicePayStation},
		gate.EventExitCredential: {authz.DeviceExitBarrier},
		gate.EventSensorChanged:  {authz.DeviceSensorGateway},
	}
	// heartbeat: the six real kinds only.
	heartbeat := []authz.DeviceKind{authz.DevicePodium, authz.DeviceDispenser, authz.DeviceEntryBarrier, authz.DeviceExitBarrier, authz.DevicePayStation, authz.DeviceSensorGateway}
	// entry.credential_presented: kind -> credential kinds allowed.
	entryOK := map[authz.DeviceKind][]string{
		authz.DeviceDispenser:    {gate.KindTicketRequest},
		authz.DeviceEntryBarrier: {gate.KindEmployeeCard, gate.KindReservationQR},
	}
	// barrier.passed: kind -> directions allowed.
	passOK := map[authz.DeviceKind][]string{
		authz.DeviceEntryBarrier: {gate.DirectionEntry},
		authz.DeviceExitBarrier:  {gate.DirectionExit},
	}
	has := func(list []authz.DeviceKind, k authz.DeviceKind) bool { return slices.Contains(list, k) }

	for _, k := range kinds {
		for _, typ := range gate.AllEventTypes() {
			switch typ {
			case gate.EventEntryCredential:
				for _, ck := range []string{gate.KindTicketRequest, gate.KindTicket, gate.KindEmployeeCard, gate.KindReservationQR, "", "other"} {
					want := slices.Contains(entryOK[k], ck)
					if got := devices.Allowed(k, evOf(typ, gate.EntryCredential{Kind: ck})); got != want {
						t.Errorf("Allowed(%q, %s, %q) = %v, want %v", k, typ, ck, got, want)
					}
				}
			case gate.EventBarrierPassed:
				for _, d := range []string{gate.DirectionEntry, gate.DirectionExit, "", "other"} {
					want := slices.Contains(passOK[k], d)
					if got := devices.Allowed(k, evOf(typ, gate.BarrierPassed{Direction: d})); got != want {
						t.Errorf("Allowed(%q, %s, %q) = %v, want %v", k, typ, d, got, want)
					}
				}
			case gate.EventHeartbeat:
				if got, want := devices.Allowed(k, evOf(typ, gate.Empty{})), has(heartbeat, k); got != want {
					t.Errorf("Allowed(%q, heartbeat) = %v, want %v", k, got, want)
				}
			default:
				if got, want := devices.Allowed(k, evOf(typ, payloadFor(typ))), has(plain[typ], k); got != want {
					t.Errorf("Allowed(%q, %s) = %v, want %v", k, typ, got, want)
				}
			}
		}
	}
}
