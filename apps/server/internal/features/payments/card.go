// Package payments holds the payment domain: card rules, the gateway port and
// the refund backoff. Card data lives in memory only and never prints.
package payments

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/validation"
)

// Card is a card in flight to the gateway. It is never stored or logged; its
// formatting methods print only the last four digits.
type Card struct {
	Number   string
	ExpMonth int
	ExpYear  int
	CVC      string
}

// String, GoString and LogValue all print card(****1234).
func (c Card) String() string   { return "card(****" + last4(c.Number) + ")" }
func (c Card) GoString() string { return c.String() }

// LogValue implements slog.LogValuer.
func (c Card) LogValue() slog.Value { return slog.StringValue(c.String()) }

// Last4 returns the last four digits of the number, or "" when it is shorter.
func (c Card) Last4() string { return last4(c.Number) }

func last4(number string) string {
	if len(number) < 4 {
		return ""
	}
	return number[len(number)-4:]
}

// PayRequest is the body of POST /v1/reservations/{id}/pay. Normalize the card
// number first (NormalizeCardNumber), then validate with a context carrying
// the business time (validation.WithNow) so card_expiry can run.
type PayRequest struct {
	CardNumber string `json:"card_number" validate:"required,number,min=13,max=19,luhn"`
	ExpMonth   int    `json:"exp_month" validate:"required,min=1,max=12"`
	ExpYear    int    `json:"exp_year" validate:"required,min=2000,max=2099,card_expiry=ExpMonth"`
	CVC        string `json:"cvc" validate:"required,number,min=3,max=4"`
}

// NormalizeCardNumber removes ASCII spaces and hyphens, the way people type
// card numbers. Nothing else is touched, so other junk still fails validation.
func NormalizeCardNumber(raw string) string {
	return strings.NewReplacer(" ", "", "-", "").Replace(raw)
}

// Luhn reports whether number passes the Luhn checksum.
func Luhn(number string) bool { return validation.Luhn(number) }

// ExpiryValid reports whether a card expiring at the end of month/year is not
// expired at now, in UTC-4 (the lot's time zone).
func ExpiryValid(month, year int, now time.Time) bool {
	return validation.CardExpiryValid(month, year, now)
}

// Brand classifies a card number by BIN: 4 visa; 51-55 and 2221-2720
// mastercard; 34 and 37 amex; anything else other.
func Brand(number string) string {
	switch {
	case strings.HasPrefix(number, "4"):
		return "visa"
	case inRange(number, 2, 51, 55), inRange(number, 4, 2221, 2720):
		return "mastercard"
	case strings.HasPrefix(number, "34"), strings.HasPrefix(number, "37"):
		return "amex"
	}
	return "other"
}

// inRange reports whether the first n digits of number are a number in [lo, hi].
func inRange(number string, n, lo, hi int) bool {
	if len(number) < n {
		return false
	}
	v := 0
	for _, c := range number[:n] {
		if c < '0' || c > '9' {
			return false
		}
		v = v*10 + int(c-'0')
	}
	return v >= lo && v <= hi
}

// Fingerprint identifies a pay attempt for idempotency (M3B-7): the
// reservation, brand, last four and expiry. The CVC and the full number are
// excluded, so the fingerprint is safe to store.
func Fingerprint(reservation uuid.UUID, brand, last4 string, expMonth, expYear int) []byte {
	h := sha256.New()
	h.Write(reservation[:])
	fmt.Fprintf(h, "%s|%s|%02d%04d", brand, last4, expMonth, expYear)
	return h.Sum(nil)
}

// MarshalJSON redacts the card: encoders (slog's JSON handler falls back to
// json.Marshal for values that are not LogValuers) must never see the number
// or the CVC. Decoding is unaffected: Card is never decoded from JSON.
func (c Card) MarshalJSON() ([]byte, error) { return json.Marshal(c.String()) }

// redacted is the safe printed form of a pay request: last four digits only.
func (r PayRequest) redacted() string {
	return "pay(card=****" + last4(r.CardNumber) + ")"
}

// String, GoString, LogValue and MarshalJSON redact the request so logging a
// decoded body cannot leak the number or the CVC. Only the encode side is
// redacted: decoding into PayRequest is untouched.
func (r PayRequest) String() string   { return r.redacted() }
func (r PayRequest) GoString() string { return r.redacted() }

// LogValue implements slog.LogValuer.
func (r PayRequest) LogValue() slog.Value { return slog.StringValue(r.redacted()) }

// MarshalJSON encodes the redacted form as a JSON string.
func (r PayRequest) MarshalJSON() ([]byte, error) { return json.Marshal(r.redacted()) }
