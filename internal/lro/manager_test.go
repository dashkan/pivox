package lro

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/structpb"

	db "github.com/dashkan/pivox/internal/db/generated"
	"github.com/dashkan/pivox/internal/testutil/mocks"
)

func newTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestNewManager(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	logger := newTestLogger()

	m := NewManager(mockQ, logger)
	require.NotNil(t, m)
	assert.NotNil(t, m.queries)
	assert.NotNil(t, m.logger)
	assert.NotNil(t, m.listeners)
}

func TestParseOperationName(t *testing.T) {
	validID := uuid.New()

	tests := []struct {
		name    string
		input   string
		wantID  uuid.UUID
		wantErr bool
	}{
		{
			name:   "operations/prefix/uuid",
			input:  fmt.Sprintf("operations/assets/%s", validID),
			wantID: validID,
		},
		{
			name:   "operations/uuid (no prefix)",
			input:  fmt.Sprintf("operations/%s", validID),
			wantID: validID,
		},
		{
			name:   "operations/nested/prefix/uuid",
			input:  fmt.Sprintf("operations/some/nested/%s", validID),
			wantID: validID,
		},
		{
			name:    "single segment",
			input:   "invalid",
			wantErr: true,
		},
		{
			name:    "bad uuid",
			input:   "operations/not-a-uuid",
			wantErr: true,
		},
		{
			name:    "empty string",
			input:   "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseOperationName(tt.input)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantID, got)
		})
	}
}

func TestGetOperation_Found(t *testing.T) {
	ctx := context.Background()
	mockQ := new(mocks.MockQuerier)
	m := NewManager(mockQ, newTestLogger())

	opID := uuid.New()
	dbOp := db.Operation{
		ID:     opID,
		Prefix: "assets",
		Done:   false,
	}
	mockQ.On("GetOperation", ctx, opID).Return(dbOp, nil)

	op, err := m.GetOperation(ctx, fmt.Sprintf("operations/assets/%s", opID))
	require.NoError(t, err)
	require.NotNil(t, op)
	assert.Equal(t, fmt.Sprintf("operations/assets/%s", opID), op.Name)
	assert.False(t, op.Done)
	mockQ.AssertExpectations(t)
}

func TestGetOperation_NotFound(t *testing.T) {
	ctx := context.Background()
	mockQ := new(mocks.MockQuerier)
	m := NewManager(mockQ, newTestLogger())

	opID := uuid.New()
	mockQ.On("GetOperation", ctx, opID).Return(db.Operation{}, pgx.ErrNoRows)

	_, err := m.GetOperation(ctx, fmt.Sprintf("operations/assets/%s", opID))
	require.Error(t, err)
	st := status.Convert(err)
	assert.Equal(t, codes.NotFound, st.Code())
	mockQ.AssertExpectations(t)
}

func TestGetOperation_InvalidName(t *testing.T) {
	ctx := context.Background()
	mockQ := new(mocks.MockQuerier)
	m := NewManager(mockQ, newTestLogger())

	_, err := m.GetOperation(ctx, "bad-name")
	require.Error(t, err)
	st := status.Convert(err)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

func TestListOperations_Success(t *testing.T) {
	ctx := context.Background()
	mockQ := new(mocks.MockQuerier)
	m := NewManager(mockQ, newTestLogger())

	op1 := db.Operation{ID: uuid.New(), Prefix: "assets", Done: false}
	op2 := db.Operation{ID: uuid.New(), Prefix: "assets", Done: true}

	mockQ.On("ListOperations", ctx, db.ListOperationsParams{
		Limit:        int32(50),
		PrefixFilter: pgtype.Text{String: "assets", Valid: true},
	}).Return([]db.Operation{op1, op2}, nil)

	ops, err := m.ListOperations(ctx, "assets", 50)
	require.NoError(t, err)
	assert.Len(t, ops, 2)
	mockQ.AssertExpectations(t)
}

func TestListOperations_Empty(t *testing.T) {
	ctx := context.Background()
	mockQ := new(mocks.MockQuerier)
	m := NewManager(mockQ, newTestLogger())

	mockQ.On("ListOperations", ctx, db.ListOperationsParams{
		Limit:        int32(100),
		PrefixFilter: pgtype.Text{},
	}).Return([]db.Operation{}, nil)

	ops, err := m.ListOperations(ctx, "", 0)
	require.NoError(t, err)
	assert.Empty(t, ops)
	mockQ.AssertExpectations(t)
}

func TestListOperations_PageSize(t *testing.T) {
	tests := []struct {
		name     string
		pageSize int32
		want     int32
	}{
		{"zero clamps to 100", 0, 100},
		{"negative clamps to 100", -5, 100},
		{"over 1000 clamps to 100", 1001, 100},
		{"valid size passes through", 50, 50},
		{"max valid size 1000", 1000, 1000},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			mockQ := new(mocks.MockQuerier)
			m := NewManager(mockQ, newTestLogger())

			mockQ.On("ListOperations", ctx, db.ListOperationsParams{
				Limit:        tt.want,
				PrefixFilter: pgtype.Text{},
			}).Return([]db.Operation{}, nil)

			_, err := m.ListOperations(ctx, "", tt.pageSize)
			require.NoError(t, err)
			mockQ.AssertExpectations(t)
		})
	}
}

func TestDeleteOperation_Done(t *testing.T) {
	ctx := context.Background()
	mockQ := new(mocks.MockQuerier)
	m := NewManager(mockQ, newTestLogger())

	opID := uuid.New()
	mockQ.On("GetOperation", ctx, opID).Return(db.Operation{
		ID:   opID,
		Done: true,
	}, nil)
	mockQ.On("DeleteOperation", ctx, opID).Return(nil)

	err := m.DeleteOperation(ctx, fmt.Sprintf("operations/assets/%s", opID))
	require.NoError(t, err)
	mockQ.AssertExpectations(t)
}

func TestDeleteOperation_Running(t *testing.T) {
	ctx := context.Background()
	mockQ := new(mocks.MockQuerier)
	m := NewManager(mockQ, newTestLogger())

	opID := uuid.New()
	mockQ.On("GetOperation", ctx, opID).Return(db.Operation{
		ID:   opID,
		Done: false,
	}, nil)

	err := m.DeleteOperation(ctx, fmt.Sprintf("operations/assets/%s", opID))
	require.Error(t, err)
	st := status.Convert(err)
	assert.Equal(t, codes.FailedPrecondition, st.Code())
	mockQ.AssertExpectations(t)
}

func TestDeleteOperation_NotFound(t *testing.T) {
	ctx := context.Background()
	mockQ := new(mocks.MockQuerier)
	m := NewManager(mockQ, newTestLogger())

	opID := uuid.New()
	mockQ.On("GetOperation", ctx, opID).Return(db.Operation{}, pgx.ErrNoRows)

	err := m.DeleteOperation(ctx, fmt.Sprintf("operations/assets/%s", opID))
	require.Error(t, err)
	st := status.Convert(err)
	assert.Equal(t, codes.NotFound, st.Code())
	mockQ.AssertExpectations(t)
}

func TestCancelOperation_Success(t *testing.T) {
	ctx := context.Background()
	mockQ := new(mocks.MockQuerier)
	m := NewManager(mockQ, newTestLogger())

	opID := uuid.New()
	mockQ.On("CancelOperation", ctx, opID).Return(db.Operation{
		ID:   opID,
		Done: true,
	}, nil)

	err := m.CancelOperation(ctx, fmt.Sprintf("operations/assets/%s", opID))
	require.NoError(t, err)
	mockQ.AssertExpectations(t)
}

func TestCancelOperation_AlreadyDone(t *testing.T) {
	ctx := context.Background()
	mockQ := new(mocks.MockQuerier)
	m := NewManager(mockQ, newTestLogger())

	opID := uuid.New()
	// CancelOperation returns ErrNoRows (no running op to cancel)
	mockQ.On("CancelOperation", ctx, opID).Return(db.Operation{}, pgx.ErrNoRows)
	// GetOperation finds the op (it exists but is done)
	mockQ.On("GetOperation", ctx, opID).Return(db.Operation{
		ID:   opID,
		Done: true,
	}, nil)

	err := m.CancelOperation(ctx, fmt.Sprintf("operations/assets/%s", opID))
	require.NoError(t, err, "cancelling an already-done op should return nil")
	mockQ.AssertExpectations(t)
}

func TestCancelOperation_NotFound(t *testing.T) {
	ctx := context.Background()
	mockQ := new(mocks.MockQuerier)
	m := NewManager(mockQ, newTestLogger())

	opID := uuid.New()
	mockQ.On("CancelOperation", ctx, opID).Return(db.Operation{}, pgx.ErrNoRows)
	mockQ.On("GetOperation", ctx, opID).Return(db.Operation{}, pgx.ErrNoRows)

	err := m.CancelOperation(ctx, fmt.Sprintf("operations/assets/%s", opID))
	require.Error(t, err)
	st := status.Convert(err)
	assert.Equal(t, codes.NotFound, st.Code())
	mockQ.AssertExpectations(t)
}

func TestRecoverPending_Success(t *testing.T) {
	ctx := context.Background()
	mockQ := new(mocks.MockQuerier)
	m := NewManager(mockQ, newTestLogger())

	op1 := db.Operation{ID: uuid.New(), Prefix: "a"}
	op2 := db.Operation{ID: uuid.New(), Prefix: "b"}

	mockQ.On("ListPendingOperations", ctx).Return([]db.Operation{op1, op2}, nil)
	mockQ.On("FailOperation", ctx, mock.MatchedBy(func(p db.FailOperationParams) bool {
		return p.ID == op1.ID && p.ErrorCode.Int32 == int32(codes.Aborted)
	})).Return(db.Operation{}, nil)
	mockQ.On("FailOperation", ctx, mock.MatchedBy(func(p db.FailOperationParams) bool {
		return p.ID == op2.ID && p.ErrorCode.Int32 == int32(codes.Aborted)
	})).Return(db.Operation{}, nil)

	err := m.RecoverPending(ctx)
	require.NoError(t, err)
	mockQ.AssertExpectations(t)
}

func TestRecoverPending_Empty(t *testing.T) {
	ctx := context.Background()
	mockQ := new(mocks.MockQuerier)
	m := NewManager(mockQ, newTestLogger())

	mockQ.On("ListPendingOperations", ctx).Return([]db.Operation{}, nil)

	err := m.RecoverPending(ctx)
	require.NoError(t, err)
	// FailOperation should never be called
	mockQ.AssertNotCalled(t, "FailOperation", mock.Anything, mock.Anything)
}

func TestCreateAndRun(t *testing.T) {
	ctx := context.Background()
	mockQ := new(mocks.MockQuerier)
	m := NewManager(mockQ, newTestLogger())

	mockQ.On("CreateOperation", ctx, mock.MatchedBy(func(p db.CreateOperationParams) bool {
		return p.Prefix == "assets"
	})).Return(db.Operation{
		ID:     uuid.New(),
		Prefix: "assets",
		Done:   false,
	}, nil)

	done := make(chan struct{})

	// The work func will signal completion. We also need to mock the CompleteOperation
	// that runWork will call.
	mockQ.On("CompleteOperation", mock.Anything, mock.Anything).Return(db.Operation{}, nil).Run(func(args mock.Arguments) {
		close(done)
	})

	s, err := structpb.NewStruct(map[string]interface{}{"key": "value"})
	require.NoError(t, err)

	op, err := m.CreateAndRun(ctx, "assets", nil, func(ctx context.Context, _ Progress) (proto.Message, error) {
		return s, nil
	})
	require.NoError(t, err)
	require.NotNil(t, op)
	assert.False(t, op.Done)
	assert.Contains(t, op.Name, "operations/assets/")

	select {
	case <-done:
		// work completed
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for work func to complete")
	}
}

func TestRunWork_Failure(t *testing.T) {
	ctx := context.Background()
	mockQ := new(mocks.MockQuerier)
	m := NewManager(mockQ, newTestLogger())

	mockQ.On("CreateOperation", ctx, mock.Anything).Return(db.Operation{Prefix: "assets", Done: false}, nil)

	failCalled := make(chan db.FailOperationParams, 1)
	mockQ.On("FailOperation", mock.Anything, mock.Anything).Return(db.Operation{}, nil).Run(func(args mock.Arguments) {
		failCalled <- args.Get(1).(db.FailOperationParams)
	})

	_, err := m.CreateAndRun(ctx, "assets", nil, func(ctx context.Context, _ Progress) (proto.Message, error) {
		return nil, fmt.Errorf("work failed")
	})
	require.NoError(t, err) // CreateAndRun itself should succeed

	select {
	case params := <-failCalled:
		assert.Equal(t, int32(codes.Internal), params.ErrorCode.Int32)
		assert.Equal(t, "work failed", params.ErrorMessage.String)
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for FailOperation call")
	}
}

func TestRunWork_GRPCStatusError(t *testing.T) {
	ctx := context.Background()
	mockQ := new(mocks.MockQuerier)
	m := NewManager(mockQ, newTestLogger())

	mockQ.On("CreateOperation", ctx, mock.Anything).Return(db.Operation{Prefix: "assets", Done: false}, nil)

	failCalled := make(chan db.FailOperationParams, 1)
	mockQ.On("FailOperation", mock.Anything, mock.Anything).Return(db.Operation{}, nil).Run(func(args mock.Arguments) {
		failCalled <- args.Get(1).(db.FailOperationParams)
	})

	_, err := m.CreateAndRun(ctx, "assets", nil, func(ctx context.Context, _ Progress) (proto.Message, error) {
		return nil, status.Error(codes.PermissionDenied, "access denied")
	})
	require.NoError(t, err)

	select {
	case params := <-failCalled:
		assert.Equal(t, int32(codes.PermissionDenied), params.ErrorCode.Int32)
		assert.Equal(t, "access denied", params.ErrorMessage.String)
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for FailOperation call")
	}
}

func TestRunWork_SuccessWithNilResult(t *testing.T) {
	ctx := context.Background()
	mockQ := new(mocks.MockQuerier)
	m := NewManager(mockQ, newTestLogger())

	mockQ.On("CreateOperation", ctx, mock.Anything).Return(db.Operation{Prefix: "assets", Done: false}, nil)

	completeCalled := make(chan db.CompleteOperationParams, 1)
	mockQ.On("CompleteOperation", mock.Anything, mock.Anything).Return(db.Operation{}, nil).Run(func(args mock.Arguments) {
		completeCalled <- args.Get(1).(db.CompleteOperationParams)
	})

	// Return nil result -- the success path with no result to marshal
	_, err := m.CreateAndRun(ctx, "assets", nil, func(ctx context.Context, _ Progress) (proto.Message, error) {
		return nil, nil
	})
	require.NoError(t, err)

	select {
	case params := <-completeCalled:
		// Result should be nil since we returned nil from work
		assert.Nil(t, params.Result)
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for CompleteOperation call")
	}
}

func TestDoneOperation(t *testing.T) {
	s, err := structpb.NewStruct(map[string]interface{}{"key": "value"})
	require.NoError(t, err)

	t.Run("valid proto message", func(t *testing.T) {
		op, err := DoneOperation(s)
		require.NoError(t, err)
		require.NotNil(t, op)
		assert.True(t, op.Done)
		assert.NotNil(t, op.GetResponse())
		assert.Contains(t, op.Name, "operations/")
	})
}

func TestMarshalAny_Valid(t *testing.T) {
	s, err := structpb.NewStruct(map[string]interface{}{"k": "v"})
	require.NoError(t, err)

	data, err := marshalAny(s)
	require.NoError(t, err)
	require.NotEmpty(t, data)

	var raw map[string]json.RawMessage
	err = json.Unmarshal(data, &raw)
	require.NoError(t, err)
	_, ok := raw["@type"]
	assert.True(t, ok, "expected @type key")
}

func TestMarshalAny_NilMessage(t *testing.T) {
	// anypb.New(nil) returns an error
	_, err := marshalAny(nil)
	require.Error(t, err)
}

func TestUnmarshalAny_Empty(t *testing.T) {
	a, err := unmarshalAny(nil)
	require.NoError(t, err)
	assert.Nil(t, a)

	a, err = unmarshalAny([]byte{})
	require.NoError(t, err)
	assert.Nil(t, a)
}

func TestDbToProto(t *testing.T) {
	tests := []struct {
		name      string
		op        db.Operation
		checkFunc func(t *testing.T, op interface{}, err error)
	}{
		{
			name: "pending operation",
			op: db.Operation{
				ID:     uuid.New(),
				Prefix: "assets",
				Done:   false,
			},
			checkFunc: func(t *testing.T, opRaw interface{}, err error) {
				require.NoError(t, err)
				// Type is already checked via function signature
			},
		},
		{
			name: "done with error",
			op: db.Operation{
				ID:           uuid.New(),
				Prefix:       "assets",
				Done:         true,
				ErrorCode:    pgtype.Int4{Int32: int32(codes.NotFound), Valid: true},
				ErrorMessage: pgtype.Text{String: "resource not found", Valid: true},
			},
			checkFunc: func(t *testing.T, _ interface{}, err error) {
				require.NoError(t, err)
			},
		},
		{
			name: "done with result",
			op: func() db.Operation {
				s, _ := structpb.NewStruct(map[string]interface{}{"k": "v"})
				data, _ := marshalAny(s)
				return db.Operation{
					ID:     uuid.New(),
					Prefix: "assets",
					Done:   true,
					Result: data,
				}
			}(),
			checkFunc: func(t *testing.T, _ interface{}, err error) {
				require.NoError(t, err)
			},
		},
		{
			name: "done with metadata",
			op: func() db.Operation {
				s, _ := structpb.NewStruct(map[string]interface{}{"progress": "50%"})
				data, _ := marshalAny(s)
				return db.Operation{
					ID:       uuid.New(),
					Prefix:   "assets",
					Done:     false,
					Metadata: data,
				}
			}(),
			checkFunc: func(t *testing.T, _ interface{}, err error) {
				require.NoError(t, err)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			op, err := dbToProto(tt.op)
			if tt.checkFunc != nil {
				tt.checkFunc(t, op, err)
			}
			if err == nil {
				require.NotNil(t, op)
				assert.Equal(t, fmt.Sprintf("operations/%s/%s", tt.op.Prefix, tt.op.ID), op.Name)
				assert.Equal(t, tt.op.Done, op.Done)

				if tt.op.Done && tt.op.ErrorCode.Valid && tt.op.ErrorCode.Int32 != 0 {
					assert.NotNil(t, op.GetError())
					assert.Equal(t, tt.op.ErrorCode.Int32, op.GetError().Code)
				}
				if tt.op.Done && len(tt.op.Result) > 0 {
					assert.NotNil(t, op.GetResponse())
				}
				if len(tt.op.Metadata) > 0 {
					assert.NotNil(t, op.Metadata)
				}
			}
		})
	}
}

func TestWaitOperation_AlreadyDone(t *testing.T) {
	ctx := context.Background()
	mockQ := new(mocks.MockQuerier)
	m := NewManager(mockQ, newTestLogger())

	opID := uuid.New()
	mockQ.On("GetOperation", ctx, opID).Return(db.Operation{
		ID:     opID,
		Prefix: "assets",
		Done:   true,
	}, nil)

	op, err := m.WaitOperation(ctx, fmt.Sprintf("operations/assets/%s", opID))
	require.NoError(t, err)
	assert.True(t, op.Done)
	mockQ.AssertExpectations(t)
}

func TestWaitOperation_WaitsForCompletion(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	m := NewManager(mockQ, newTestLogger())

	opID := uuid.New()
	opName := fmt.Sprintf("operations/assets/%s", opID)

	// First GetOperation call (in WaitOperation): not done.
	// After notification, second GetOperation call: done.
	firstCall := mockQ.On("GetOperation", mock.Anything, opID).Return(
		db.Operation{ID: opID, Prefix: "assets", Done: false}, nil,
	).Once()

	mockQ.On("GetOperation", mock.Anything, opID).Return(
		db.Operation{ID: opID, Prefix: "assets", Done: true}, nil,
	).NotBefore(firstCall)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	type waitResult struct {
		err error
	}
	resultCh := make(chan waitResult, 1)
	go func() {
		_, err := m.WaitOperation(ctx, opName)
		resultCh <- waitResult{err}
	}()

	// Give the goroutine time to register the listener
	time.Sleep(50 * time.Millisecond)

	// Notify listeners (simulating work completion)
	m.notifyListeners(opID)

	select {
	case result := <-resultCh:
		require.NoError(t, result.err)
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for WaitOperation to return")
	}
}

func TestWaitOperation_ContextCancelled(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	m := NewManager(mockQ, newTestLogger())

	opID := uuid.New()
	opName := fmt.Sprintf("operations/assets/%s", opID)

	mockQ.On("GetOperation", mock.Anything, opID).Return(db.Operation{
		ID:     opID,
		Prefix: "assets",
		Done:   false,
	}, nil)

	ctx, cancel := context.WithCancel(context.Background())

	resultCh := make(chan error, 1)
	go func() {
		_, err := m.WaitOperation(ctx, opName)
		resultCh <- err
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-resultCh:
		// When context is cancelled, WaitOperation calls GetOperation with background ctx
		// and returns whatever the state is (still not done in our mock).
		// No error is returned because the operation just isn't done yet.
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for WaitOperation to return after cancel")
	}
}

// --- Additional coverage tests ---

func TestCreateAndRun_WithMetadata(t *testing.T) {
	ctx := context.Background()
	mockQ := new(mocks.MockQuerier)
	m := NewManager(mockQ, newTestLogger())

	metadata, err := structpb.NewStruct(map[string]interface{}{"progress": "0%"})
	require.NoError(t, err)

	mockQ.On("CreateOperation", ctx, mock.MatchedBy(func(p db.CreateOperationParams) bool {
		return p.Prefix == "assets" && len(p.Metadata) > 0
	})).Return(db.Operation{
		ID:     uuid.New(),
		Prefix: "assets",
		Done:   false,
	}, nil)

	done := make(chan struct{})
	mockQ.On("CompleteOperation", mock.Anything, mock.Anything).Return(db.Operation{}, nil).Run(func(_ mock.Arguments) {
		close(done)
	})

	op, err := m.CreateAndRun(ctx, "assets", metadata, func(ctx context.Context, _ Progress) (proto.Message, error) {
		return nil, nil
	})
	require.NoError(t, err)
	require.NotNil(t, op)

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out")
	}
}

func TestCreateAndRun_CreateOperationError(t *testing.T) {
	ctx := context.Background()
	mockQ := new(mocks.MockQuerier)
	m := NewManager(mockQ, newTestLogger())

	mockQ.On("CreateOperation", ctx, mock.Anything).Return(db.Operation{}, fmt.Errorf("db down"))

	op, err := m.CreateAndRun(ctx, "assets", nil, func(ctx context.Context, _ Progress) (proto.Message, error) {
		return nil, nil
	})
	require.Error(t, err)
	assert.Nil(t, op)
	st := status.Convert(err)
	assert.Equal(t, codes.Internal, st.Code())
}

func TestRunWork_MarshalResultError(t *testing.T) {
	// To trigger marshal error in runWork, we call runWork directly with a work func
	// that returns a message causing marshalAny to fail. We use a bare proto.Message
	// interface implementation that isn't registered in the protobuf type registry.
	mockQ := new(mocks.MockQuerier)
	m := NewManager(mockQ, newTestLogger())

	opID := uuid.New()

	failCalled := make(chan db.FailOperationParams, 1)
	mockQ.On("FailOperation", mock.Anything, mock.MatchedBy(func(p db.FailOperationParams) bool {
		return p.ID == opID
	})).Return(db.Operation{}, nil).Run(func(args mock.Arguments) {
		failCalled <- args.Get(1).(db.FailOperationParams)
	})

	// Use an unregistered proto message: anypb.Any with an unknown type URL.
	// anypb.New() will fail when the message type is not resolvable.
	badMsg := &anypb.Any{TypeUrl: "type.googleapis.com/nonexistent.Type", Value: []byte("bad")}

	go m.runWork(opID, func(ctx context.Context, _ Progress) (proto.Message, error) {
		return badMsg, nil
	})

	select {
	case params := <-failCalled:
		assert.Equal(t, int32(codes.Internal), params.ErrorCode.Int32)
		assert.Contains(t, params.ErrorMessage.String, "marshal result")
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for FailOperation from marshal error")
	}
}

func TestDeleteOperation_InvalidName(t *testing.T) {
	ctx := context.Background()
	mockQ := new(mocks.MockQuerier)
	m := NewManager(mockQ, newTestLogger())

	err := m.DeleteOperation(ctx, "bad-name")
	require.Error(t, err)
	st := status.Convert(err)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

func TestDeleteOperation_DBError(t *testing.T) {
	ctx := context.Background()
	mockQ := new(mocks.MockQuerier)
	m := NewManager(mockQ, newTestLogger())

	opID := uuid.New()
	mockQ.On("GetOperation", ctx, opID).Return(db.Operation{}, fmt.Errorf("connection refused"))

	err := m.DeleteOperation(ctx, fmt.Sprintf("operations/assets/%s", opID))
	require.Error(t, err)
	st := status.Convert(err)
	assert.Equal(t, codes.Internal, st.Code())
}

func TestDeleteOperation_DeleteDBError(t *testing.T) {
	ctx := context.Background()
	mockQ := new(mocks.MockQuerier)
	m := NewManager(mockQ, newTestLogger())

	opID := uuid.New()
	mockQ.On("GetOperation", ctx, opID).Return(db.Operation{ID: opID, Done: true}, nil)
	mockQ.On("DeleteOperation", ctx, opID).Return(fmt.Errorf("disk full"))

	err := m.DeleteOperation(ctx, fmt.Sprintf("operations/assets/%s", opID))
	require.Error(t, err)
	st := status.Convert(err)
	assert.Equal(t, codes.Internal, st.Code())
	mockQ.AssertExpectations(t)
}

func TestCancelOperation_InvalidName(t *testing.T) {
	ctx := context.Background()
	mockQ := new(mocks.MockQuerier)
	m := NewManager(mockQ, newTestLogger())

	err := m.CancelOperation(ctx, "bad-name")
	require.Error(t, err)
	st := status.Convert(err)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

func TestCancelOperation_DBError(t *testing.T) {
	ctx := context.Background()
	mockQ := new(mocks.MockQuerier)
	m := NewManager(mockQ, newTestLogger())

	opID := uuid.New()
	mockQ.On("CancelOperation", ctx, opID).Return(db.Operation{}, fmt.Errorf("connection refused"))

	err := m.CancelOperation(ctx, fmt.Sprintf("operations/assets/%s", opID))
	require.Error(t, err)
	st := status.Convert(err)
	assert.Equal(t, codes.Internal, st.Code())
	mockQ.AssertExpectations(t)
}

func TestRecoverPending_ListError(t *testing.T) {
	ctx := context.Background()
	mockQ := new(mocks.MockQuerier)
	m := NewManager(mockQ, newTestLogger())

	mockQ.On("ListPendingOperations", ctx).Return([]db.Operation{}, fmt.Errorf("db down"))

	err := m.RecoverPending(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "list pending operations")
	mockQ.AssertExpectations(t)
}

func TestRecoverPending_FailOperationError(t *testing.T) {
	ctx := context.Background()
	mockQ := new(mocks.MockQuerier)
	m := NewManager(mockQ, newTestLogger())

	op := db.Operation{ID: uuid.New(), Prefix: "a"}
	mockQ.On("ListPendingOperations", ctx).Return([]db.Operation{op}, nil)
	mockQ.On("FailOperation", ctx, mock.MatchedBy(func(p db.FailOperationParams) bool {
		return p.ID == op.ID
	})).Return(db.Operation{}, fmt.Errorf("disk full"))

	// RecoverPending logs the error but does not fail
	err := m.RecoverPending(ctx)
	require.NoError(t, err)
	mockQ.AssertExpectations(t)
}

func TestGetOperation_DBError(t *testing.T) {
	ctx := context.Background()
	mockQ := new(mocks.MockQuerier)
	m := NewManager(mockQ, newTestLogger())

	opID := uuid.New()
	mockQ.On("GetOperation", ctx, opID).Return(db.Operation{}, fmt.Errorf("connection refused"))

	_, err := m.GetOperation(ctx, fmt.Sprintf("operations/assets/%s", opID))
	require.Error(t, err)
	st := status.Convert(err)
	assert.Equal(t, codes.Internal, st.Code())
	mockQ.AssertExpectations(t)
}

func TestListOperations_DBError(t *testing.T) {
	ctx := context.Background()
	mockQ := new(mocks.MockQuerier)
	m := NewManager(mockQ, newTestLogger())

	mockQ.On("ListOperations", ctx, mock.Anything).Return([]db.Operation{}, fmt.Errorf("db down"))

	_, err := m.ListOperations(ctx, "", 10)
	require.Error(t, err)
	st := status.Convert(err)
	assert.Equal(t, codes.Internal, st.Code())
	mockQ.AssertExpectations(t)
}

func TestListOperations_SkipsBadConversion(t *testing.T) {
	ctx := context.Background()
	mockQ := new(mocks.MockQuerier)
	m := NewManager(mockQ, newTestLogger())

	good := db.Operation{ID: uuid.New(), Prefix: "assets", Done: false}
	// Bad operation: Done=true with invalid metadata JSON that will cause unmarshalAny to fail
	bad := db.Operation{
		ID:       uuid.New(),
		Prefix:   "assets",
		Done:     false,
		Metadata: json.RawMessage(`{invalid json`),
	}

	mockQ.On("ListOperations", ctx, mock.Anything).Return([]db.Operation{bad, good}, nil)

	ops, err := m.ListOperations(ctx, "assets", 10)
	require.NoError(t, err)
	// The bad one should be skipped, only the good one returned
	assert.Len(t, ops, 1)
	mockQ.AssertExpectations(t)
}

func TestDoneOperation_NilMessage(t *testing.T) {
	// DoneOperation with a nil message should fail because anypb.New(nil) errors
	_, err := DoneOperation(nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "marshal response to Any")
}

func TestUnmarshalAny_InvalidJSON(t *testing.T) {
	_, err := unmarshalAny([]byte(`{not valid json`))
	require.Error(t, err)
}

func TestDbToProto_InvalidMetadata(t *testing.T) {
	op := db.Operation{
		ID:       uuid.New(),
		Prefix:   "assets",
		Done:     false,
		Metadata: json.RawMessage(`{bad json`),
	}
	_, err := dbToProto(op)
	require.Error(t, err)
}

func TestDbToProto_InvalidResult(t *testing.T) {
	op := db.Operation{
		ID:     uuid.New(),
		Prefix: "assets",
		Done:   true,
		Result: json.RawMessage(`{bad json`),
	}
	_, err := dbToProto(op)
	require.Error(t, err)
}

func TestDbToProto_DoneNoErrorNoResult(t *testing.T) {
	// Done with no error and no result -- covers the fall-through path
	op := db.Operation{
		ID:     uuid.New(),
		Prefix: "assets",
		Done:   true,
	}
	pbOp, err := dbToProto(op)
	require.NoError(t, err)
	assert.True(t, pbOp.Done)
	assert.Nil(t, pbOp.GetError())
	assert.Nil(t, pbOp.GetResponse())
}

func TestDbToProto_ErrorWithInvalidMessage(t *testing.T) {
	// Error code set but ErrorMessage not valid -- covers the msg="" branch
	op := db.Operation{
		ID:           uuid.New(),
		Prefix:       "assets",
		Done:         true,
		ErrorCode:    pgtype.Int4{Int32: int32(codes.Internal), Valid: true},
		ErrorMessage: pgtype.Text{Valid: false},
	}
	pbOp, err := dbToProto(op)
	require.NoError(t, err)
	require.NotNil(t, pbOp.GetError())
	assert.Equal(t, int32(codes.Internal), pbOp.GetError().Code)
	assert.Equal(t, "", pbOp.GetError().Message)
}

func TestRunWork_FailOperationDBError(t *testing.T) {
	// Test that when work fails AND FailOperation also fails, we just log (no panic)
	ctx := context.Background()
	mockQ := new(mocks.MockQuerier)
	m := NewManager(mockQ, newTestLogger())

	mockQ.On("CreateOperation", ctx, mock.Anything).Return(db.Operation{
		ID:     uuid.New(),
		Prefix: "assets",
		Done:   false,
	}, nil)

	done := make(chan struct{})
	mockQ.On("FailOperation", mock.Anything, mock.Anything).Return(db.Operation{}, fmt.Errorf("db down")).Run(func(_ mock.Arguments) {
		close(done)
	})

	_, err := m.CreateAndRun(ctx, "assets", nil, func(ctx context.Context, _ Progress) (proto.Message, error) {
		return nil, fmt.Errorf("work failed")
	})
	require.NoError(t, err)

	select {
	case <-done:
		// FailOperation was called (and errored), runWork should not panic
	case <-time.After(5 * time.Second):
		t.Fatal("timed out")
	}
}

func TestRunWork_CompleteOperationDBError(t *testing.T) {
	// Test that when work succeeds but CompleteOperation fails, we just log
	ctx := context.Background()
	mockQ := new(mocks.MockQuerier)
	m := NewManager(mockQ, newTestLogger())

	mockQ.On("CreateOperation", ctx, mock.Anything).Return(db.Operation{
		ID:     uuid.New(),
		Prefix: "assets",
		Done:   false,
	}, nil)

	done := make(chan struct{})
	mockQ.On("CompleteOperation", mock.Anything, mock.Anything).Return(db.Operation{}, fmt.Errorf("db down")).Run(func(_ mock.Arguments) {
		close(done)
	})

	_, err := m.CreateAndRun(ctx, "assets", nil, func(ctx context.Context, _ Progress) (proto.Message, error) {
		s, _ := structpb.NewStruct(map[string]interface{}{"k": "v"})
		return s, nil
	})
	require.NoError(t, err)

	select {
	case <-done:
		// CompleteOperation was called (and errored), runWork should not panic
	case <-time.After(5 * time.Second):
		t.Fatal("timed out")
	}
}

func TestRunWork_MarshalResultError_FailOperationAlsoFails(t *testing.T) {
	// When marshal error occurs and FailOperation also fails, we just log both
	mockQ := new(mocks.MockQuerier)
	m := NewManager(mockQ, newTestLogger())

	opID := uuid.New()

	done := make(chan struct{})
	mockQ.On("FailOperation", mock.Anything, mock.MatchedBy(func(p db.FailOperationParams) bool {
		return p.ID == opID
	})).Return(db.Operation{}, fmt.Errorf("db down")).Run(func(_ mock.Arguments) {
		close(done)
	})

	badMsg := &anypb.Any{TypeUrl: "type.googleapis.com/nonexistent.Type", Value: []byte("bad")}

	go m.runWork(opID, func(ctx context.Context, _ Progress) (proto.Message, error) {
		return badMsg, nil
	})

	select {
	case <-done:
		// both errors logged, no panic
	case <-time.After(5 * time.Second):
		t.Fatal("timed out")
	}
}

func TestReaper_Run_DeleteError(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	logger := newTestLogger()

	r := NewReaper(mockQ, 10*time.Millisecond, logger)

	called := make(chan struct{}, 10)
	mockQ.On("DeleteExpiredOperations", mock.Anything).Return(fmt.Errorf("db error")).Run(func(_ mock.Arguments) {
		select {
		case called <- struct{}{}:
		default:
		}
	})

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	err := r.Run(ctx)
	require.Error(t, err)
	assert.ErrorIs(t, err, context.DeadlineExceeded)

	// Verify it was called at least once despite the error
	assert.NotEmpty(t, called)
}
