// Package jobs runs the worker's periodic background jobs.
package jobs

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// Job is one periodic task. Run is called once at start and then every Every.
type Job struct {
	Name  string
	Every time.Duration
	Run   func(ctx context.Context) error
}

// Run starts one goroutine per job and blocks until ctx is canceled and every
// goroutine has returned. A job that errors or panics is logged and tried
// again on its next tick; neither stops the runner. Panic values are not
// logged, since they can carry user data.
func Run(ctx context.Context, log *slog.Logger, js ...Job) {
	var wg sync.WaitGroup
	for _, j := range js {
		wg.Go(func() { loop(ctx, log.With("job", j.Name), j) })
	}
	wg.Wait()
}

func loop(ctx context.Context, log *slog.Logger, j Job) {
	t := time.NewTicker(j.Every)
	defer t.Stop()
	for {
		if err := runOnce(ctx, j); err != nil && ctx.Err() == nil {
			log.Error("job failed", "error", err.Error())
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func runOnce(ctx context.Context, j Job) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic recovered (%T)", r)
		}
	}()
	return j.Run(ctx)
}
