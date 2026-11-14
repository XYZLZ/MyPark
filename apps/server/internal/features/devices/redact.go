package devices

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/XYZLZ/MyPark/apps/server/internal/features/gate"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/secretbox"
)

// RedactPayload returns the payload of ev as JSON with every credential value
// replaced by "sha256:" and the first 12 hex characters of its SHA-256, so
// the stored event log never holds a ticket code or card UID. A payload
// without a value is returned unchanged.
func RedactPayload(ev gate.Event) (json.RawMessage, error) {
	switch p := ev.Payload.(type) {
	case nil:
		return nil, errors.New("devices: event has no payload")
	case gate.EntryCredential:
		p.Value = redactValue(p.Value)
		return json.Marshal(p)
	case gate.PayScan:
		p.Credential.Value = redactValue(p.Credential.Value)
		return json.Marshal(p)
	case gate.ExitCredential:
		p.Credential.Value = redactValue(p.Credential.Value)
		return json.Marshal(p)
	case gate.Empty, gate.BarrierPassed, gate.LostTicket, gate.CashPaid, gate.SensorChanged:
		// No credential value: stored as is.
		return json.Marshal(p)
	default:
		return nil, fmt.Errorf("devices: no redaction rule for payload type %T", p)
	}
}

func redactValue(v string) string {
	if v == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(v))
	return "sha256:" + hex.EncodeToString(sum[:])[:12]
}

// sealedBox is the stored stand-in for a ticket code.
type sealedBox struct {
	KeyID string `json:"key_id"`
	Box   string `json:"box"`
}

// storedResult is a Result as stored: ticket_code is absent and a sealed box
// takes its place.
type storedResult struct {
	gate.Result
	Sealed *sealedBox `json:"sealed,omitempty"`
}

// SealResult returns r as JSON for storage with a non-empty ticket_code
// replaced by "sealed":{"key_id","box"} (the code sealed under the current
// key, box in base64). A result without a code is stored as is.
func SealResult(s *secretbox.Sealer, r gate.Result) (json.RawMessage, error) {
	st := storedResult{Result: r}
	if r.TicketCode != "" {
		box, keyID, err := s.Seal([]byte(r.TicketCode))
		if err != nil {
			return nil, fmt.Errorf("devices: seal ticket code: %w", err)
		}
		st.TicketCode = ""
		st.Sealed = &sealedBox{KeyID: keyID, Box: base64.StdEncoding.EncodeToString(box)}
	}
	return json.Marshal(st)
}

// OpenResult restores the ticket_code of a stored result, returning JSON
// identical to marshaling the original Result. A stored result without a
// sealed box is returned in canonical form.
func OpenResult(s *secretbox.Sealer, stored json.RawMessage) (json.RawMessage, error) {
	var st storedResult
	if err := json.Unmarshal(stored, &st); err != nil {
		return nil, fmt.Errorf("devices: decode stored result: %w", err)
	}
	if st.Sealed != nil {
		box, err := base64.StdEncoding.DecodeString(st.Sealed.Box)
		if err != nil {
			return nil, fmt.Errorf("devices: decode sealed box: %w", err)
		}
		plain, err := s.Open(box, st.Sealed.KeyID)
		if err != nil {
			return nil, fmt.Errorf("devices: open ticket code: %w", err)
		}
		st.TicketCode = string(plain)
	}
	return json.Marshal(st.Result)
}
