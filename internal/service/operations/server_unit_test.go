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
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"

	db "github.com/dashkan/pivox/internal/db/generated"
	"github.com/dashkan/pivox/internal/lro"
	"github.com/dashkan/pivox/internal/testutil/mocks"
)

// assertNameRequiredViolation checks that err is an InvalidArgument
// status carrying a typed BadRequest detail with a field-level
// "name is required" violation on the `name` field. This is the
// AIP-193 shape produced by apierr.InvalidArgument(FieldViolation(...))
// — assert on the typed details, not the wire message string.
func assertNameRequiredViolation(t *testing.T, err error) {
	t.Helper()
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
	var found bool
	for _, d := range st.Details() {
		br, ok := d.(*errdetails.BadRequest)
		if !ok {
			continue
		}
		for _, fv := range br.GetFieldViolations() {
			if fv.GetField() == "name" && fv.GetDescription() == "name is required" {
				found = true
			}
		}
	}
	assert.True(t, found, "expected BadRequest detail with field=name, description='name is required'; got details: %+v", st.Details())
}

// --- Mock LROManager ---

type mockLROManager struct {
	mock.Mock
}

func (m *mockLROManager) GetOperation(ctx context.Context, name string) (*longrunningpb.Operation, error) {
	args := m.Called(ctx, name)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*longrunningpb.Operation), args.Error(1)
}

func (m *mockLROManager) ListOperations(ctx context.Context, prefix string, pageSize int32) ([]*longrunningpb.Operation, error) {
	args := m.Called(ctx, prefix, pageSize)
	return args.Get(0).([]*longrunningpb.Operation), args.Error(1)
}

func (m *mockLROManager) WaitOperation(ctx context.Context, name string) (*longrunningpb.Operation, error) {
	args := m.Called(ctx, name)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*longrunningpb.Operation), args.Error(1)
}

func (m *mockLROManager) DeleteOperation(ctx context.Context, name string) error {
	args := m.Called(ctx, name)
	return args.Error(0)
}

func (m *mockLROManager) CancelOperation(ctx context.Context, name string) error {
	args := m.Called(ctx, name)
	return args.Error(0)
}

// newTestServer creates an OperationsServer backed by a real lro.Manager
// with a mock Querier but nil pool. This is sufficient for input validation
// tests that fail before the Manager touches the pool.
func newTestServer() *OperationsServer {
	mockQ := new(mocks.MockQuerier)
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	manager := lro.NewManager(mockQ, logger)
	return NewOperationsServer(Config{LRO: manager})
}

func TestUnit_GetOperation_EmptyName(t *testing.T) {
	srv := newTestServer()
	ctx := context.Background()

	_, err := srv.GetOperation(ctx, &longrunningpb.GetOperationRequest{
		Name: "",
	})

	require.Error(t, err)
	assertNameRequiredViolation(t, err)
}

func TestUnit_GetOperation_Success(t *testing.T) {
	mgr := new(mockLROManager)
	srv := NewOperationsServer(Config{LRO: mgr})
	ctx := context.Background()

	op := &longrunningpb.Operation{Name: "operations/test/123", Done: true}
	mgr.On("GetOperation", mock.Anything, "operations/test/123").Return(op, nil)

	resp, err := srv.GetOperation(ctx, &longrunningpb.GetOperationRequest{
		Name: "operations/test/123",
	})

	require.NoError(t, err)
	assert.Equal(t, "operations/test/123", resp.GetName())
	assert.True(t, resp.GetDone())
	mgr.AssertExpectations(t)
}

func TestUnit_ListOperations_WithResults(t *testing.T) {
	mgr := new(mockLROManager)
	srv := NewOperationsServer(Config{LRO: mgr})
	ctx := context.Background()

	ops := []*longrunningpb.Operation{
		{Name: "operations/a/1", Done: true},
		{Name: "operations/a/2", Done: false},
	}
	mgr.On("ListOperations", mock.Anything, "operations/a", int32(10)).Return(ops, nil)

	resp, err := srv.ListOperations(ctx, &longrunningpb.ListOperationsRequest{
		Name:     "operations/a",
		PageSize: 10,
	})

	require.NoError(t, err)
	assert.Len(t, resp.GetOperations(), 2)
	mgr.AssertExpectations(t)
}

func TestUnit_WaitOperation_Success(t *testing.T) {
	mgr := new(mockLROManager)
	srv := NewOperationsServer(Config{LRO: mgr})
	ctx := context.Background()

	op := &longrunningpb.Operation{Name: "operations/test/456", Done: true}
	mgr.On("WaitOperation", mock.Anything, "operations/test/456").Return(op, nil)

	resp, err := srv.WaitOperation(ctx, &longrunningpb.WaitOperationRequest{
		Name: "operations/test/456",
	})

	require.NoError(t, err)
	assert.True(t, resp.GetDone())
	mgr.AssertExpectations(t)
}

func TestUnit_WaitOperation_WithTimeout(t *testing.T) {
	mgr := new(mockLROManager)
	srv := NewOperationsServer(Config{LRO: mgr})
	ctx := context.Background()

	// When timeout is set, the server creates a derived context with deadline.
	// The mock returns a pending operation.
	pending := &longrunningpb.Operation{Name: "operations/test/789", Done: false}
	mgr.On("WaitOperation", mock.Anything, "operations/test/789").Return(pending, nil)

	resp, err := srv.WaitOperation(ctx, &longrunningpb.WaitOperationRequest{
		Name:    "operations/test/789",
		Timeout: durationpb.New(1000000), // 1ms
	})

	require.NoError(t, err)
	assert.False(t, resp.GetDone())
	mgr.AssertExpectations(t)
}

func TestUnit_DeleteOperation_Success(t *testing.T) {
	mgr := new(mockLROManager)
	srv := NewOperationsServer(Config{LRO: mgr})
	ctx := context.Background()

	mgr.On("DeleteOperation", mock.Anything, "operations/test/del").Return(nil)

	resp, err := srv.DeleteOperation(ctx, &longrunningpb.DeleteOperationRequest{
		Name: "operations/test/del",
	})

	require.NoError(t, err)
	assert.NotNil(t, resp)
	mgr.AssertExpectations(t)
}

func TestUnit_CancelOperation_Success(t *testing.T) {
	mgr := new(mockLROManager)
	srv := NewOperationsServer(Config{LRO: mgr})
	ctx := context.Background()

	mgr.On("CancelOperation", mock.Anything, "operations/test/cancel").Return(nil)

	resp, err := srv.CancelOperation(ctx, &longrunningpb.CancelOperationRequest{
		Name: "operations/test/cancel",
	})

	require.NoError(t, err)
	assert.NotNil(t, resp)
	mgr.AssertExpectations(t)
}

func TestUnit_DeleteOperation_EmptyName(t *testing.T) {
	srv := newTestServer()
	ctx := context.Background()

	_, err := srv.DeleteOperation(ctx, &longrunningpb.DeleteOperationRequest{
		Name: "",
	})

	require.Error(t, err)
	assertNameRequiredViolation(t, err)
}

func TestUnit_CancelOperation_EmptyName(t *testing.T) {
	srv := newTestServer()
	ctx := context.Background()

	_, err := srv.CancelOperation(ctx, &longrunningpb.CancelOperationRequest{
		Name: "",
	})

	require.Error(t, err)
	assertNameRequiredViolation(t, err)
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
	srv := NewOperationsServer(Config{LRO: manager})
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
