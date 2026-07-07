package operations

import (
	"context"
	"errors"

	"cloud.google.com/go/longrunning/autogen/longrunningpb"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/dashkan/pivox/internal/apierr"
	"github.com/dashkan/pivox/internal/convert"
	db "github.com/dashkan/pivox/internal/db/generated"
	"github.com/dashkan/pivox/internal/lro"
	"github.com/dashkan/pivox/internal/permission"
	"github.com/dashkan/pivox/internal/server"
)

// LROManager defines the long-running operation methods the server needs.
type LROManager interface {
	GetOperation(ctx context.Context, name string) (*longrunningpb.Operation, error)
	WaitOperation(ctx context.Context, name string) (*longrunningpb.Operation, error)
	DeleteOperation(ctx context.Context, name string) error
	CancelOperation(ctx context.Context, name string) error
}

// OperationsServer implements longrunningpb.OperationsServer.
//
// Every per-operation method authorizes the caller against the
// operation's scope before doing anything: a space-scoped op requires
// spaces.read at its space, an org-scoped op requires organizations.read
// at its org, and an account-scoped op (no org/space) is visible only to
// its creator. Denied access returns NotFound — an operation the caller
// may not see is indistinguishable from one that doesn't exist, so the
// surface can't be probed for cross-tenant operation IDs. ListOperations
// resolves the same rule set-wise in a single membership-scoped query
// (no per-row N+1).
type OperationsServer struct {
	longrunningpb.UnimplementedOperationsServer
	lro      LROManager
	queries  db.Querier
	resolver *permission.Resolver
}

// Config is the constructor input for OperationsServer.
type Config struct {
	// LRO is the long-running operation manager that backs the action
	// methods (wait/cancel/delete). Required.
	LRO LROManager
	// Queries reads an operation row for its authz scope and runs the
	// membership-scoped list query. Required.
	Queries db.Querier
	// Resolver answers the per-operation permission check for
	// Get/Wait/Cancel/Delete. Required.
	Resolver *permission.Resolver
}

// NewOperationsServer constructs the server from cfg. Panics on a
// missing required field.
func NewOperationsServer(cfg Config) *OperationsServer {
	if cfg.LRO == nil {
		panic("operations: Config.LRO is required")
	}
	if cfg.Queries == nil {
		panic("operations: Config.Queries is required")
	}
	if cfg.Resolver == nil {
		panic("operations: Config.Resolver is required")
	}
	return &OperationsServer{lro: cfg.LRO, queries: cfg.Queries, resolver: cfg.Resolver}
}

// authorizeOp loads the operation named by `name` and confirms the
// caller may see it, returning the row on success. NotFound is returned
// both when the operation is absent and when the caller may not see it —
// the two cases are deliberately indistinguishable.
func (s *OperationsServer) authorizeOp(ctx context.Context, name string) (db.Operation, error) {
	opID, err := lro.ParseOperationName(name)
	if err != nil {
		return db.Operation{}, apierr.InvalidArgument(apierr.FieldViolation("name", err.Error()))
	}
	op, err := s.queries.GetOperation(ctx, opID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return db.Operation{}, apierr.NotFound("Operation", name)
		}
		return db.Operation{}, apierr.Internal(err, "failed to load operation")
	}

	allowed, err := s.callerCanSee(ctx, server.MustUserID(ctx), op)
	if err != nil {
		return db.Operation{}, err
	}
	if !allowed {
		return db.Operation{}, apierr.NotFound("Operation", name)
	}
	return op, nil
}

// callerCanSee applies the generic scope rule for a single operation:
// space_id → spaces.read, else org_id → organizations.read, else
// account-scoped → created_by must equal the caller.
func (s *OperationsServer) callerCanSee(ctx context.Context, caller uuid.UUID, op db.Operation) (bool, error) {
	switch {
	case op.SpaceID.Valid:
		return s.resolver.HasPermission(ctx, caller, permission.SpaceTarget(uuid.UUID(op.SpaceID.Bytes)), permission.SpacesRead)
	case op.OrgID.Valid:
		return s.resolver.HasPermission(ctx, caller, permission.OrgTarget(uuid.UUID(op.OrgID.Bytes)), permission.OrganizationsRead)
	default:
		return op.CreatedBy.Valid && uuid.UUID(op.CreatedBy.Bytes) == caller, nil
	}
}

// GetOperation returns the latest state of a long-running operation the
// caller is authorized to see.
func (s *OperationsServer) GetOperation(ctx context.Context, req *longrunningpb.GetOperationRequest) (*longrunningpb.Operation, error) {
	if req.GetName() == "" {
		return nil, apierr.InvalidArgument(apierr.FieldViolation("name", "name is required"))
	}
	op, err := s.authorizeOp(ctx, req.GetName())
	if err != nil {
		return nil, err
	}
	return lro.OperationToProto(op)
}

// ListOperations returns the operations the caller is permitted to see,
// scope-trimmed in a single query (account ops they created, plus org/
// space ops they can read). The request's name/filter are not used as a
// server-side prefix — visibility is the trim.
func (s *OperationsServer) ListOperations(ctx context.Context, req *longrunningpb.ListOperationsRequest) (*longrunningpb.ListOperationsResponse, error) {
	pageSize := req.GetPageSize()
	if pageSize <= 0 {
		pageSize = 100
	}

	rows, err := s.queries.ListAuthorizedOperations(ctx, db.ListAuthorizedOperationsParams{
		Caller:   convert.PgUUID(server.MustUserID(ctx)),
		PageSize: pageSize,
	})
	if err != nil {
		return nil, apierr.Internal(err, "failed to list operations")
	}

	ops := make([]*longrunningpb.Operation, 0, len(rows))
	for _, row := range rows {
		p, err := lro.OperationToProto(row)
		if err != nil {
			return nil, apierr.Internal(err, "failed to convert operation")
		}
		ops = append(ops, p)
	}
	return &longrunningpb.ListOperationsResponse{Operations: ops}, nil
}

// WaitOperation waits until the operation is done or the timeout elapses,
// returning the latest state — after authorizing the caller.
func (s *OperationsServer) WaitOperation(ctx context.Context, req *longrunningpb.WaitOperationRequest) (*longrunningpb.Operation, error) {
	if _, err := s.authorizeOp(ctx, req.GetName()); err != nil {
		return nil, err
	}
	if req.GetTimeout() != nil {
		timeout := req.GetTimeout().AsDuration()
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	return s.lro.WaitOperation(ctx, req.GetName())
}

// DeleteOperation deletes an operation the caller is authorized to see,
// indicating the client is no longer interested in the result.
func (s *OperationsServer) DeleteOperation(ctx context.Context, req *longrunningpb.DeleteOperationRequest) (*emptypb.Empty, error) {
	if req.GetName() == "" {
		return nil, apierr.InvalidArgument(apierr.FieldViolation("name", "name is required"))
	}
	if _, err := s.authorizeOp(ctx, req.GetName()); err != nil {
		return nil, err
	}
	if err := s.lro.DeleteOperation(ctx, req.GetName()); err != nil {
		return nil, err
	}
	return &emptypb.Empty{}, nil
}

// CancelOperation starts asynchronous cancellation on an operation the
// caller is authorized to see.
func (s *OperationsServer) CancelOperation(ctx context.Context, req *longrunningpb.CancelOperationRequest) (*emptypb.Empty, error) {
	if req.GetName() == "" {
		return nil, apierr.InvalidArgument(apierr.FieldViolation("name", "name is required"))
	}
	if _, err := s.authorizeOp(ctx, req.GetName()); err != nil {
		return nil, err
	}
	if err := s.lro.CancelOperation(ctx, req.GetName()); err != nil {
		return nil, err
	}
	return &emptypb.Empty{}, nil
}
