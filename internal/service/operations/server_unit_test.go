package operations

import (
	"context"
	"log/slog"
	"os"
	"testing"

	"cloud.google.com/go/longrunning/autogen/longrunningpb"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	db "github.com/dashkan/pivox/internal/db/generated"
	"github.com/dashkan/pivox/internal/lro"
	"github.com/dashkan/pivox/internal/testutil/mocks"
)

// newTestServer creates an OperationsServer backed by a real lro.Manager
// with a mock Querier but nil pool. This is sufficient for input validation
// tests that fail before the Manager touches the pool.
func newTestServer() *OperationsServer {
	mockQ := new(mocks.MockQuerier)
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	manager := lro.NewManager(mockQ, logger)
	return NewOperationsServer(manager)
}

func TestUnit_GetOperation_EmptyName(t *testing.T) {
	srv := newTestServer()
	ctx := context.Background()

	_, err := srv.GetOperation(ctx, &longrunningpb.GetOperationRequest{
		Name: "",
	})

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
	assert.Contains(t, st.Message(), "name is required")
}

func TestUnit_DeleteOperation_EmptyName(t *testing.T) {
	srv := newTestServer()
	ctx := context.Background()

	_, err := srv.DeleteOperation(ctx, &longrunningpb.DeleteOperationRequest{
		Name: "",
	})

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
	assert.Contains(t, st.Message(), "name is required")
}

func TestUnit_CancelOperation_EmptyName(t *testing.T) {
	srv := newTestServer()
	ctx := context.Background()

	_, err := srv.CancelOperation(ctx, &longrunningpb.CancelOperationRequest{
		Name: "",
	})

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
	assert.Contains(t, st.Message(), "name is required")
}

func TestUnit_ListOperations_DefaultPageSize(t *testing.T) {
	// This test validates the server's default page size logic.
	// The server sets pageSize=100 when the request has pageSize<=0,
	// then delegates to lro.Manager.ListOperations.
	// We verify the server code path by checking it does not error
	// on a zero page size.
	mockQ := new(mocks.MockQuerier)
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	manager := lro.NewManager(mockQ, logger)
	srv := NewOperationsServer(manager)
	ctx := context.Background()

	// The Manager's ListOperations will call queries.ListOperations.
	// Since the server defaults pageSize to 100 and the Manager then also
	// clamps, we mock at the querier level.
	mockQ.On("ListOperations", ctx, mock.Anything).Return([]db.Operation{}, nil)

	resp, err := srv.ListOperations(ctx, &longrunningpb.ListOperationsRequest{
		Name:     "",
		PageSize: 0,
	})

	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Empty(t, resp.GetOperations())
	mockQ.AssertExpectations(t)
}
