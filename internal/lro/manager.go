package lro

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"cloud.google.com/go/longrunning/autogen/longrunningpb"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	"github.com/dashkan/pivox/internal/apierr"
	db "github.com/dashkan/pivox/internal/db/generated"
)

// WorkFunc performs the actual work for an operation. The supplied
// `progress` reports phase transitions back to the operation
// metadata so polling clients can observe state. Progress reporting
// is best-effort; the Update method swallows DB errors and never
// returns one (a metadata write failure must not abort the work).
type WorkFunc func(ctx context.Context, progress Progress) (proto.Message, error)

// Progress is the phase-reporting handle passed to a WorkFunc. It
// captures the operation ID so callers don't have to thread it
// manually. Implementations are safe to call from any goroutine.
type Progress interface {
	Update(ctx context.Context, metadata proto.Message)
}

type managerProgress struct {
	m    *Manager
	opID uuid.UUID
}

func (p *managerProgress) Update(ctx context.Context, metadata proto.Message) {
	p.m.UpdateMetadata(ctx, p.opID, metadata)
}

// Manager manages long-running operations.
type Manager struct {
	queries db.Querier
	logger  *slog.Logger

	mu        sync.Mutex
	listeners map[uuid.UUID][]chan struct{}
}

// NewManager creates a new LRO manager.
func NewManager(queries db.Querier, logger *slog.Logger) *Manager {
	return &Manager{
		queries:   queries,
		logger:    logger,
		listeners: make(map[uuid.UUID][]chan struct{}),
	}
}

// UpdateMetadata replaces the metadata blob on an in-flight
// operation. Used by multi-phase work functions to surface progress
// (e.g. DeleteOrganization transitioning VALIDATING →
// CANCELLING_OPERATIONS → MARKING_DELETED) so polling clients see
// the current phase. Marshal errors are logged but not returned —
// progress reporting is best-effort and must not abort the work.
func (m *Manager) UpdateMetadata(ctx context.Context, opID uuid.UUID, metadata proto.Message) {
	metaJSON, err := marshalAny(metadata)
	if err != nil {
		m.logger.Error("lro: marshal metadata for update", "op", opID, "error", err)
		return
	}
	if err := m.queries.UpdateOperationMetadata(ctx, db.UpdateOperationMetadataParams{
		ID:       opID,
		Metadata: metaJSON,
	}); err != nil {
		m.logger.Error("lro: update operation metadata", "op", opID, "error", err)
	}
}

// CreateAndRun creates a new operation and runs the work function
// asynchronously. The operation is unscoped — operations.org_id is
// NULL — so it isn't cancellable via DeleteOrganization's
// CANCELLING_OPERATIONS phase. Use CreateAndRunForOrg when the LRO
// targets an organization and should be cancelled if the org is
// soft-deleted.
func (m *Manager) CreateAndRun(ctx context.Context, prefix string, metadata proto.Message, work WorkFunc) (*longrunningpb.Operation, error) {
	return m.createAndRun(ctx, prefix, pgtype.UUID{}, metadata, work)
}

// CreateAndRunForOrg is the org-scoped variant: the operation row's
// org_id is set to `orgID`, so DeleteOrganization's
// CancelRunningOpsForOrg will mark this LRO done with codes.Cancelled
// when the org enters DELETE_REQUESTED. Use this for any LRO whose
// progress would mutate org-scoped state (asset imports, domain
// verifications, gateway upgrades). DO NOT use it for the
// DeleteOrganization LRO itself — a self-pointing org_id would
// cause the cancellation phase to cancel its own work.
func (m *Manager) CreateAndRunForOrg(ctx context.Context, prefix string, orgID uuid.UUID, metadata proto.Message, work WorkFunc) (*longrunningpb.Operation, error) {
	return m.createAndRun(ctx, prefix, pgtype.UUID{Bytes: orgID, Valid: true}, metadata, work)
}

func (m *Manager) createAndRun(ctx context.Context, prefix string, orgID pgtype.UUID, metadata proto.Message, work WorkFunc) (*longrunningpb.Operation, error) {
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
		OrgID:    orgID,
	})
	if err != nil {
		return nil, apierr.Internal("failed to create operation")
	}

	go m.runWork(opID, work)

	return dbToProto(dbOp)
}

func (m *Manager) runWork(opID uuid.UUID, work WorkFunc) {
	ctx := context.Background()

	progress := &managerProgress{m: m, opID: opID}
	result, err := work(ctx, progress)
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
				if _, dbErr := m.queries.FailOperation(ctx, db.FailOperationParams{
					ID:           opID,
					ErrorCode:    pgtype.Int4{Int32: int32(codes.Internal), Valid: true},
					ErrorMessage: pgtype.Text{String: "marshal result: " + marshalErr.Error(), Valid: true},
				}); dbErr != nil {
					m.logger.Error("failed to mark operation as failed after marshal error", "op", opID, "error", dbErr)
				}
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
		if errors.Is(err, pgx.ErrNoRows) {
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
		if errors.Is(err, pgx.ErrNoRows) {
			return apierr.NotFound("Operation", name)
		}
		return apierr.Internal("failed to get operation")
	}
	if !dbOp.Done {
		return apierr.FailedPrecondition("cannot delete a running operation")
	}
	if err := m.queries.DeleteOperation(ctx, opID); err != nil {
		return apierr.Internal("failed to delete operation")
	}
	return nil
}

// CancelOperation cancels a running operation.
func (m *Manager) CancelOperation(ctx context.Context, name string) error {
	opID, err := parseOperationName(name)
	if err != nil {
		return apierr.InvalidArgument(apierr.FieldViolation("name", err.Error()))
	}
	_, err = m.queries.CancelOperation(ctx, opID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
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
