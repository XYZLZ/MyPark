// Package secret provides a string type that never prints its value.
package secret

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
)

const redacted = "[redacted]"

// String holds a sensitive value (password, code, token). Every formatting,
// logging and JSON-encoding path prints "[redacted]"; only Reveal exposes it.
type String string

// Format implements fmt.Formatter so that every verb (%d, %x, %c, %o...) prints
// the marker; fmt calls String only for %v, %s, %x, %X and %q.
func (String) Format(f fmt.State, _ rune) { _, _ = io.WriteString(f, redacted) }

// Reveal returns the plaintext value.
func (s String) Reveal() string { return string(s) }

// String implements fmt.Stringer.
func (String) String() string { return redacted }

// GoString implements fmt.GoStringer (%#v).
func (String) GoString() string { return redacted }

// LogValue implements slog.LogValuer.
func (String) LogValue() slog.Value { return slog.StringValue(redacted) }

// MarshalJSON always encodes the redaction marker.
func (String) MarshalJSON() ([]byte, error) { return json.Marshal(redacted) }

// UnmarshalJSON accepts a JSON string only.
func (s *String) UnmarshalJSON(b []byte) error {
	var v string
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	*s = String(v)
	return nil
}

// Bytes holds sensitive raw bytes (a decrypted device secret, a key). Every
// formatting, logging and JSON-encoding path prints "[redacted]"; only Reveal
// exposes the bytes.
//
// Limitation: fmt cannot call methods on unexported struct fields, so a Bytes
// held in an unexported field of a struct that gets printed (%v, %+v) still
// shows its raw bytes. Keep secrets out of structs that are logged.
type Bytes []byte

// Format implements fmt.Formatter so that every verb (%d, %x, %c, %o...) prints
// the marker; fmt calls String only for %v, %s, %x, %X and %q.
func (Bytes) Format(f fmt.State, _ rune) { _, _ = io.WriteString(f, redacted) }

// Reveal returns the plaintext bytes.
func (b Bytes) Reveal() []byte { return b }

// String implements fmt.Stringer.
func (Bytes) String() string { return redacted }

// GoString implements fmt.GoStringer (%#v).
func (Bytes) GoString() string { return redacted }

// LogValue implements slog.LogValuer.
func (Bytes) LogValue() slog.Value { return slog.StringValue(redacted) }

// MarshalJSON always encodes the redaction marker.
func (Bytes) MarshalJSON() ([]byte, error) { return json.Marshal(redacted) }
