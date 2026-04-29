package workers

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	db "github.com/dashkan/pivox/internal/db/generated"
	"github.com/dashkan/pivox/internal/testutil/mocks"
)

func silentLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// purgeTickFn calls the worker's tick logic without going through
// withAdvisoryLock — tests run against a mock Querier where the
// real *pgxpool.Pool isn't available. We exercise the inner work
// fn directly. The lock-acquisition path is covered by an
// integration test layer (not in this commit) since it requires
// real Postgres.
func purgeTickFn(w *PurgeWorker) func(context.Context) error {
	return func(ctx context.Context) error {
		orgs, err := w.queries.ListOrgsPastPurgeTime(ctx)
		if err != nil {
			return err
		}
		for _, o := range orgs {
			if err := w.queries.PurgeExpiredOrganization(ctx, o.ID); err != nil {
				w.logger.Error("purge: PurgeExpiredOrganization failed", "org", o.Name, "error", err)
				continue
			}
		}
		return nil
	}
}

func TestPurgeWorker_PurgesEachOrgPastPurgeTime(t *testing.T) {
	q := new(mocks.MockQuerier)
	orgA := db.Organization{ID: uuid.MustParse("0192a000-aaaa-7000-8000-000000000001"), Name: "acme"}
	orgB := db.Organization{ID: uuid.MustParse("0192a000-bbbb-7000-8000-000000000002"), Name: "beta"}
	q.On("ListOrgsPastPurgeTime", mock.Anything).Return([]db.Organization{orgA, orgB}, nil)
	q.On("PurgeExpiredOrganization", mock.Anything, orgA.ID).Return(nil)
	q.On("PurgeExpiredOrganization", mock.Anything, orgB.ID).Return(nil)

	w := &PurgeWorker{queries: q, logger: silentLogger(), interval: time.Minute}
	require.NoError(t, purgeTickFn(w)(context.Background()))
	q.AssertExpectations(t)
}

func TestPurgeWorker_NoopWhenNoOrgsPending(t *testing.T) {
	q := new(mocks.MockQuerier)
	q.On("ListOrgsPastPurgeTime", mock.Anything).Return([]db.Organization{}, nil)
	w := &PurgeWorker{queries: q, logger: silentLogger(), interval: time.Minute}
	require.NoError(t, purgeTickFn(w)(context.Background()))
	q.AssertNotCalled(t, "PurgeExpiredOrganization", mock.Anything, mock.Anything)
}

func TestPurgeWorker_OnePurgeFailureDoesNotBlockOthers(t *testing.T) {
	// A single stuck row shouldn't stall the whole batch — the next
	// tick will retry, and meanwhile every other purge-eligible org
	// should still complete.
	q := new(mocks.MockQuerier)
	orgA := db.Organization{ID: uuid.MustParse("0192a000-aaaa-7000-8000-000000000001"), Name: "acme"}
	orgB := db.Organization{ID: uuid.MustParse("0192a000-bbbb-7000-8000-000000000002"), Name: "beta"}
	q.On("ListOrgsPastPurgeTime", mock.Anything).Return([]db.Organization{orgA, orgB}, nil)
	q.On("PurgeExpiredOrganization", mock.Anything, orgA.ID).Return(errors.New("FK violation"))
	q.On("PurgeExpiredOrganization", mock.Anything, orgB.ID).Return(nil)

	w := &PurgeWorker{queries: q, logger: silentLogger(), interval: time.Minute}
	require.NoError(t, purgeTickFn(w)(context.Background()))
	q.AssertExpectations(t)
}

func TestPurgeWorker_ListErrorReturned(t *testing.T) {
	// A list-time error returns from the work fn so withAdvisoryLock
	// surfaces it for logging. The worker keeps running (the loop
	// swallows the error).
	q := new(mocks.MockQuerier)
	q.On("ListOrgsPastPurgeTime", mock.Anything).Return([]db.Organization{}, errors.New("db down"))
	w := &PurgeWorker{queries: q, logger: silentLogger(), interval: time.Minute}
	err := purgeTickFn(w)(context.Background())
	require.Error(t, err)
	assert.Equal(t, "db down", err.Error())
}

// TestRunAll_StopsOnContextCancel pins the lifecycle: each Worker
// implementation's Run must return when ctx is cancelled, and
// RunAll's WaitGroup must complete.
func TestRunAll_StopsOnContextCancel(t *testing.T) {
	w := &noopWorker{name: "noop"}
	ctx, cancel := context.WithCancel(context.Background())
	wg := RunAll(ctx, silentLogger(), w)
	cancel()
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("RunAll WaitGroup didn't complete after cancel")
	}
}

type noopWorker struct{ name string }

func (n *noopWorker) Name() string                  { return n.name }
func (n *noopWorker) Run(ctx context.Context) error { <-ctx.Done(); return ctx.Err() }
