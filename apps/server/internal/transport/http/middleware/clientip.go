package middleware

import (
	"context"
	"net"
	"net/http"
	"net/netip"
	"strings"

	"github.com/XYZLZ/MyPark/apps/server/internal/transport/http/reqctx"
)

// ClientIP resolves the caller IP and stores it in the request context
// (read it with ClientIPFrom). X-Forwarded-For is honored only when the TCP
// peer is inside trusted; the chain is then walked from the right and the
// first address not in trusted wins, so a client-supplied leftmost entry
// cannot spoof the result (OC-22). An unparsable header falls back to the
// peer. It is a per-route wrapper, not part of the global chain.
func ClientIP(trusted []netip.Prefix) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := resolveClientIP(r, trusted)
			next.ServeHTTP(w, r.WithContext(reqctx.WithClientIP(r.Context(), ip)))
		})
	}
}

// ClientIPFrom returns the IP stored by ClientIP, or the zero Addr.
func ClientIPFrom(ctx context.Context) netip.Addr { return reqctx.ClientIP(ctx) }

func resolveClientIP(r *http.Request, trusted []netip.Prefix) netip.Addr {
	peer := parsePeer(r.RemoteAddr)
	if !peer.IsValid() || !inPrefixes(trusted, peer) {
		return peer
	}
	var parts []string
	for _, v := range r.Header.Values("X-Forwarded-For") {
		parts = append(parts, strings.Split(v, ",")...)
	}
	if len(parts) == 0 {
		return peer
	}
	var leftmost netip.Addr
	for i := len(parts) - 1; i >= 0; i-- {
		a, err := netip.ParseAddr(strings.TrimSpace(parts[i]))
		if err != nil {
			return peer
		}
		a = a.Unmap()
		if !inPrefixes(trusted, a) {
			return a
		}
		leftmost = a
	}
	return leftmost // every hop is a trusted proxy
}

func parsePeer(remote string) netip.Addr {
	host, _, err := net.SplitHostPort(remote)
	if err != nil {
		host = remote
	}
	a, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}
	}
	return a.Unmap()
}

func inPrefixes(ps []netip.Prefix, a netip.Addr) bool {
	for _, p := range ps {
		if p.Contains(a) {
			return true
		}
	}
	return false
}
