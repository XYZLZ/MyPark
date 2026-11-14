// Package devicesig builds, signs and verifies the canonical string of a
// device request (design §7.1, §7.3, M2C-16). The API verifies with it and
// the simulator signs with the same algorithm; testdata/golden.json pins it.
package devicesig

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"time"
)

// TimestampLayout is the only accepted X-Timestamp form: RFC 3339 in UTC with
// a literal Z and no fractional seconds.
const TimestampLayout = "2006-01-02T15:04:05Z"

// signatureLen is the length of a hex-encoded HMAC-SHA256.
const signatureLen = 2 * sha256.Size

// Canonical returns METHOD\nREQUEST_URI\nX-Timestamp\nhex(sha256(body)) with
// lowercase hex and no trailing newline. requestURI is the path plus the raw
// query exactly as sent, and timestamp is the header value verbatim.
func Canonical(method, requestURI, timestamp string, body []byte) []byte {
	sum := sha256.Sum256(body)
	out := make([]byte, 0, len(method)+len(requestURI)+len(timestamp)+3+hex.EncodedLen(len(sum)))
	out = append(out, method...)
	out = append(out, '\n')
	out = append(out, requestURI...)
	out = append(out, '\n')
	out = append(out, timestamp...)
	out = append(out, '\n')
	return hex.AppendEncode(out, sum[:])
}

// Sign returns hex(HMAC-SHA256(secret, canonical)), 64 lowercase hex.
func Sign(secret, canonical []byte) string {
	return hex.EncodeToString(mac(secret, canonical))
}

// Verify reports whether sig is the well-formed signature of canonical under
// secret. The MAC comparison is constant time (hmac.Equal).
func Verify(secret, canonical []byte, sig string) bool {
	if !WellFormedSignature(sig) {
		return false
	}
	got, err := hex.DecodeString(sig)
	if err != nil {
		return false
	}
	return hmac.Equal(mac(secret, canonical), got)
}

// WellFormedSignature reports whether sig is exactly 64 lowercase hex digits.
func WellFormedSignature(sig string) bool {
	if len(sig) != signatureLen {
		return false
	}
	for i := range len(sig) {
		c := sig[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// ParseTimestamp parses an X-Timestamp. It accepts only YYYY-MM-DDTHH:MM:SSZ:
// time.Parse alone would also take a fractional second, so the value must
// round-trip through the layout byte for byte.
func ParseTimestamp(s string) (time.Time, bool) {
	if len(s) != len(TimestampLayout) {
		return time.Time{}, false
	}
	t, err := time.Parse(TimestampLayout, s)
	if err != nil || t.Format(TimestampLayout) != s {
		return time.Time{}, false
	}
	return t, true
}

func mac(secret, canonical []byte) []byte {
	m := hmac.New(sha256.New, secret)
	m.Write(canonical)
	return m.Sum(nil)
}
