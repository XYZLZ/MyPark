package devices_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/XYZLZ/MyPark/apps/server/internal/features/devices"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/gate"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/tariffs"
	"github.com/XYZLZ/MyPark/apps/server/internal/testsupport"
)

func TestRedactPayload(t *testing.T) {
	stay := uuid.MustParse("0191a2b3-c4d5-7e6f-8a9b-0c1d2e3f4a5c")
	tests := []struct {
		name string
		ev   gate.Event
		want string
	}{
		{"pay scan credential", evOf(gate.EventPayScan, gate.PayScan{Credential: gate.Credential{Kind: "ticket", Value: "DFFE6EADBA0A4C"}}),
			`{"credential":{"kind":"ticket","value":"sha256:a923af4c08c9"}}`},
		{"exit credential", evOf(gate.EventExitCredential, gate.ExitCredential{Credential: gate.Credential{Kind: "employee_card", Value: "DFFE6EADBA0A4C"}}),
			`{"credential":{"kind":"employee_card","value":"sha256:a923af4c08c9"}}`},
		{"entry credential", evOf(gate.EventEntryCredential, gate.EntryCredential{Kind: "employee_card", Value: "DFFE6EADBA0A4C"}),
			`{"kind":"employee_card","value":"sha256:a923af4c08c9"}`},
		{"ticket_request without value", evOf(gate.EventEntryCredential, gate.EntryCredential{Kind: "ticket_request"}),
			`{"kind":"ticket_request"}`},
		{"cash paid unchanged", evOf(gate.EventCashPaid, gate.CashPaid{StayID: stay, AmountCents: 5000}),
			`{"stay_id":"0191a2b3-c4d5-7e6f-8a9b-0c1d2e3f4a5c","amount_cents":5000}`},
		{"empty", evOf(gate.EventHeartbeat, gate.Empty{}), `{}`},
		{"sensor", evOf(gate.EventSensorChanged, gate.SensorChanged{SpaceCode: "E001", Occupied: true}), `{"space_code":"E001","occupied":true}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := devices.RedactPayload(tc.ev)
			if err != nil {
				t.Fatalf("RedactPayload: %v", err)
			}
			if string(got) != tc.want {
				t.Fatalf("got %s, want %s", got, tc.want)
			}
		})
	}
	if _, err := devices.RedactPayload(gate.Event{Type: gate.EventHeartbeat}); err == nil {
		t.Fatal("nil payload: want error")
	}
}

func TestSealResult_RoundTrip(t *testing.T) {
	s := testsupport.TestSealer(t)
	stay := uuid.MustParse("0191a2b3-c4d5-7e6f-8a9b-0c1d2e3f4a5c")
	amount := int64(5000)
	paid := time.Date(2026, 11, 14, 12, 0, 0, 123456000, time.UTC)
	code := "7K3M9QZX0ABC1DEF"
	res := gate.Result{
		Result: gate.OutcomeAck, StayID: &stay, TicketCode: code, Status: "active", AmountCents: &amount,
		Breakdown: &tariffs.Breakdown{Kind: tariffs.BreakdownTime, DurationSeconds: 3600, FullBlocks: 1, TariffEffectiveFrom: paid},
		ReceiptNo: "MP-00000001", PaidAt: &paid, ExitDeadline: &paid,
	}
	stored, err := devices.SealResult(s, res)
	if err != nil {
		t.Fatalf("SealResult: %v", err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(stored, &m); err != nil {
		t.Fatal(err)
	}
	if _, ok := m["ticket_code"]; ok {
		t.Fatal("stored JSON still has ticket_code")
	}
	if strings.Contains(string(stored), code) {
		t.Fatal("stored JSON contains the code")
	}
	var sealed struct {
		KeyID string `json:"key_id"`
		Box   string `json:"box"`
	}
	if err := json.Unmarshal(m["sealed"], &sealed); err != nil || sealed.KeyID != "test" || sealed.Box == "" {
		t.Fatalf("sealed = %s (%v)", m["sealed"], err)
	}

	opened, err := devices.OpenResult(s, stored)
	if err != nil {
		t.Fatalf("OpenResult: %v", err)
	}
	want, _ := json.Marshal(res)
	if string(opened) != string(want) {
		t.Fatalf("opened = %s\nwant   = %s", opened, want)
	}
}

func TestSealResult_NoTicketCodeStoresPlain(t *testing.T) {
	s := testsupport.TestSealer(t)
	res := gate.Result{Result: gate.OutcomeDeny, Reason: "gate.lot_full"}
	stored, err := devices.SealResult(s, res)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := json.Marshal(res)
	if string(stored) != string(want) || strings.Contains(string(stored), "sealed") {
		t.Fatalf("stored = %s", stored)
	}
	opened, err := devices.OpenResult(s, stored)
	if err != nil || string(opened) != string(want) {
		t.Fatalf("opened = %s (%v)", opened, err)
	}
}

func TestOpenResult_TamperedBoxFails(t *testing.T) {
	s := testsupport.TestSealer(t)
	stored, _ := devices.SealResult(s, gate.Result{Result: gate.OutcomeOpen, TicketCode: "7K3M9QZX0ABC1DEF"})
	bad := strings.Replace(string(stored), `"box":"`, `"box":"AAAA`, 1)
	if _, err := devices.OpenResult(s, json.RawMessage(bad)); err == nil {
		t.Fatal("tampered box opened")
	}
	if _, err := devices.OpenResult(s, json.RawMessage(`{"result":"open","sealed":{"key_id":"nope","box":"AAAA"}}`)); err == nil {
		t.Fatal("unknown key opened")
	}
}

type strayPayload struct{ Value string }

func TestRedactPayload_UnknownPayloadTypeErrors(t *testing.T) {
	if _, err := devices.RedactPayload(gate.Event{Type: gate.EventHeartbeat, Payload: strayPayload{Value: "secret"}}); err == nil {
		t.Fatal("unlisted payload type must not be stored unredacted")
	}
}
