package workers

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/riverqueue/river"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	db "github.com/dashkan/pivox/internal/db/generated"
	"github.com/dashkan/pivox/internal/testutil/mocks"
)

// TestPurgeOrgsWorker_Work pins the post-River replacement for the
// old PurgeWorker.processBatch test. River drives invocation via
// scheduled periodic jobs; the Work() method does the same SQL the
// old worker did, just shaped as a river.Worker handler.
func TestPurgeOrgsWorker_Work_PurgesEachOrgPastPurgeTime(t *testing.T) {
	q := new(mocks.MockQuerier)
	orgA := db.Organization{ID: uuid.MustParse("0192a000-aaaa-7000-8000-000000000001"), Name: "acme"}
	orgB := db.Organization{ID: uuid.MustParse("0192a000-bbbb-7000-8000-000000000002"), Name: "beta"}
	q.On("ListOrgsPastPurgeTime", mock.Anything).Return([]db.Organization{orgA, orgB}, nil)
	q.On("PurgeExpiredOrganization", mock.Anything, orgA.ID).Return(nil)
	q.On("PurgeExpiredOrganization", mock.Anything, orgB.ID).Return(nil)

	w := &PurgeOrgsWorker{Queries: q, Logger: silentLogger()}
	err := w.Work(context.Background(), &river.Job[PurgeOrgsArgs]{Args: PurgeOrgsArgs{}})
	require.NoError(t, err)
	q.AssertExpectations(t)
}

func TestPurgeOrgsWorker_Work_NoopWhenEmpty(t *testing.T) {
	q := new(mocks.MockQuerier)
	q.On("ListOrgsPastPurgeTime", mock.Anything).Return([]db.Organization{}, nil)

	w := &PurgeOrgsWorker{Queries: q, Logger: silentLogger()}
	require.NoError(t, w.Work(context.Background(), &river.Job[PurgeOrgsArgs]{Args: PurgeOrgsArgs{}}))
	q.AssertNotCalled(t, "PurgeExpiredOrganization", mock.Anything, mock.Anything)
}

// TestPurgeOrgsWorker_Work_OneFailureDoesNotBlockOthers preserves
// the old PurgeWorker's "stuck row doesn't stall the batch" behavior.
// The next periodic tick will retry the failure; meanwhile every
// other purge-eligible org should still complete on this tick.
func TestPurgeOrgsWorker_Work_OneFailureDoesNotBlockOthers(t *testing.T) {
	q := new(mocks.MockQuerier)
	orgA := db.Organization{ID: uuid.MustParse("0192a000-aaaa-7000-8000-000000000001"), Name: "acme"}
	orgB := db.Organization{ID: uuid.MustParse("0192a000-bbbb-7000-8000-000000000002"), Name: "beta"}
	q.On("ListOrgsPastPurgeTime", mock.Anything).Return([]db.Organization{orgA, orgB}, nil)
	q.On("PurgeExpiredOrganization", mock.Anything, orgA.ID).Return(errors.New("FK violation"))
	q.On("PurgeExpiredOrganization", mock.Anything, orgB.ID).Return(nil)

	w := &PurgeOrgsWorker{Queries: q, Logger: silentLogger()}
	require.NoError(t, w.Work(context.Background(), &river.Job[PurgeOrgsArgs]{Args: PurgeOrgsArgs{}}))
	q.AssertExpectations(t)
}

// TestPurgeOrgsWorker_Work_ListErrorReturned: list-time failures
// surface from Work() so River records the job as errored and
// retries on its own schedule. The old worker swallowed it via the
// loop helper; River's retry semantics replace that.
func TestPurgeOrgsWorker_Work_ListErrorReturned(t *testing.T) {
	q := new(mocks.MockQuerier)
	q.On("ListOrgsPastPurgeTime", mock.Anything).Return([]db.Organization{}, errors.New("db down"))

	w := &PurgeOrgsWorker{Queries: q, Logger: silentLogger()}
	err := w.Work(context.Background(), &river.Job[PurgeOrgsArgs]{Args: PurgeOrgsArgs{}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "db down")
}

// TestPurgeOrgsArgs_Kind locks the job kind string. River uses Kind
// to dispatch jobs to handlers; changing it without coordinating a
// migration would orphan in-flight rows. The string is part of the
// on-disk contract.
func TestPurgeOrgsArgs_Kind(t *testing.T) {
	assert.Equal(t, "purge_orgs", PurgeOrgsArgs{}.Kind())
}
