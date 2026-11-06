// Package middleware holds the HTTP middleware chain: request ID, panic
// recovery, access logging and JSON 404/405 responses for the router.
package middleware

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"net/http"
	"time"
)

// HeaderRequestID is the request correlation header, read from clients and
// always set on responses.
const HeaderRequestID = "X-Request-Id"

// maxRequestIDLen bounds accepted inbound IDs.
const maxRequestIDLen = 64

type ctxKey struct{}

// RequestID accepts a valid inbound X-Request-Id (1-64 chars of
// [A-Za-z0-9-]) or generates a UUIDv7. It sets the response header before
// calling next, and stores the ID in the request context.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get(HeaderRequestID)
		if !validRequestID(id) {
			id = newUUIDv7()
		}
		w.Header().Set(HeaderRequestID, id)
		next.ServeHTTP(w, r.WithContext(withRequestID(r.Context(), id)))
	})
}

// RequestIDFrom returns the request ID stored by RequestID, or "".
func RequestIDFrom(ctx context.Context) string {
	id, _ := ctx.Value(ctxKey{}).(string)
	return id
}

func withRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, ctxKey{}, id)
}

func validRequestID(id string) bool {
	if id == "" || len(id) > maxRequestIDLen {
		return false
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		if !('a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' || '0' <= c && c <= '9' || c == '-') {
			return false
		}
	}
	return true
}

// newUUIDv7 returns a canonical (8-4-4-4-12, lowercase hex) RFC 9562
// UUIDv7: a 48-bit big-endian Unix millisecond timestamp followed by random
// bits, with the version and variant bits set.
func newUUIDv7() string {
	var b [16]byte
	_, _ = rand.Read(b[6:]) // crypto/rand.Read never fails (Go 1.24+).
	var ts [8]byte
	binary.BigEndian.PutUint64(ts[:], uint64(time.Now().UnixMilli()))
	copy(b[:6], ts[2:])
	b[6] = b[6]&0x0f | 0x70 // version 7
	b[8] = b[8]&0x3f | 0x80 // variant 10

	var out [36]byte
	hex.Encode(out[0:8], b[0:4])
	out[8] = '-'
	hex.Encode(out[9:13], b[4:6])
	out[13] = '-'
	hex.Encode(out[14:18], b[6:8])
	out[18] = '-'
	hex.Encode(out[19:23], b[8:10])
	out[23] = '-'
	hex.Encode(out[24:], b[10:])
	return string(out[:])
}
