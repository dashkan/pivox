package workers

import (
	"context"
	"errors"
	"testing"

	"github.com/riverqueue/river"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/dashkan/pivox/internal/testutil/mocks"
)

// TestReapOperationsWorker_Work pins the SQL contract of the
// pre-River lro.Reaper: every tick calls DeleteExpiredOperations
// once, no more no less.
func TestReapOperationsWorker_Work_DeletesExpiredOperations(t *testing.T) {
	q := new(mocks.MockQuerier)
	q.On("DeleteExpiredOperations", mock.Anything).Return(nil)

	w := &ReapOperationsWorker{Queries: q, Logger: silentLogger()}
	require.NoError(t, w.Work(context.Background(), &river.Job[ReapOperationsArgs]{Args: ReapOperationsArgs{}}))
	q.AssertExpectations(t)
}

// TestReapOperationsWorker_Work_ErrorPropagates: list-time errors
// surface from Work() so River applies its retry schedule. The old
// reaper logged + swallowed; River's retry logic replaces that.
func TestReapOperationsWorker_Work_ErrorPropagates(t *testing.T) {
	q := new(mocks.MockQuerier)
	q.On("DeleteExpiredOperations", mock.Anything).Return(errors.New("db down"))

	w := &ReapOperationsWorker{Queries: q, Logger: silentLogger()}
	err := w.Work(context.Background(), &river.Job[ReapOperationsArgs]{Args: ReapOperationsArgs{}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "db down")
}

func TestReapOperationsArgs_Kind(t *testing.T) {
	assert.Equal(t, "reap_operations", ReapOperationsArgs{}.Kind())
}
