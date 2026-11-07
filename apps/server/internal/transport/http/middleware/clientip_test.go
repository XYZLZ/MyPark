package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"

	"github.com/XYZLZ/MyPark/apps/server/internal/transport/http/middleware"
)

func resolveIP(t *testing.T, trusted []netip.Prefix, remote, xff string) netip.Addr {
	t.Helper()
	var got netip.Addr
	h := middleware.ClientIP(trusted)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got = middleware.ClientIPFrom(r.Context())
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = remote
	if xff != "" {
		req.Header.Set("X-Forwarded-For", xff)
	}
	h.ServeHTTP(httptest.NewRecorder(), req)
	return got
}

var proxyNet = []netip.Prefix{netip.MustParsePrefix("172.16.0.0/12")}

func TestClientIP_UntrustedPeerIgnoresXFF(t *testing.T) {
	got := resolveIP(t, proxyNet, "203.0.113.9:5", "1.2.3.4")
	if got != netip.MustParseAddr("203.0.113.9") {
		t.Fatalf("ip = %v", got)
	}
}

func TestClientIP_TrustedPeerUsesRightmostUntrusted(t *testing.T) {
	got := resolveIP(t, proxyNet, "172.18.0.5:9", "1.2.3.4, 198.51.100.7, 172.18.0.9")
	if got != netip.MustParseAddr("198.51.100.7") {
		t.Fatalf("ip = %v", got)
	}
}

func TestClientIP_SpoofedLeftmostIgnored(t *testing.T) {
	got := resolveIP(t, proxyNet, "172.18.0.5:9", "6.6.6.6, 198.51.100.7")
	if got != netip.MustParseAddr("198.51.100.7") {
		t.Fatalf("ip = %v", got)
	}
}

func TestClientIP_GarbageXFFFallsBackToPeer(t *testing.T) {
	got := resolveIP(t, proxyNet, "172.18.0.5:9", "198.51.100.7, not-an-ip")
	if got != netip.MustParseAddr("172.18.0.5") {
		t.Fatalf("ip = %v", got)
	}
}
