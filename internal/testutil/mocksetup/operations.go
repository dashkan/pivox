package mocksetup

import (
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/mock"

	db "github.com/dashkan/pivox/internal/db/generated"
	"github.com/dashkan/pivox/internal/testutil/mocks"
)

// ExpectCreateOperationAny configures the mock to return `op` for
// any CreateOperation call. Use this when the test doesn't care
// about the exact params (most LRO-handler tests don't — they
// inspect the returned operation, not what was inserted).
func ExpectCreateOperationAny(q *mocks.MockQuerier, op db.Operation) {
	q.On("CreateOperation", mock.Anything, mock.Anything).Return(op, nil)
}

// ExpectCreateOperation configures the mock to return `op` for a
// specific CreateOperation params struct. Use when the test asserts
// the exact fields passed in (parent, metadata, etc.).
func ExpectCreateOperation(q *mocks.MockQuerier, params db.CreateOperationParams, op db.Operation) {
	q.On("CreateOperation", mock.Anything, params).Return(op, nil)
}

// ExpectGetOperation configures the mock to return `op` for
// GetOperation called with the given ID.
func ExpectGetOperation(q *mocks.MockQuerier, id uuid.UUID, op db.Operation) {
	q.On("GetOperation", mock.Anything, id).Return(op, nil)
}

// ExpectGetOperationNotFound configures the mock to return
// pgx.ErrNoRows for GetOperation with the given ID.
func ExpectGetOperationNotFound(q *mocks.MockQuerier, id uuid.UUID) {
	q.On("GetOperation", mock.Anything, id).Return(db.Operation{}, pgx.ErrNoRows)
}

// ExpectFailOperation configures the mock to return `op` for any
// FailOperation call. The returned op is the post-update row; tests
// that inspect the failure usually want this populated.
func ExpectFailOperation(q *mocks.MockQuerier, op db.Operation) {
	q.On("FailOperation", mock.Anything, mock.Anything).Return(op, nil)
}

// ExpectCompleteOperation configures the mock to return `op` for
// any CompleteOperation call.
func ExpectCompleteOperation(q *mocks.MockQuerier, op db.Operation) {
	q.On("CompleteOperation", mock.Anything, mock.Anything).Return(op, nil)
}
