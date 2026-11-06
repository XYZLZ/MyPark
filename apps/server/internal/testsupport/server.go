package testsupport

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

// startTimeout bounds how long StartServer waits for the listening log line.
const startTimeout = 5 * time.Second

// SyncBuffer is a goroutine-safe io.Writer for capturing the log output of
// a command's run function while it is running.
type SyncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *SyncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *SyncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// RunFunc is the signature shared by the run functions of cmd/api,
// cmd/worker and cmd/simulator.
type RunFunc func(ctx context.Context, getenv func(string) string, stdout io.Writer) error

// StartServer starts run with env in the background and returns the base
// URL ("http://host:port") taken from the JSON log line whose msg is
// listeningMsg. When the test ends, run is canceled and must return nil.
func StartServer(t *testing.T, run RunFunc, env map[string]string, listeningMsg string) string {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	var out SyncBuffer
	done := make(chan error, 1)
	go func() { done <- run(ctx, func(k string) string { return env[k] }, &out) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("run returned %v, want nil after shutdown", err)
			}
		case <-time.After(15 * time.Second):
			t.Error("run did not return after cancel")
		}
	})

	deadline := time.Now().Add(startTimeout)
	for {
		for line := range strings.Lines(out.String()) {
			var entry struct {
				Msg  string `json:"msg"`
				Addr string `json:"addr"`
			}
			if json.Unmarshal([]byte(line), &entry) == nil && entry.Msg == listeningMsg {
				return "http://" + entry.Addr
			}
		}
		select {
		case err := <-done:
			done <- err // let the cleanup see that run has returned
			t.Fatalf("run returned early: %v; output: %s", err, out.String())
		default:
		}
		if time.Now().After(deadline) {
			t.Fatalf("server did not log %q; output: %s", listeningMsg, out.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
}
