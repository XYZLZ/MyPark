package devices

import (
	"encoding/json"
	"regexp"
	"slices"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/XYZLZ/MyPark/apps/server/internal/features/gate"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/validation"
	"github.com/XYZLZ/MyPark/apps/server/internal/transport/http/httpx"
)

const (
	// maxClockSkewAhead is how far occurred_at may run ahead of the server clock.
	maxClockSkewAhead = 60 * time.Second
	// maxCredentialValue is the longest credential value, in characters.
	maxCredentialValue = 512
)

var spaceCodePattern = regexp.MustCompile(`^E[0-9]{3}$`)

// Rule names the decoder reports beyond the validator's own, in
// details.fields[].rule. They are only ever read by device firmware.
const (
	ruleRequired = "required"
	ruleOneOf    = "oneof"
	ruleMin      = "min"
	ruleMax      = "max"
	ruleUUID     = "uuid"
	ruleDatetime = "datetime"
	ruleFuture   = "future"
	ruleFormat   = "format"
)

// wireEnvelope is the request body before any rule is applied. Every field is
// optional so that a missing one is reported as `required`, not defaulted.
type wireEnvelope struct {
	EventID    *string         `json:"event_id"`
	Type       *string         `json:"type"`
	OccurredAt *string         `json:"occurred_at"`
	Payload    json.RawMessage `json:"payload"`
}

// DecodeEvent strictly decodes one device event (design §7.1): unknown fields
// at any level, trailing data and a non-object body are refused. A malformed
// body or a wrong JSON type is 400 http.invalid_json; a rule failure or an
// unknown field is 422 validation.failed listing the failed fields, never
// their values. The envelope's field errors are reported together, and only
// when the envelope is sound is the payload decoded.
func DecodeEvent(raw []byte, now time.Time) (gate.Event, error) {
	var env wireEnvelope
	if err := httpx.DecodeStrict(raw, &env); err != nil {
		return gate.Event{}, err
	}

	var fe fieldErrors
	var ev gate.Event

	switch {
	case env.EventID == nil || *env.EventID == "":
		fe.add("event_id", ruleRequired)
	default:
		id, ok := parseUUID(*env.EventID)
		if !ok {
			fe.add("event_id", ruleUUID)
		}
		ev.ID = id
	}

	switch {
	case env.Type == nil || *env.Type == "":
		fe.add("type", ruleRequired)
	default:
		ev.Type = gate.EventType(*env.Type)
		if !knownType(ev.Type) {
			fe.add("type", ruleOneOf)
		}
	}

	switch {
	case env.OccurredAt == nil || *env.OccurredAt == "":
		fe.add("occurred_at", ruleRequired)
	default:
		at, err := time.Parse(time.RFC3339, *env.OccurredAt)
		switch {
		case err != nil:
			fe.add("occurred_at", ruleDatetime)
		case at.After(now.Add(maxClockSkewAhead)):
			fe.add("occurred_at", ruleFuture)
		default:
			ev.OccurredAt = at.UTC()
		}
	}

	payload := trimmed(env.Payload)
	switch {
	case len(payload) == 0 || string(payload) == "null":
		fe.add("payload", ruleRequired)
	case payload[0] != '{':
		return gate.Event{}, httpx.DecodeStrict(payload, &struct{}{})
	}

	if fe.any() {
		return gate.Event{}, fe.err()
	}
	p, err := decodePayload(ev.Type, payload)
	if err != nil {
		return gate.Event{}, err
	}
	ev.Payload = p
	return ev, nil
}

func trimmed(b json.RawMessage) []byte {
	for len(b) > 0 && (b[0] == ' ' || b[0] == '\t' || b[0] == '\n' || b[0] == '\r') {
		b = b[1:]
	}
	return b
}

func knownType(t gate.EventType) bool { return slices.Contains(gate.AllEventTypes(), t) }

func parseUUID(s string) (uuid.UUID, bool) {
	if len(s) != 36 {
		return uuid.UUID{}, false
	}
	id, err := uuid.Parse(s)
	return id, err == nil && id != uuid.Nil
}

// fieldErrors collects the field errors of one decode.
type fieldErrors []validation.FieldError

func (f *fieldErrors) add(field, rule string) {
	*f = append(*f, validation.FieldError{Field: field, Rule: rule})
}
func (f fieldErrors) any() bool { return len(f) > 0 }
func (f fieldErrors) err() error {
	return httpx.InvalidFields(f...)
}

// Wire payloads: every field a pointer, so absence is told from a zero value.
type (
	// wireCredential is the kind+value pair of an entry credential and of a
	// presented credential alike.
	wireCredential struct {
		Kind  *string `json:"kind"`
		Value *string `json:"value"`
	}
	wireCredentialPayload struct {
		Credential json.RawMessage `json:"credential"`
	}
	wireBarrier struct {
		Direction *string `json:"direction"`
		StayID    *string `json:"stay_id"`
	}
	wireStay struct {
		StayID *string `json:"stay_id"`
	}
	wireCash struct {
		StayID      *string `json:"stay_id"`
		AmountCents *int64  `json:"amount_cents"`
	}
	wireSensor struct {
		SpaceCode *string `json:"space_code"`
		Occupied  *bool   `json:"occupied"`
	}
)

// decodePayload decodes the payload object of type t with a second strict
// decoder and applies the per-type rules. Field names are prefixed "payload.".
func decodePayload(t gate.EventType, raw []byte) (any, error) {
	var fe fieldErrors
	var out any

	switch t {
	case gate.EventPodiumDetected, gate.EventHeartbeat:
		if err := httpx.DecodeStrictAt(raw, &struct{}{}, "payload"); err != nil {
			return nil, err
		}
		out = gate.Empty{}

	case gate.EventEntryCredential:
		var w wireCredential
		if err := httpx.DecodeStrictAt(raw, &w, "payload"); err != nil {
			return nil, err
		}
		kind := oneOf(&fe, "payload.kind", w.Kind, gate.KindTicketRequest, gate.KindEmployeeCard, gate.KindReservationQR)
		// value is required except for a ticket request.
		value := credentialValue(&fe, "payload.value", w.Value, kind != gate.KindTicketRequest && w.Kind != nil)
		out = gate.EntryCredential{Kind: kind, Value: value}

	case gate.EventBarrierPassed:
		var w wireBarrier
		if err := httpx.DecodeStrictAt(raw, &w, "payload"); err != nil {
			return nil, err
		}
		dir := oneOf(&fe, "payload.direction", w.Direction, gate.DirectionEntry, gate.DirectionExit)
		out = gate.BarrierPassed{Direction: dir, StayID: stayID(&fe, "payload.stay_id", w.StayID)}

	case gate.EventPayScan, gate.EventExitCredential:
		var w wireCredentialPayload
		if err := httpx.DecodeStrictAt(raw, &w, "payload"); err != nil {
			return nil, err
		}
		cred, err := credential(&fe, w.Credential)
		if err != nil {
			return nil, err
		}
		if t == gate.EventPayScan {
			out = gate.PayScan{Credential: cred}
		} else {
			out = gate.ExitCredential{Credential: cred}
		}

	case gate.EventLostTicket:
		var w wireStay
		if err := httpx.DecodeStrictAt(raw, &w, "payload"); err != nil {
			return nil, err
		}
		out = gate.LostTicket{StayID: stayID(&fe, "payload.stay_id", w.StayID)}

	case gate.EventCashPaid:
		var w wireCash
		if err := httpx.DecodeStrictAt(raw, &w, "payload"); err != nil {
			return nil, err
		}
		c := gate.CashPaid{StayID: stayID(&fe, "payload.stay_id", w.StayID)}
		switch {
		case w.AmountCents == nil:
			fe.add("payload.amount_cents", ruleRequired)
		case *w.AmountCents < 1:
			fe.add("payload.amount_cents", ruleMin)
		default:
			c.AmountCents = *w.AmountCents
		}
		out = c

	case gate.EventSensorChanged:
		var w wireSensor
		if err := httpx.DecodeStrictAt(raw, &w, "payload"); err != nil {
			return nil, err
		}
		s := gate.SensorChanged{}
		switch {
		case w.SpaceCode == nil || *w.SpaceCode == "":
			fe.add("payload.space_code", ruleRequired)
		case !spaceCodePattern.MatchString(*w.SpaceCode):
			fe.add("payload.space_code", ruleFormat)
		default:
			s.SpaceCode = *w.SpaceCode
		}
		if w.Occupied == nil {
			fe.add("payload.occupied", ruleRequired)
		} else {
			s.Occupied = *w.Occupied
		}
		out = s
	}

	if fe.any() {
		return nil, fe.err()
	}
	return out, nil
}

// oneOf returns *p when it is one of allowed, else records required or oneof.
func oneOf(fe *fieldErrors, field string, p *string, allowed ...string) string {
	if p == nil || *p == "" {
		fe.add(field, ruleRequired)
		return ""
	}
	for _, a := range allowed {
		if *p == a {
			return *p
		}
	}
	fe.add(field, ruleOneOf)
	return ""
}

// credentialValue checks a credential value: at most 512 characters, and
// present when required is true.
func credentialValue(fe *fieldErrors, field string, p *string, required bool) string {
	switch {
	case p == nil || *p == "":
		if required {
			fe.add(field, ruleRequired)
		}
		return ""
	case utf8.RuneCountInString(*p) > maxCredentialValue:
		fe.add(field, ruleMax)
		return ""
	}
	return *p
}

func credential(fe *fieldErrors, raw json.RawMessage) (gate.Credential, error) {
	raw = trimmed(raw)
	if len(raw) == 0 || string(raw) == "null" {
		fe.add("payload.credential", ruleRequired)
		return gate.Credential{}, nil
	}
	var w wireCredential
	if err := httpx.DecodeStrictAt(raw, &w, "payload.credential"); err != nil {
		return gate.Credential{}, err
	}
	kind := oneOf(fe, "payload.credential.kind", w.Kind, gate.KindTicket, gate.KindEmployeeCard, gate.KindReservationQR)
	value := credentialValue(fe, "payload.credential.value", w.Value, true)
	return gate.Credential{Kind: kind, Value: value}, nil
}

func stayID(fe *fieldErrors, field string, p *string) uuid.UUID {
	if p == nil || *p == "" {
		fe.add(field, ruleRequired)
		return uuid.UUID{}
	}
	id, ok := parseUUID(*p)
	if !ok {
		fe.add(field, ruleUUID)
	}
	return id
}
