// Package reqctx carries per-request values (request ID, log fields) through
// a context.Context without making response or middleware import each other.
package reqctx

import (
	"context"
	"net/netip"
)

type (
	requestIDKey struct{}
	logFieldsKey struct{}
)

// LogFields holds values that handlers learn late in a request (for example
// the authenticated user) and that the access log reads after the handler
// returns. It must hold no personal data, only opaque public IDs.
type LogFields struct {
	UserPublicID string
}

// WithRequestID returns ctx carrying the request ID.
func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey{}, id)
}

// RequestID returns the request ID stored by WithRequestID, or "".
func RequestID(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey{}).(string)
	return id
}

// WithLogFields returns ctx carrying a fresh LogFields, and the pointer so
// the caller can read the values handlers set later.
func WithLogFields(ctx context.Context) (context.Context, *LogFields) {
	f := &LogFields{}
	return context.WithValue(ctx, logFieldsKey{}, f), f
}

// Fields returns the LogFields stored by WithLogFields, or nil when absent.
func Fields(ctx context.Context) *LogFields {
	f, _ := ctx.Value(logFieldsKey{}).(*LogFields)
	return f
}

type clientIPKey struct{}

// WithClientIP returns ctx carrying the resolved client IP.
func WithClientIP(ctx context.Context, ip netip.Addr) context.Context {
	return context.WithValue(ctx, clientIPKey{}, ip)
}

// ClientIP returns the IP stored by WithClientIP, or the zero Addr.
func ClientIP(ctx context.Context) netip.Addr {
	ip, _ := ctx.Value(clientIPKey{}).(netip.Addr)
	return ip
}
