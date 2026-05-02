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

func TestPurgeSpacesWorker_Work_PurgesEachSpacePastPurgeTime(t *testing.T) {
	q := new(mocks.MockQuerier)
	a := db.Space{ID: uuid.MustParse("0192a000-aaaa-7000-8000-000000000001"), Name: "spaces/aa"}
	b := db.Space{ID: uuid.MustParse("0192a000-bbbb-7000-8000-000000000002"), Name: "spaces/bb"}
	q.On("ListSpacesPastPurgeTime", mock.Anything).Return([]db.Space{a, b}, nil)
	q.On("PurgeExpiredSpace", mock.Anything, a.ID).Return(nil)
	q.On("PurgeExpiredSpace", mock.Anything, b.ID).Return(nil)

	w := &PurgeSpacesWorker{Queries: q, Logger: silentLogger()}
	require.NoError(t, w.Work(context.Background(), &river.Job[PurgeSpacesArgs]{Args: PurgeSpacesArgs{}}))
	q.AssertExpectations(t)
}

func TestPurgeSpacesWorker_Work_NoopWhenEmpty(t *testing.T) {
	q := new(mocks.MockQuerier)
	q.On("ListSpacesPastPurgeTime", mock.Anything).Return([]db.Space{}, nil)

	w := &PurgeSpacesWorker{Queries: q, Logger: silentLogger()}
	require.NoError(t, w.Work(context.Background(), &river.Job[PurgeSpacesArgs]{Args: PurgeSpacesArgs{}}))
	q.AssertNotCalled(t, "PurgeExpiredSpace", mock.Anything, mock.Anything)
}

func TestPurgeSpacesWorker_Work_OneFailureDoesNotBlockOthers(t *testing.T) {
	q := new(mocks.MockQuerier)
	a := db.Space{ID: uuid.MustParse("0192a000-aaaa-7000-8000-000000000001"), Name: "spaces/aa"}
	b := db.Space{ID: uuid.MustParse("0192a000-bbbb-7000-8000-000000000002"), Name: "spaces/bb"}
	q.On("ListSpacesPastPurgeTime", mock.Anything).Return([]db.Space{a, b}, nil)
	q.On("PurgeExpiredSpace", mock.Anything, a.ID).Return(errors.New("FK violation"))
	q.On("PurgeExpiredSpace", mock.Anything, b.ID).Return(nil)

	w := &PurgeSpacesWorker{Queries: q, Logger: silentLogger()}
	require.NoError(t, w.Work(context.Background(), &river.Job[PurgeSpacesArgs]{Args: PurgeSpacesArgs{}}))
	q.AssertExpectations(t)
}

func TestPurgeSpacesWorker_Work_ListErrorReturned(t *testing.T) {
	q := new(mocks.MockQuerier)
	q.On("ListSpacesPastPurgeTime", mock.Anything).Return([]db.Space{}, errors.New("db down"))

	w := &PurgeSpacesWorker{Queries: q, Logger: silentLogger()}
	err := w.Work(context.Background(), &river.Job[PurgeSpacesArgs]{Args: PurgeSpacesArgs{}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "db down")
}

func TestPurgeSpacesArgs_Kind(t *testing.T) {
	assert.Equal(t, "purge_spaces", PurgeSpacesArgs{}.Kind())
}
