package jobs_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/jobs"
)

type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func TestJobs_PanicDoesNotKillRunner(t *testing.T) {
	var logs syncBuf
	log := slog.New(slog.NewJSONHandler(&logs, nil))
	ctx, cancel := context.WithCancel(t.Context())
	var panics, healthy atomic.Int32
	done := make(chan struct{})
	go func() {
		defer close(done)
		jobs.Run(ctx, log,
			jobs.Job{Name: "boom", Every: 5 * time.Millisecond, Run: func(context.Context) error {
				panics.Add(1)
				panic("kaboom secret@example.com")
			}},
			jobs.Job{Name: "fails", Every: 5 * time.Millisecond, Run: func(context.Context) error {
				healthy.Add(1)
				return errors.New("db down")
			}},
		)
	}()
	deadline := time.Now().Add(10 * time.Second)
	for panics.Load() < 3 || healthy.Load() < 3 {
		if time.Now().After(deadline) {
			t.Fatalf("jobs stopped: panics=%d fails=%d", panics.Load(), healthy.Load())
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
	out := logs.String()
	if !strings.Contains(out, `"job":"boom"`) || !strings.Contains(out, "panic") || !strings.Contains(out, "db down") {
		t.Errorf("panic or error not logged: %s", out)
	}
	if strings.Contains(out, "secret@example.com") {
		t.Errorf("panic value leaked into logs: %s", out)
	}
}

func TestJobs_RunsImmediatelyAndStopsOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	ran := make(chan struct{}, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		jobs.Run(ctx, slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)),
			jobs.Job{Name: "slow", Every: time.Hour, Run: func(context.Context) error {
				select {
				case ran <- struct{}{}:
				default:
				}
				return nil
			}})
	}()
	select {
	case <-ran:
	case <-time.After(5 * time.Second):
		t.Fatal("job did not run at start")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not stop")
	}
}
