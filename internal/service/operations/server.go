package operations

import (
	"context"
	"errors"
	"time"

	"cloud.google.com/go/longrunning/autogen/longrunningpb"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/dashkan/pivox/internal/apierr"
	"github.com/dashkan/pivox/internal/appkey"
	db "github.com/dashkan/pivox/internal/db/generated"
	"github.com/dashkan/pivox/internal/filter"
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
	pool     db.RWPool
	queries  db.Querier
	codec    *appkey.Codec
	resolver *permission.Resolver
}

// Config is the constructor input for OperationsServer.
type Config struct {
	// LRO is the long-running operation manager that backs the action
	// methods (wait/cancel/delete). Required.
	LRO LROManager
	// Pool runs the dynamic, keyset-paginated ListOperations query
	// (filter.BuildListQuery). Required.
	Pool db.RWPool
	// Queries reads an operation row for its authz scope
	// (Get/Wait/Cancel/Delete). Required.
	Queries db.Querier
	// Codec opaque-encodes ListOperations page tokens (the keyset cursor).
	// Required.
	Codec *appkey.Codec
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
	if cfg.Pool == nil {
		panic("operations: Config.Pool is required")
	}
	if cfg.Queries == nil {
		panic("operations: Config.Queries is required")
	}
	if cfg.Codec == nil {
		panic("operations: Config.Codec is required")
	}
	if cfg.Resolver == nil {
		panic("operations: Config.Resolver is required")
	}
	return &OperationsServer{lro: cfg.LRO, pool: cfg.Pool, queries: cfg.Queries, codec: cfg.Codec, resolver: cfg.Resolver}
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

// authorizedOperationsScope is the set-wise visibility predicate for
// ListOperations: the caller sees an account-scoped op only if they created
// it, an org-scoped op if they hold organizations.read at its org, and a
// space-scoped op if they hold spaces.read at its space (via direct space
// membership OR inherited parent-org membership). Membership resolves both
// direct (user_id) and group (group_id) bindings.
//
// It is the non-negotiable base scope ANDed into filter.BuildListQuery — the
// request's filter/order_by layer ON TOP and can only narrow, never widen it.
// This is the same authorization the pre-migration ListAuthorizedOperations
// sqlc query enforced (its cross-tenant-IDOR regression tests still guard it);
// the migration moves it here as a base predicate so the keyset cursor + AIP
// filter can compose with it.
//
// The single `%[1]s` verb is BuildListQuery's placeholder for this predicate's
// one bound Arg (the caller's identity). Go's explicit-argument-index verb
// emits that SAME placeholder at every caller reference, so the caller is bound
// EXACTLY ONCE and reused — no value is ever interpolated, and the placeholder
// tracks whatever $N BuildListQuery assigns. The outer parentheses keep the
// OR-expression intact when BuildListQuery ANDs it with the filter/cursor.
// Column refs are qualified `operations.` because BuildListQuery selects
// `FROM operations` with no alias.
const authorizedOperationsScope = `(
  (operations.org_id IS NULL AND operations.space_id IS NULL AND operations.created_by = %[1]s)
  OR (operations.space_id IS NULL AND operations.org_id IS NOT NULL AND EXISTS (
        SELECT 1 FROM org_members om
          JOIN role_permissions rp ON rp.role_id = om.role_id
          JOIN permissions perm ON perm.id = rp.permission_id
         WHERE om.org_id = operations.org_id
           AND perm.permission_id = 'organizations.read'
           AND (om.user_id = %[1]s
                OR om.group_id IN (SELECT gm.group_id FROM group_members gm WHERE gm.user_id = %[1]s))))
  OR (operations.space_id IS NOT NULL AND (
        EXISTS (SELECT 1 FROM space_members sm
                  JOIN role_permissions rp ON rp.role_id = sm.role_id
                  JOIN permissions perm ON perm.id = rp.permission_id
                 WHERE sm.space_id = operations.space_id
                   AND perm.permission_id = 'spaces.read'
                   AND (sm.user_id = %[1]s
                        OR sm.group_id IN (SELECT gm.group_id FROM group_members gm WHERE gm.user_id = %[1]s)))
        OR EXISTS (SELECT 1 FROM spaces s
                     JOIN org_members om ON om.org_id = s.org_id
                     JOIN role_permissions rp ON rp.role_id = om.role_id
                     JOIN permissions perm ON perm.id = rp.permission_id
                    WHERE s.id = operations.space_id
                      AND perm.permission_id = 'spaces.read'
                      AND (om.user_id = %[1]s
                           OR om.group_id IN (SELECT gm.group_id FROM group_members gm WHERE gm.user_id = %[1]s))))))`

// ListOperations returns the operations the caller is permitted to see,
// scope-trimmed set-wise (account ops they created, plus org/space ops they can
// read) via the dynamic keyset engine (filter.BuildListQuery). The
// authorization is the NON-NEGOTIABLE base scope; the request's AIP-160 filter
// (e.g. `done = true`) layers on top and can only narrow. Pagination is a
// compound (create_time, id) keyset — every page returns a working
// next_page_token so a client can read past the first page (the pre-migration
// handler accepted page_size but never emitted a token).
func (s *OperationsServer) ListOperations(ctx context.Context, req *longrunningpb.ListOperationsRequest) (*longrunningpb.ListOperationsResponse, error) {
	rf := filter.OperationFilter()
	pageSize := filter.ClampPageSize(rf, req.GetPageSize())

	// The longrunning request has no order_by field; the plan is always the
	// declared default (createTime desc → compound (create_time, id) keyset).
	plan, err := filter.PlanOrderBy(rf, "")
	if err != nil {
		return nil, apierr.Internal(err, "resolve operations order")
	}

	cursor, err := filter.DecodeCursor(s.codec, plan, req.GetPageToken())
	if err != nil {
		return nil, apierr.InvalidArgument(apierr.FieldViolation("page_token", "invalid or malformed"))
	}

	sql, args, err := filter.BuildListQuery(filter.ListQuery{
		Resource: rf,
		Base:     []filter.Predicate{{SQL: authorizedOperationsScope, Arg: server.MustUserID(ctx)}},
		Filter:   req.GetFilter(),
		Order:    plan,
		PageSize: pageSize,
		Cursor:   cursor,
	})
	if err != nil {
		// The only error source is the filter transpiler (bad user filter, e.g.
		// an unknown field) — surface it as InvalidArgument on "filter".
		return nil, apierr.InvalidArgument(apierr.FieldViolation("filter", err.Error()))
	}

	pgxRows, err := s.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, apierr.Internal(err, "failed to list operations")
	}
	rows, err := filter.ScanOperations(pgxRows)
	if err != nil {
		return nil, apierr.Internal(err, "failed to list operations")
	}

	// filter.Paginate trims the over-fetched result to pageSize and derives the
	// next-page token from the LAST RETURNED row (see the connectors comment for
	// the off-by-one this closes).
	rows, nextPageToken, err := filter.Paginate(rows, int(pageSize), func(last db.Operation) (string, error) {
		return filter.EncodeCursor(s.codec, plan, operationSortValue(plan, last), last.ID)
	})
	if err != nil {
		return nil, apierr.Internal(err, "encode page token")
	}

	ops := make([]*longrunningpb.Operation, 0, len(rows))
	for _, row := range rows {
		p, err := lro.OperationToProto(row)
		if err != nil {
			return nil, apierr.Internal(err, "failed to convert operation")
		}
		ops = append(ops, p)
	}
	return &longrunningpb.ListOperationsResponse{Operations: ops, NextPageToken: nextPageToken}, nil
}

// operationSortValue renders the primary order_by column's value for the given
// row as the string the compound page token carries. The only sortable column
// is create_time (RFC3339Nano so filter.DecodeCursor round-trips it to an exact
// time.Time). Mirrors connectorSortValue.
func operationSortValue(plan filter.OrderByPlan, r db.Operation) string {
	if plan.Field == "createTime" {
		return r.CreateTime.UTC().Format(time.RFC3339Nano)
	}
	return ""
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
