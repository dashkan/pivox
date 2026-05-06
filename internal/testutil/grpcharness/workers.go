package grpcharness

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/stretchr/testify/require"
)

// StartRiverWorkers boots an in-process River client wired to the
// harness's pool, lets the caller register workers via add, then
// starts the client. The client is stopped via t.Cleanup. Returns
// the started client so callers that want rivertest helpers or
// direct job inspection can use it.
//
// Mirrors cmd/pivox-worker/main.go's wiring (same Schema, same
// driver, same default queue config) so test-side Work() executes
// against the same shape as production.
//
// Note: completion of a River-backed LRO does NOT signal
// LROManager listeners today (see internal/lro/manager.go's
// notifyListeners — there is no bridge from worker to in-process
// manager). WaitOperation calls that depend on these workers
// observe completion only after their context times out, then
// fall through to a final GetOperation read. Tests should size
// their WaitOperation context with that fall-through cost in mind.
func (h *Harness) StartRiverWorkers(t *testing.T, add func(rw *river.Workers)) *river.Client[pgx.Tx] {
	t.Helper()
	rw := river.NewWorkers()
	add(rw)
	c, err := river.NewClient(riverpgxv5.New(h.Pool), &river.Config{
		Logger:  SilentLogger(),
		Queues:  map[string]river.QueueConfig{river.QueueDefault: {MaxWorkers: 2}},
		Schema:  "river",
		Workers: rw,
	})
	require.NoError(t, err)
	require.NoError(t, c.Start(context.Background()))
	t.Cleanup(func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer stopCancel()
		_ = c.Stop(stopCtx)
	})
	return c
}
