package healthprobe_test

import (
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/XYZLZ/MyPark/apps/server/internal/transport/http/healthprobe"
)

func TestProbe(t *testing.T) {
	for _, tc := range []struct {
		status  int
		wantErr bool
	}{
		{http.StatusOK, false},
		{http.StatusNoContent, false},
		{http.StatusServiceUnavailable, true},
		{http.StatusNotFound, true},
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(tc.status)
		}))
		err := healthprobe.Probe(srv.URL + "/readyz")
		srv.Close()
		if (err != nil) != tc.wantErr {
			t.Errorf("status %d: Probe error = %v, wantErr %v", tc.status, err, tc.wantErr)
		}
	}
}

func TestProbe_Unreachable(t *testing.T) {
	// Reserve a port, then free it so nothing listens there.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	ln.Close()
	if err := healthprobe.Probe("http://" + addr + "/healthz"); err == nil {
		t.Fatal("Probe of a closed port returned nil, want an error")
	}
}

func TestLocalURL(t *testing.T) {
	for _, tc := range []struct {
		addr, want string
	}{
		{":8080", "http://127.0.0.1:8080/readyz"},
		{"0.0.0.0:8091", "http://127.0.0.1:8091/readyz"},
		{"[::]:8090", "http://127.0.0.1:8090/readyz"},
		{"127.0.0.1:41234", "http://127.0.0.1:41234/readyz"},
	} {
		got, err := healthprobe.LocalURL(tc.addr, "/readyz")
		if err != nil || got != tc.want {
			t.Errorf("LocalURL(%q) = %q, %v; want %q", tc.addr, got, err, tc.want)
		}
	}
	for _, addr := range []string{"nope", ":0", ""} {
		if got, err := healthprobe.LocalURL(addr, "/readyz"); err == nil {
			t.Errorf("LocalURL(%q) = %q, want an error", addr, got)
		}
	}
}
