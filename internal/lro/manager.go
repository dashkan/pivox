package lro

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"cloud.google.com/go/longrunning/autogen/longrunningpb"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	"github.com/dashkan/pivox-server/internal/apierr"
	db "github.com/dashkan/pivox-server/internal/db/generated"
)

// WorkFunc performs the actual work for an operation.
type WorkFunc func(ctx context.Context) (proto.Message, error)

// Manager manages long-running operations.
type Manager struct {
	pool    *pgxpool.Pool
	queries *db.Queries
	logger  *slog.Logger

	mu        sync.Mutex
	listeners map[uuid.UUID][]chan struct{}
}

// NewManager creates a new LRO manager.
func NewManager(pool *pgxpool.Pool, queries *db.Queries, logger *slog.Logger) *Manager {
	return &Manager{
		pool:      pool,
		queries:   queries,
		logger:    logger,
		listeners: make(map[uuid.UUID][]chan struct{}),
	}
}

// CreateAndRun creates a new operation and runs the work function asynchronously.
func (m *Manager) CreateAndRun(ctx context.Context, prefix string, metadata proto.Message, work WorkFunc) (*longrunningpb.Operation, error) {
	opID := uuid.New()

	var metaJSON json.RawMessage
	if metadata != nil {
		var err error
		metaJSON, err = marshalAny(metadata)
		if err != nil {
			return nil, apierr.Internal("failed to marshal operation metadata")
		}
	}

	dbOp, err := m.queries.CreateOperation(ctx, db.CreateOperationParams{
		ID:       opID,
		Prefix:   prefix,
		Metadata: metaJSON,
	})
	if err != nil {
		return nil, apierr.Internal("failed to create operation")
	}

	go m.runWork(opID, work)

	return dbToProto(dbOp)
}

func (m *Manager) runWork(opID uuid.UUID, work WorkFunc) {
	ctx := context.Background()

	result, err := work(ctx)
	if err != nil {
		errCode := int32(codes.Internal)
		errMsg := err.Error()
		if st, ok := grpcstatus.FromError(err); ok {
			errCode = int32(st.Code())
			errMsg = st.Message()
		}
		if _, dbErr := m.queries.FailOperation(ctx, db.FailOperationParams{
			ID:           opID,
			ErrorCode:    pgtype.Int4{Int32: errCode, Valid: true},
			ErrorMessage: pgtype.Text{String: errMsg, Valid: true},
		}); dbErr != nil {
			m.logger.Error("failed to mark operation as failed", "op", opID, "error", dbErr)
		}
	} else {
		var resultJSON json.RawMessage
		if result != nil {
			var marshalErr error
			resultJSON, marshalErr = marshalAny(result)
			if marshalErr != nil {
				m.logger.Error("failed to marshal operation result", "op", opID, "error", marshalErr)
				return
			}
		}
		if _, dbErr := m.queries.CompleteOperation(ctx, db.CompleteOperationParams{
			ID:     opID,
			Result: resultJSON,
		}); dbErr != nil {
			m.logger.Error("failed to complete operation", "op", opID, "error", dbErr)
		}
	}

	m.notifyListeners(opID)
}

// parseOperationName extracts the UUID from "operations/{prefix}/{uuid}" or "operations/{uuid}".
func parseOperationName(name string) (uuid.UUID, error) {
	parts := strings.Split(name, "/")
	if len(parts) < 2 {
		return uuid.Nil, fmt.Errorf("invalid operation name %q", name)
	}
	// The UUID is always the last segment
	id, err := uuid.Parse(parts[len(parts)-1])
	if err != nil {
		return uuid.Nil, fmt.Errorf("invalid operation ID in %q: %w", name, err)
	}
	return id, nil
}

// GetOperation retrieves an operation by name.
func (m *Manager) GetOperation(ctx context.Context, name string) (*longrunningpb.Operation, error) {
	opID, err := parseOperationName(name)
	if err != nil {
		return nil, apierr.InvalidArgument(apierr.FieldViolation("name", err.Error()))
	}
	dbOp, err := m.queries.GetOperation(ctx, opID)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, apierr.NotFound("Operation", name)
		}
		return nil, apierr.Internal("failed to get operation")
	}
	return dbToProto(dbOp)
}

// ListOperations lists operations with optional filtering by prefix.
func (m *Manager) ListOperations(ctx context.Context, prefix string, pageSize int32) ([]*longrunningpb.Operation, error) {
	if pageSize <= 0 || pageSize > 1000 {
		pageSize = 100
	}

	var prefixFilter pgtype.Text
	if prefix != "" {
		prefixFilter = pgtype.Text{String: prefix, Valid: true}
	}

	dbOps, err := m.queries.ListOperations(ctx, db.ListOperationsParams{
		Limit:        pageSize,
		PrefixFilter: prefixFilter,
	})
	if err != nil {
		return nil, apierr.Internal("failed to list operations")
	}

	ops := make([]*longrunningpb.Operation, 0, len(dbOps))
	for _, dbOp := range dbOps {
		op, err := dbToProto(dbOp)
		if err != nil {
			continue
		}
		ops = append(ops, op)
	}
	return ops, nil
}

// WaitOperation waits for an operation to complete or the context to be cancelled.
func (m *Manager) WaitOperation(ctx context.Context, name string) (*longrunningpb.Operation, error) {
	op, err := m.GetOperation(ctx, name)
	if err != nil {
		return nil, err
	}
	if op.Done {
		return op, nil
	}

	opID, _ := parseOperationName(name)

	ch := make(chan struct{}, 1)
	m.mu.Lock()
	m.listeners[opID] = append(m.listeners[opID], ch)
	m.mu.Unlock()

	defer func() {
		m.mu.Lock()
		listeners := m.listeners[opID]
		for i, l := range listeners {
			if l == ch {
				m.listeners[opID] = append(listeners[:i], listeners[i+1:]...)
				break
			}
		}
		if len(m.listeners[opID]) == 0 {
			delete(m.listeners, opID)
		}
		m.mu.Unlock()
	}()

	select {
	case <-ch:
		return m.GetOperation(ctx, name)
	case <-ctx.Done():
		return m.GetOperation(context.Background(), name)
	}
}

// DeleteOperation deletes a completed operation.
func (m *Manager) DeleteOperation(ctx context.Context, name string) error {
	opID, err := parseOperationName(name)
	if err != nil {
		return apierr.InvalidArgument(apierr.FieldViolation("name", err.Error()))
	}
	dbOp, err := m.queries.GetOperation(ctx, opID)
	if err != nil {
		if err == pgx.ErrNoRows {
			return apierr.NotFound("Operation", name)
		}
		return apierr.Internal("failed to get operation")
	}
	if !dbOp.Done {
		return apierr.FailedPrecondition("cannot delete a running operation")
	}
	return m.queries.DeleteOperation(ctx, opID)
}

// CancelOperation cancels a running operation.
func (m *Manager) CancelOperation(ctx context.Context, name string) error {
	opID, err := parseOperationName(name)
	if err != nil {
		return apierr.InvalidArgument(apierr.FieldViolation("name", err.Error()))
	}
	_, err = m.queries.CancelOperation(ctx, opID)
	if err != nil {
		if err == pgx.ErrNoRows {
			_, getErr := m.queries.GetOperation(ctx, opID)
			if getErr != nil {
				return apierr.NotFound("Operation", name)
			}
			return nil
		}
		return apierr.Internal("failed to cancel operation")
	}
	m.notifyListeners(opID)
	return nil
}

// RecoverPending marks any pending (non-done) operations as failed on startup.
func (m *Manager) RecoverPending(ctx context.Context) error {
	ops, err := m.queries.ListPendingOperations(ctx)
	if err != nil {
		return fmt.Errorf("list pending operations: %w", err)
	}
	for _, op := range ops {
		if _, err := m.queries.FailOperation(ctx, db.FailOperationParams{
			ID:           op.ID,
			ErrorCode:    pgtype.Int4{Int32: int32(codes.Aborted), Valid: true},
			ErrorMessage: pgtype.Text{String: "operation abandoned during server restart", Valid: true},
		}); err != nil {
			m.logger.Error("failed to recover pending operation", "op", op.ID, "error", err)
		}
	}
	if len(ops) > 0 {
		m.logger.Info("recovered pending operations", "count", len(ops))
	}
	return nil
}

func (m *Manager) notifyListeners(opID uuid.UUID) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, ch := range m.listeners[opID] {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}
