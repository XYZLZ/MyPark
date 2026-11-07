package legal_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/XYZLZ/MyPark/apps/server/internal/features/legal"
	"github.com/XYZLZ/MyPark/apps/server/internal/testsupport"
)

// blockingRepo parks Current until release is closed and counts the calls.
type blockingRepo struct {
	started chan struct{}
	release chan struct{}
	calls   atomic.Int64
}

func (r *blockingRepo) Current(context.Context, time.Time) (legal.Versions, error) {
	if r.calls.Add(1) == 1 {
		close(r.started)
	}
	<-r.release
	return legal.Versions{Terms: legal.Document{Version: "v1"}, Privacy: legal.Document{Version: "v1"}}, nil
}

func TestVersions_CanceledCallerReturnsWhileRefreshInFlight(t *testing.T) {
	repo := &blockingRepo{started: make(chan struct{}), release: make(chan struct{})}
	svc := legal.NewService(repo, testsupport.NewFakeClock(t0), time.Minute)

	first := make(chan error, 1)
	go func() {
		_, err := svc.Versions(context.Background())
		first <- err
	}()
	<-repo.started

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	done := make(chan error, 1)
	go func() {
		_, err := svc.Versions(ctx)
		done <- err
	}()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("canceled caller err = %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a canceled caller stayed queued behind the in-flight refresh")
	}

	close(repo.release)
	if err := <-first; err != nil {
		t.Fatalf("first caller: %v", err)
	}
	if got, err := svc.Versions(t.Context()); err != nil || got.Terms.Version != "v1" {
		t.Errorf("after refresh: %+v, %v, want the cached v1", got, err)
	}
	if n := repo.calls.Load(); n != 1 {
		t.Errorf("repo calls = %d, want 1 (shared refresh, then cached)", n)
	}
}

func TestVersions_ConcurrentCallersShareOneQuery(t *testing.T) {
	repo := &blockingRepo{started: make(chan struct{}), release: make(chan struct{})}
	svc := legal.NewService(repo, testsupport.NewFakeClock(t0), time.Minute)

	const callers = 8
	errs := make(chan error, callers)
	for range callers {
		go func() {
			_, err := svc.Versions(context.Background())
			errs <- err
		}()
	}
	<-repo.started
	close(repo.release)
	for range callers {
		if err := <-errs; err != nil {
			t.Fatalf("caller: %v", err)
		}
	}
	if n := repo.calls.Load(); n != 1 {
		t.Errorf("repo calls = %d, want 1", n)
	}
}
