package devices_test

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/XYZLZ/MyPark/apps/server/internal/domain/apperr"
	"github.com/XYZLZ/MyPark/apps/server/internal/domain/errcodes"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/devices"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/gate"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/validation"
)

var decodeNow = time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)

const evID = "0191a2b3-c4d5-7e6f-8a9b-0c1d2e3f4a5b"

func evBody(typ, payload string) string {
	return fmt.Sprintf(`{"event_id":%q,"type":%q,"occurred_at":"2026-11-14T12:00:00Z","payload":%s}`, evID, typ, payload)
}

func decodeErr(t *testing.T, raw string) *apperr.Error {
	t.Helper()
	_, err := devices.DecodeEvent([]byte(raw), decodeNow)
	if err == nil {
		t.Fatalf("DecodeEvent(%s) = nil, want error", raw)
	}
	ae, ok := errors.AsType[*apperr.Error](err)
	if !ok {
		t.Fatalf("error %T is not *apperr.Error", err)
	}
	return ae
}

func fieldRules(ae *apperr.Error) map[string]string {
	out := map[string]string{}
	fields, _ := ae.Details["fields"].([]validation.FieldError)
	for _, f := range fields {
		out[f.Field] = f.Rule
	}
	return out
}

func wantInvalid(t *testing.T, raw, field, rule string) {
	t.Helper()
	ae := decodeErr(t, raw)
	if ae.Kind != apperr.KindInvalid || ae.Code != errcodes.ValidationFailed {
		t.Fatalf("%s: kind/code = %v/%v, want 422 validation.failed", raw, ae.Kind, ae.Code)
	}
	if got := fieldRules(ae)[field]; got != rule {
		t.Fatalf("%s: rule for %q = %q, want %q (all: %v)", raw, field, got, rule, fieldRules(ae))
	}
}

func wantBadJSON(t *testing.T, raw string) {
	t.Helper()
	ae := decodeErr(t, raw)
	if ae.Kind != apperr.KindBadRequest || ae.Code != errcodes.HTTPInvalidJSON {
		t.Fatalf("%s: kind/code = %v/%v, want 400 http.invalid_json", raw, ae.Kind, ae.Code)
	}
}

func TestDecodeEvent_EveryTypeValid(t *testing.T) {
	stay := "0191a2b3-c4d5-7e6f-8a9b-0c1d2e3f4a5c"
	stayID := uuid.MustParse(stay)
	tests := []struct {
		typ     gate.EventType
		payload string
		want    any
	}{
		{gate.EventPodiumDetected, `{}`, gate.Empty{}},
		{gate.EventEntryCredential, `{"kind":"ticket_request"}`, gate.EntryCredential{Kind: "ticket_request"}},
		{gate.EventEntryCredential, `{"kind":"employee_card","value":"DF:FE:6E:AD"}`, gate.EntryCredential{Kind: "employee_card", Value: "DF:FE:6E:AD"}},
		{gate.EventBarrierPassed, `{"direction":"exit","stay_id":"` + stay + `"}`, gate.BarrierPassed{Direction: "exit", StayID: stayID}},
		{gate.EventPayScan, `{"credential":{"kind":"ticket","value":"7K3M"}}`, gate.PayScan{Credential: gate.Credential{Kind: "ticket", Value: "7K3M"}}},
		{gate.EventLostTicket, `{"stay_id":"` + stay + `"}`, gate.LostTicket{StayID: stayID}},
		{gate.EventCashPaid, `{"stay_id":"` + stay + `","amount_cents":5000}`, gate.CashPaid{StayID: stayID, AmountCents: 5000}},
		{gate.EventExitCredential, `{"credential":{"kind":"reservation_qr","value":"abc"}}`, gate.ExitCredential{Credential: gate.Credential{Kind: "reservation_qr", Value: "abc"}}},
		{gate.EventSensorChanged, `{"space_code":"E001","occupied":false}`, gate.SensorChanged{SpaceCode: "E001", Occupied: false}},
		{gate.EventHeartbeat, `{}`, gate.Empty{}},
	}
	for _, tc := range tests {
		t.Run(string(tc.typ), func(t *testing.T) {
			ev, err := devices.DecodeEvent([]byte(evBody(string(tc.typ), tc.payload)), decodeNow)
			if err != nil {
				t.Fatalf("DecodeEvent: %v", err)
			}
			if ev.ID != uuid.MustParse(evID) || ev.Type != tc.typ || !ev.OccurredAt.Equal(decodeNow) {
				t.Fatalf("envelope = %+v", ev)
			}
			if !reflect.DeepEqual(ev.Payload, tc.want) {
				t.Fatalf("payload = %#v, want %#v", ev.Payload, tc.want)
			}
		})
	}
}

func TestDecodeEvent_AmountHasNoCeiling(t *testing.T) {
	stay := "0191a2b3-c4d5-7e6f-8a9b-0c1d2e3f4a5c"
	ev, err := devices.DecodeEvent([]byte(evBody("paystation.cash_paid", `{"stay_id":"`+stay+`","amount_cents":9223372036854775807}`)), decodeNow)
	if err != nil {
		t.Fatalf("DecodeEvent: %v", err)
	}
	if got := ev.Payload.(gate.CashPaid).AmountCents; got != 9223372036854775807 {
		t.Fatalf("amount = %d", got)
	}
}

func TestDecodeEvent_EnvelopeRules(t *testing.T) {
	at := func(d time.Duration) string {
		return fmt.Sprintf(`{"event_id":%q,"type":"heartbeat","occurred_at":%q,"payload":{}}`, evID, decodeNow.Add(d).Format(time.RFC3339))
	}
	for name, d := range map[string]time.Duration{"now+60s": 60 * time.Second, "now-3h": -3 * time.Hour} {
		if _, err := devices.DecodeEvent([]byte(at(d)), decodeNow); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	wantInvalid(t, at(61*time.Second), "occurred_at", "future")

	wantInvalid(t, `{"type":"heartbeat","occurred_at":"2026-11-14T12:00:00Z","payload":{}}`, "event_id", "required")
	wantInvalid(t, strings.Replace(evBody("heartbeat", `{}`), evID, "abc", 1), "event_id", "uuid")
	wantInvalid(t, evBody("door.opened", `{}`), "type", "oneof")
	wantInvalid(t, `{"event_id":"`+evID+`","occurred_at":"2026-11-14T12:00:00Z","payload":{}}`, "type", "required")
	wantInvalid(t, strings.Replace(evBody("heartbeat", `{}`), "2026-11-14T12:00:00Z", "yesterday", 1), "occurred_at", "datetime")
	wantInvalid(t, `{"event_id":"`+evID+`","type":"heartbeat","occurred_at":"2026-11-14T12:00:00Z"}`, "payload", "required")
}

func TestDecodeEvent_EnvelopeReportsEveryField(t *testing.T) {
	ae := decodeErr(t, `{"type":"door.opened","occurred_at":"x","payload":{}}`)
	got := fieldRules(ae)
	want := map[string]string{"event_id": "required", "type": "oneof", "occurred_at": "datetime"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("fields = %v, want %v", got, want)
	}
}

func TestDecodeEvent_NestedJunk(t *testing.T) {
	stay := "0191a2b3-c4d5-7e6f-8a9b-0c1d2e3f4a5c"
	// payload null or absent is 422 required, never a nil dereference.
	wantInvalid(t, evBody("heartbeat", `null`), "payload", "required")
	// payload of the wrong JSON type is 400.
	wantBadJSON(t, evBody("heartbeat", `[]`))
	wantBadJSON(t, evBody("heartbeat", `5`))
	// Unknown fields at every level are 422 unknown_field.
	wantInvalid(t, evBody("entry.credential_presented", `{"kind":"ticket_request","x":1}`), "payload.x", "unknown_field")
	wantInvalid(t, evBody("paystation.scan", `{"credential":{"kind":"ticket","value":"A","extra":true}}`), "payload.credential.extra", "unknown_field")
	wantInvalid(t, evBody("heartbeat", `{"x":1}`), "payload.x", "unknown_field")
	wantInvalid(t, strings.Replace(evBody("heartbeat", `{}`), `"payload"`, `"zzz":1,"payload"`, 1), "zzz", "unknown_field")
	// Wrong amount types are 400; out-of-range values are 422.
	for _, amt := range []string{`5000.5`, `"5000"`, `1e20`, `true`} {
		wantBadJSON(t, evBody("paystation.cash_paid", `{"stay_id":"`+stay+`","amount_cents":`+amt+`}`))
	}
	for _, amt := range []string{`0`, `-1`} {
		wantInvalid(t, evBody("paystation.cash_paid", `{"stay_id":"`+stay+`","amount_cents":`+amt+`}`), "payload.amount_cents", "min")
	}
	wantInvalid(t, evBody("paystation.cash_paid", `{"stay_id":"`+stay+`"}`), "payload.amount_cents", "required")
	wantBadJSON(t, evBody("paystation.scan", `{"credential":5}`))
	wantInvalid(t, evBody("paystation.scan", `{"credential":null}`), "payload.credential", "required")
	// value rules.
	wantInvalid(t, evBody("paystation.scan", `{"credential":{"kind":"ticket","value":"`+strings.Repeat("a", 513)+`"}}`), "payload.credential.value", "max")
	wantInvalid(t, evBody("paystation.scan", `{"credential":{"kind":"ticket"}}`), "payload.credential.value", "required")
	wantInvalid(t, evBody("paystation.scan", `{"credential":{"kind":"ticket_request","value":"a"}}`), "payload.credential.kind", "oneof")
	wantInvalid(t, evBody("entry.credential_presented", `{"kind":"employee_card"}`), "payload.value", "required")
	wantInvalid(t, evBody("entry.credential_presented", `{"kind":"ticket"}`), "payload.kind", "oneof")
	// space_code, occupied, direction, stay_id.
	wantInvalid(t, evBody("sensor.space_changed", `{"space_code":"e001","occupied":true}`), "payload.space_code", "format")
	wantInvalid(t, evBody("sensor.space_changed", `{"space_code":"E001"}`), "payload.occupied", "required")
	wantInvalid(t, evBody("barrier.passed", `{"direction":"sideways","stay_id":"`+stay+`"}`), "payload.direction", "oneof")
	wantInvalid(t, evBody("barrier.passed", `{"direction":"entry","stay_id":"nope"}`), "payload.stay_id", "uuid")
	wantInvalid(t, evBody("paystation.lost_ticket", `{}`), "payload.stay_id", "required")
	// Top level.
	wantBadJSON(t, `null`)
	wantBadJSON(t, `[]`)
	wantBadJSON(t, ``)
	wantBadJSON(t, evBody("heartbeat", `{}`)+` {}`)
	wantBadJSON(t, `{"event_id":5}`)
}

func FuzzDecodeEvent(f *testing.F) {
	stay := "0191a2b3-c4d5-7e6f-8a9b-0c1d2e3f4a5c"
	seeds := map[string]string{
		"podium.vehicle_detected":    `{}`,
		"entry.credential_presented": `{"kind":"employee_card","value":"DFFE6EAD"}`,
		"barrier.passed":             `{"direction":"entry","stay_id":"` + stay + `"}`,
		"paystation.scan":            `{"credential":{"kind":"ticket","value":"A"}}`,
		"paystation.lost_ticket":     `{"stay_id":"` + stay + `"}`,
		"paystation.cash_paid":       `{"stay_id":"` + stay + `","amount_cents":5000}`,
		"exit.credential_presented":  `{"credential":{"kind":"ticket","value":"A"}}`,
		"sensor.space_changed":       `{"space_code":"E001","occupied":true}`,
		"heartbeat":                  `{}`,
	}
	for typ, p := range seeds {
		f.Add([]byte(evBody(typ, p)))
	}
	f.Add([]byte(evBody("paystation.scan", `{"credential":{"kind":"ticket","Value":"A","value":"B","x":[1,{"y":null}]}}`)))
	f.Add([]byte(`{"payload":null}`))
	f.Fuzz(func(t *testing.T, raw []byte) {
		ev, err := devices.DecodeEvent(raw, decodeNow)
		if err != nil {
			ae, ok := errors.AsType[*apperr.Error](err)
			if !ok || (ae.Kind != apperr.KindBadRequest && ae.Kind != apperr.KindInvalid) {
				t.Fatalf("error %v is not a 400/422 *apperr.Error", err)
			}
			return
		}
		if ev.Payload == nil || reflect.TypeOf(ev.Payload) != reflect.TypeOf(payloadFor(ev.Type)) {
			t.Fatalf("payload %T does not match type %q", ev.Payload, ev.Type)
		}
	})
}

func TestDecodeEvent_CaseVariantAndDuplicateKeys(t *testing.T) {
	stay := "0191a2b3-c4d5-7e6f-8a9b-0c1d2e3f4a5c"
	wantInvalid(t, strings.Replace(evBody("heartbeat", `{}`), `"type"`, `"Type"`, 1), "Type", "unknown_field")
	wantInvalid(t, strings.Replace(evBody("heartbeat", `{}`), `"type":"heartbeat"`, `"type":"heartbeat","type":"podium.vehicle_detected"`, 1), "type", "unknown_field")
	wantInvalid(t, evBody("paystation.scan", `{"credential":{"kind":"ticket","Value":"A"}}`), "payload.credential.Value", "unknown_field")
	wantInvalid(t, evBody("paystation.scan", `{"credential":{"kind":"ticket","value":"A","value":"B"}}`), "payload.credential.value", "unknown_field")
	wantInvalid(t, evBody("paystation.lost_ticket", `{"stay_id":"`+stay+`","stay_id":"`+stay+`"}`), "payload.stay_id", "unknown_field")
	wantInvalid(t, evBody("paystation.lost_ticket", `{"Stay_ID":"`+stay+`"}`), "payload.Stay_ID", "unknown_field")
}

func TestDecodeEvent_NilUUIDRejected(t *testing.T) {
	nilID := "00000000-0000-0000-0000-000000000000"
	wantInvalid(t, strings.Replace(evBody("heartbeat", `{}`), evID, nilID, 1), "event_id", "uuid")
	wantInvalid(t, evBody("paystation.lost_ticket", `{"stay_id":"`+nilID+`"}`), "payload.stay_id", "uuid")
}
