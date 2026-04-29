package workers

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	db "github.com/dashkan/pivox/internal/db/generated"
	"github.com/dashkan/pivox/internal/testutil/mocks"
)

func TestSpacePurgeWorker_PurgesEachSpacePastPurgeTime(t *testing.T) {
	q := new(mocks.MockQuerier)
	spA := db.Space{ID: uuid.MustParse("0192a000-cccc-7000-8000-000000000001"), Name: "alpha"}
	spB := db.Space{ID: uuid.MustParse("0192a000-dddd-7000-8000-000000000002"), Name: "beta"}
	q.On("ListSpacesPastPurgeTime", mock.Anything).Return([]db.Space{spA, spB}, nil)
	q.On("PurgeExpiredSpace", mock.Anything, spA.ID).Return(nil)
	q.On("PurgeExpiredSpace", mock.Anything, spB.ID).Return(nil)

	w := &SpacePurgeWorker{queries: q, logger: silentLogger(), interval: time.Minute}
	require.NoError(t, w.processBatch(context.Background()))
	q.AssertExpectations(t)
}

func TestSpacePurgeWorker_NoopWhenNoSpacesPending(t *testing.T) {
	q := new(mocks.MockQuerier)
	q.On("ListSpacesPastPurgeTime", mock.Anything).Return([]db.Space{}, nil)
	w := &SpacePurgeWorker{queries: q, logger: silentLogger(), interval: time.Minute}
	require.NoError(t, w.processBatch(context.Background()))
	q.AssertNotCalled(t, "PurgeExpiredSpace", mock.Anything, mock.Anything)
}

// TestSpacePurgeWorker_OnePurgeFailureDoesNotBlockOthers mirrors the
// org-purge resilience test: a stuck row (e.g. a residual FK
// violation from a future child table without CASCADE) shouldn't
// stall the whole batch.
func TestSpacePurgeWorker_OnePurgeFailureDoesNotBlockOthers(t *testing.T) {
	q := new(mocks.MockQuerier)
	spA := db.Space{ID: uuid.MustParse("0192a000-cccc-7000-8000-000000000001"), Name: "alpha"}
	spB := db.Space{ID: uuid.MustParse("0192a000-dddd-7000-8000-000000000002"), Name: "beta"}
	q.On("ListSpacesPastPurgeTime", mock.Anything).Return([]db.Space{spA, spB}, nil)
	q.On("PurgeExpiredSpace", mock.Anything, spA.ID).Return(errors.New("FK violation"))
	q.On("PurgeExpiredSpace", mock.Anything, spB.ID).Return(nil)

	w := &SpacePurgeWorker{queries: q, logger: silentLogger(), interval: time.Minute}
	require.NoError(t, w.processBatch(context.Background()))
	q.AssertExpectations(t)
}

func TestSpacePurgeWorker_ListErrorReturned(t *testing.T) {
	q := new(mocks.MockQuerier)
	q.On("ListSpacesPastPurgeTime", mock.Anything).Return([]db.Space{}, errors.New("db down"))
	w := &SpacePurgeWorker{queries: q, logger: silentLogger(), interval: time.Minute}
	err := w.processBatch(context.Background())
	require.Error(t, err)
	assert.Equal(t, "db down", err.Error())
}
