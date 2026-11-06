// Package healthprobe implements the -healthcheck flag of the server
// binaries. The runtime image is distroless (no shell, no curl), so a Docker
// HEALTHCHECK runs the binary itself, which probes its own HTTP endpoint.
package healthprobe

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/XYZLZ/MyPark/apps/server/internal/config"
)

// Timeout bounds one probe, below Docker's default healthcheck timeout.
const Timeout = 3 * time.Second

// maxDrain caps how much of a response body is read before closing it.
const maxDrain = 4 << 10

var client = &http.Client{Timeout: Timeout}

// Probe sends GET url and returns nil only for a 2xx response.
func Probe(url string) error {
	resp, err := client.Get(url)
	if err != nil {
		return fmt.Errorf("healthcheck: %w", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxDrain))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("healthcheck: GET %s: status %d", url, resp.StatusCode)
	}
	return nil
}

// LocalURL returns the URL of path on the loopback interface at the port of
// the listen address addr (":8080", "0.0.0.0:8080", ...). The probe runs in
// the same container as the server, so it always dials 127.0.0.1, whatever
// host the server binds. Port 0 cannot be probed and is an error.
func LocalURL(addr, path string) (string, error) {
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "", fmt.Errorf("healthcheck: listen address %q: %w", addr, err)
	}
	if n, err := strconv.ParseUint(port, 10, 16); err != nil || n == 0 {
		return "", fmt.Errorf("healthcheck: listen address %q: port must be 1-65535", addr)
	}
	return "http://" + net.JoinHostPort("127.0.0.1", port) + path, nil
}

// Local probes path on the loopback port of the listen address held in the
// environment variable addrEnv (defaultAddr when unset). It reads nothing
// else, so the probe works without the server's other configuration.
func Local(getenv func(string) string, addrEnv, defaultAddr, path string) error {
	addr, err := config.ListenAddrFrom(getenv, addrEnv, defaultAddr)
	if err != nil {
		return err
	}
	url, err := LocalURL(addr, path)
	if err != nil {
		return err
	}
	return Probe(url)
}
