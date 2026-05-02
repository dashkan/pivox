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

// TestCleanupAuthWorker_Work covers the dual-SQL behavior of the
// pre-River inline auth cleanup goroutine (cmd/pivox-cloud/main.go):
// every tick deletes expired auth_token_codes AND expired
// delegated_auth_sessions, and a failure on the first must not
// suppress the second.
func TestCleanupAuthWorker_Work_DeletesBothExpiredSets(t *testing.T) {
	q := new(mocks.MockQuerier)
	q.On("DeleteExpiredAuthTokenCodes", mock.Anything).Return(nil)
	q.On("DeleteExpiredDelegatedAuthSessions", mock.Anything).Return(nil)

	w := &CleanupAuthWorker{Queries: q, Logger: silentLogger()}
	require.NoError(t, w.Work(context.Background(), &river.Job[CleanupAuthArgs]{Args: CleanupAuthArgs{}}))
	q.AssertExpectations(t)
}

// TestCleanupAuthWorker_Work_TokenCodeFailureDoesNotBlockSessions:
// the two cleanups are independent; a failure on one shouldn't
// stop the other. Behavior preserved from the inline goroutine
// where each call's error was logged but didn't short-circuit.
func TestCleanupAuthWorker_Work_TokenCodeFailureDoesNotBlockSessions(t *testing.T) {
	q := new(mocks.MockQuerier)
	q.On("DeleteExpiredAuthTokenCodes", mock.Anything).Return(errors.New("token cleanup failed"))
	q.On("DeleteExpiredDelegatedAuthSessions", mock.Anything).Return(nil)

	w := &CleanupAuthWorker{Queries: q, Logger: silentLogger()}
	// Returns a non-nil error so River retries, but BOTH SQL paths
	// must have been attempted before returning.
	err := w.Work(context.Background(), &river.Job[CleanupAuthArgs]{Args: CleanupAuthArgs{}})
	require.Error(t, err)
	q.AssertExpectations(t)
}

func TestCleanupAuthWorker_Work_SessionFailureSurfaced(t *testing.T) {
	q := new(mocks.MockQuerier)
	q.On("DeleteExpiredAuthTokenCodes", mock.Anything).Return(nil)
	q.On("DeleteExpiredDelegatedAuthSessions", mock.Anything).Return(errors.New("session cleanup failed"))

	w := &CleanupAuthWorker{Queries: q, Logger: silentLogger()}
	err := w.Work(context.Background(), &river.Job[CleanupAuthArgs]{Args: CleanupAuthArgs{}})
	require.Error(t, err)
	q.AssertExpectations(t)
}

func TestCleanupAuthArgs_Kind(t *testing.T) {
	assert.Equal(t, "cleanup_auth", CleanupAuthArgs{}.Kind())
}
