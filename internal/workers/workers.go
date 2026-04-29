// Package workers hosts the in-process background workers that
// drive Pivox's deferred cascades — the org/user purge worker that
// runs after the 30-day soft-delete grace window, and the domain
// verification worker that ticks `CreateDomain` LROs to VERIFIED.
//
// Both workers are designed to run as goroutines inside the gRPC
// server process for v1, with dependencies (queries, dns resolver,
// logger, config) injected — never reaching into HTTP/gRPC server
// internals. Once the workload justifies splitting them into
// dedicated binaries (a `cmd/pivox-purge-worker/` etc.), the
// transition is a wiring change, not a refactor.
//
// Multi-replica safety: each worker takes a Postgres advisory lock
// before doing work, so deploying N replicas leaves at most one
// active worker per type at any moment. Replicas that lose the
// race silently skip the tick.
package workers

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"
)

// Worker is the common shape all background workers expose. Run is
// blocking — typically invoked in its own goroutine — and returns
// when the supplied ctx is cancelled. A non-nil error from Run is
// fatal for that worker's loop; transient per-tick errors are
// logged inside Run and don't surface.
type Worker interface {
	Name() string
	Run(ctx context.Context) error
}

// RunAll launches each worker on its own goroutine and returns a
// WaitGroup the caller can use to block on shutdown. Use it from
// main.go to start the worker fleet alongside the gRPC server.
func RunAll(ctx context.Context, logger *slog.Logger, ws ...Worker) *sync.WaitGroup {
	wg := &sync.WaitGroup{}
	for _, w := range ws {
		wg.Add(1)
		go func(w Worker) {
			defer wg.Done()
			logger.Info("worker: starting", "name", w.Name())
			if err := w.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
				logger.Error("worker: exited with error", "name", w.Name(), "error", err)
				return
			}
			logger.Info("worker: stopped", "name", w.Name())
		}(w)
	}
	return wg
}

// loop is the canonical worker tick body: do one pass of work, then
// wait for the next tick or ctx cancellation. Per-tick errors are
// logged inside `tick` and not propagated — the loop only exits on
// ctx cancellation. Used by both PurgeWorker and VerifyDomainWorker
// to keep their Run methods narrow.
func loop(ctx context.Context, logger *slog.Logger, name string, interval time.Duration, tick func(context.Context)) error {
	t := time.NewTicker(interval)
	defer t.Stop()
	// First tick fires immediately so a freshly-started replica
	// doesn't wait an interval before doing useful work.
	tick(ctx)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
			tick(ctx)
		}
	}
}
