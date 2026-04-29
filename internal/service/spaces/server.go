package spaces

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"cloud.google.com/go/longrunning/autogen/longrunningpb"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/dashkan/pivox/internal/apierr"
	"github.com/dashkan/pivox/internal/appkey"
	"github.com/dashkan/pivox/internal/convert"
	db "github.com/dashkan/pivox/internal/db/generated"
	"github.com/dashkan/pivox/internal/filter"
	"github.com/dashkan/pivox/internal/lro"
	"github.com/dashkan/pivox/internal/permission"
	apiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/api/v1"
	"github.com/dashkan/pivox/internal/server"
)

// TxBeginner abstracts transaction creation for testability.
// *pgxpool.Pool satisfies this interface. Same shape as the
// organizations service's local TxBeginner.
type TxBeginner interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

type SpacesServer struct {
	apiv1.UnimplementedSpacesServer
	db       db.DBTX
	pool     TxBeginner
	queries  db.Querier
	filter   *filter.ResourceFilter
	codec    *appkey.Codec
	resolver *permission.Resolver
	caller   server.CallerIdentityResolver
}

// NewSpacesServer constructs the server. `resolver` and `caller` are
// only consumed by the IAM-shaped handlers (TestIamPermissions and
// space-scope Member CRUD). `pool` is used for tx-wrapped writes
// (CreateMember principal validation + insert atomicity); tests that
// only exercise reads may pass nil.
func NewSpacesServer(pool db.DBTX, txPool TxBeginner, queries db.Querier, codec *appkey.Codec, resolver *permission.Resolver, caller server.CallerIdentityResolver) *SpacesServer {
	return &SpacesServer{
		db:       pool,
		pool:     txPool,
		queries:  queries,
		filter:   filter.SpaceFilter(),
		codec:    codec,
		resolver: resolver,
		caller:   caller,
	}
}

// parseSpaceName parses "organizations/{org}/spaces/{space}" and returns (orgName, spaceName).
func parseSpaceName(name string) (string, string, error) {
	parts := strings.Split(name, "/")
	if len(parts) != 4 || parts[0] != "organizations" || parts[2] != "spaces" {
		return "", "", fmt.Errorf("invalid space name %q: expected organizations/*/spaces/*", name)
	}
	return parts[1], parts[3], nil
}

// parseSpaceParent extracts the org slug from "organizations/{org}".
// Surfaced as InvalidArgument; mirrors the org-scope parent parser in
// the organizations service.
func parseSpaceParent(parent string) (string, error) {
	parts := strings.Split(parent, "/")
	if len(parts) != 2 || parts[0] != "organizations" || parts[1] == "" {
		return "", apierr.InvalidArgument(apierr.FieldViolation("parent",
			fmt.Sprintf("invalid parent %q: expected organizations/{org}", parent)))
	}
	return parts[1], nil
}

func (s *SpacesServer) GetSpace(ctx context.Context, req *apiv1.GetSpaceRequest) (*apiv1.Space, error) {
	orgName, spaceName, err := parseSpaceName(req.GetName())
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Space", req.GetName())
	}
	// Use the rows resolved by the permission interceptor — its gate
	// already paid for both lookups (org + space) and used the
	// soft-delete-aware GetSpaceByNameForGate so reads still work
	// during the grace window. Defensive slug match catches any
	// future scope-extractor / handler-name divergence.
	resolvedOrg := server.MustResolvedOrgFromContext(ctx)
	resolvedSpace := server.MustResolvedSpaceFromContext(ctx)
	if orgName != resolvedOrg.Slug || spaceName != resolvedSpace.Slug {
		return nil, apierr.InvalidArgument(apierr.FieldViolation("name",
			"space path does not match resolved scope"))
	}
	return convert.SpaceToProto(resolvedSpace.Row, resolvedOrg.Slug), nil
}

func (s *SpacesServer) ListSpaces(ctx context.Context, req *apiv1.ListSpacesRequest) (*apiv1.ListSpacesResponse, error) {
	parentSlug, err := parseSpaceParent(req.GetParent())
	if err != nil {
		return nil, err
	}
	resolvedOrg := server.MustResolvedOrgFromContext(ctx)
	if parentSlug != resolvedOrg.Slug {
		return nil, apierr.InvalidArgument(apierr.FieldViolation("parent",
			"org slug in parent does not match resolved scope"))
	}

	rows, err := filter.Query(ctx, s.db, s.filter, filter.QueryParams{
		Filter:      req.GetFilter(),
		ParentID:    resolvedOrg.ID.String(),
		OrderBy:     req.GetOrderBy(),
		PageSize:    req.GetPageSize(),
		Cursor:      req.GetPageToken(),
		ShowDeleted: req.GetShowDeleted(),
		Codec:       s.codec,
	})
	if err != nil {
		return nil, apierr.InvalidArgument(apierr.FieldViolation("filter", err.Error()))
	}

	results, err := filter.ScanSpaces(rows)
	if err != nil {
		return nil, apierr.Internal("database error")
	}

	pageSize := req.GetPageSize()
	if pageSize <= 0 {
		pageSize = 100
	}
	if pageSize > 1000 {
		pageSize = 1000
	}

	var nextPageToken string
	if int32(len(results)) > pageSize {
		nextPageToken, err = filter.EncodeNextPageToken(s.codec, results[pageSize].ID)
		if err != nil {
			return nil, apierr.Internal("encode page token")
		}
		results = results[:pageSize]
	}

	spaces := make([]*apiv1.Space, 0, len(results))
	for _, r := range results {
		spaces = append(spaces, convert.SpaceToProto(r, resolvedOrg.Slug))
	}

	return &apiv1.ListSpacesResponse{
		Spaces:        spaces,
		NextPageToken: nextPageToken,
	}, nil
}

// CreateSpace creates a new space under the parent org and seeds an
// owner-role binding for the caller in the same transaction. The
// founder binding establishes "≥1 owner per space" by definition for
// new spaces from this point forward (mirrors CreateOrganization's
// founder-bootstrap pattern).
//
// The caller must already have an org-scope users row (the per-org
// identity used as the principal_id on org_members and space_members).
// In production this is guaranteed: the caller reached us through the
// permission interceptor with a `spaces.create` permission, which is
// only granted via an org-level role binding — and that binding
// requires a users row.
func (s *SpacesServer) CreateSpace(ctx context.Context, req *apiv1.CreateSpaceRequest) (*longrunningpb.Operation, error) {
	space := req.GetSpace()

	parentSlug, err := parseSpaceParent(req.GetParent())
	if err != nil {
		return nil, err
	}
	resolvedOrg := server.MustResolvedOrgFromContext(ctx)
	if parentSlug != resolvedOrg.Slug {
		return nil, apierr.InvalidArgument(apierr.FieldViolation("parent",
			"org slug in parent does not match resolved scope"))
	}

	callerFirebaseID, err := s.caller(ctx)
	if err != nil {
		return nil, err
	}

	spaceName := req.GetSpaceId()
	if spaceName == "" {
		spaceName = uuid.New().String()[:8]
	}

	var labelsJSON json.RawMessage
	if labels := space.GetLabels(); labels != nil {
		labelsJSON, _ = json.Marshal(labels)
	} else {
		labelsJSON = json.RawMessage("{}")
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "create space: begin tx failed", "error", err)
		return nil, apierr.Internal("begin transaction")
	}
	defer func() { _ = tx.Rollback(ctx) }()

	qtx := db.New(tx)
	createdBy := callerFirebaseID.String()

	// Resolve the founder's per-org user row inside the tx so a
	// concurrent DeleteUser can't soft-delete the row between this
	// lookup and the space_member insert below — without this we'd
	// write a binding pointing at a doomed user (space_members.
	// principal_id has no FK to users; PG can't catch the dangling
	// reference for us).
	founderUser, err := qtx.GetUserMembership(ctx, db.GetUserMembershipParams{
		OrgID:              resolvedOrg.ID,
		FirebaseIdentityID: callerFirebaseID,
	})
	if err != nil {
		// The interceptor admitted this caller via an org-level
		// binding, so the users row must exist. Reaching here means
		// either a concurrent DeleteUser raced us into the tx or a
		// server invariant is violated; surface Internal either way.
		slog.ErrorContext(ctx, "create space: caller has no per-org user row",
			"org_id", resolvedOrg.ID, "firebase_identity_id", callerFirebaseID, "error", err)
		return nil, apierr.Internal("resolve caller user row")
	}

	result, err := qtx.CreateSpace(ctx, db.CreateSpaceParams{
		ID:          uuid.New(),
		OrgID:       resolvedOrg.ID,
		Name:        spaceName,
		DisplayName: space.GetDisplayName(),
		Labels:      labelsJSON,
		CreatedBy:   createdBy,
	})
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Space", "")
	}

	// Resolve the org-level owner role and seed a space-level owner
	// binding for the founder. System roles are seeded per-org at
	// CreateOrganization time, so the lookup must succeed for any
	// reachable org.
	ownerRole, err := qtx.GetSystemRole(ctx, db.GetSystemRoleParams{
		OrgID: resolvedOrg.ID,
		Name:  permission.RoleOwner,
	})
	if err != nil {
		slog.ErrorContext(ctx, "create space: owner role lookup failed",
			"org_id", resolvedOrg.ID, "error", err)
		return nil, apierr.Internal("resolve owner role")
	}
	if _, err := qtx.CreateSpaceMember(ctx, db.CreateSpaceMemberParams{
		ID:            uuid.New(),
		SpaceID:       result.ID,
		RoleID:        ownerRole.ID,
		PrincipalKind: db.PrincipalKindUser,
		PrincipalID:   founderUser.ID,
		CreatedBy:     createdBy,
	}); err != nil {
		slog.ErrorContext(ctx, "create space: seed founder owner binding failed",
			"space_id", result.ID, "error", err)
		return nil, apierr.Internal("seed founder owner binding")
	}

	if err := tx.Commit(ctx); err != nil {
		slog.ErrorContext(ctx, "create space: commit failed", "space_id", result.ID, "error", err)
		return nil, apierr.Internal("commit transaction")
	}

	return lro.DoneOperation(convert.SpaceToProto(result, resolvedOrg.Slug))
}

func (s *SpacesServer) UpdateSpace(ctx context.Context, req *apiv1.UpdateSpaceRequest) (*longrunningpb.Operation, error) {
	space := req.GetSpace()
	orgName, spaceName, err := parseSpaceName(space.GetName())
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Space", space.GetName())
	}
	resolvedOrg := server.MustResolvedOrgFromContext(ctx)
	resolvedSpace := server.MustResolvedSpaceFromContext(ctx)
	if orgName != resolvedOrg.Slug || spaceName != resolvedSpace.Slug {
		return nil, apierr.InvalidArgument(apierr.FieldViolation("space.name",
			"space path does not match resolved scope"))
	}
	// State guard lives at the gate (enforceSpaceSoftDeleteGate); a
	// non-ACTIVE space is already rejected before the handler runs.

	caller, err := s.caller(ctx)
	if err != nil {
		return nil, err
	}

	updateParams := db.UpdateSpaceParams{
		ID:        resolvedSpace.ID,
		UpdatedBy: caller.String(),
	}

	mask := req.GetUpdateMask()
	if mask != nil {
		for _, path := range mask.GetPaths() {
			switch path {
			case "display_name":
				updateParams.DisplayName = pgtype.Text{String: space.GetDisplayName(), Valid: true}
			case "labels":
				labelsJSON, err := json.Marshal(space.GetLabels())
				if err != nil {
					return nil, apierr.Internal("failed to marshal labels")
				}
				updateParams.Labels = labelsJSON
			}
		}
	} else {
		updateParams.DisplayName = pgtype.Text{String: space.GetDisplayName(), Valid: true}
		if labels := space.GetLabels(); labels != nil {
			labelsJSON, _ := json.Marshal(labels)
			updateParams.Labels = labelsJSON
		} else {
			updateParams.Labels = resolvedSpace.Row.Labels
		}
	}

	result, err := s.queries.UpdateSpace(ctx, updateParams)
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Space", space.GetName())
	}

	return lro.DoneOperation(convert.SpaceToProto(result, resolvedOrg.Slug))
}

func (s *SpacesServer) DeleteSpace(ctx context.Context, req *apiv1.DeleteSpaceRequest) (*longrunningpb.Operation, error) {
	orgName, spaceName, err := parseSpaceName(req.GetName())
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Space", req.GetName())
	}
	resolvedOrg := server.MustResolvedOrgFromContext(ctx)
	resolvedSpace := server.MustResolvedSpaceFromContext(ctx)
	if orgName != resolvedOrg.Slug || spaceName != resolvedSpace.Slug {
		return nil, apierr.InvalidArgument(apierr.FieldViolation("name",
			"space path does not match resolved scope"))
	}
	if resolvedSpace.Row.State != db.ResourceStateACTIVE {
		return nil, apierr.FailedPrecondition(
			"space is not in ACTIVE state; current state is " + string(resolvedSpace.Row.State))
	}

	caller, err := s.caller(ctx)
	if err != nil {
		return nil, err
	}

	result, err := s.queries.SoftDeleteSpace(ctx, db.SoftDeleteSpaceParams{
		ID:        resolvedSpace.ID,
		DeletedBy: caller.String(),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Lost the race with a concurrent delete — surface
			// FailedPrecondition rather than NotFound; the caller's
			// view of the row is now stale.
			return nil, apierr.FailedPrecondition("space is not in ACTIVE state")
		}
		return nil, apierr.HandleResourceError(err, "Space", req.GetName())
	}
	return lro.DoneOperation(convert.SpaceToProto(result, resolvedOrg.Slug))
}

func (s *SpacesServer) UndeleteSpace(ctx context.Context, req *apiv1.UndeleteSpaceRequest) (*longrunningpb.Operation, error) {
	orgName, spaceName, err := parseSpaceName(req.GetName())
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Space", req.GetName())
	}
	resolvedOrg := server.MustResolvedOrgFromContext(ctx)
	resolvedSpace := server.MustResolvedSpaceFromContext(ctx)
	if orgName != resolvedOrg.Slug || spaceName != resolvedSpace.Slug {
		return nil, apierr.InvalidArgument(apierr.FieldViolation("name",
			"space path does not match resolved scope"))
	}
	if resolvedSpace.Row.State != db.ResourceStateDELETEREQUESTED {
		return nil, apierr.FailedPrecondition(
			"space is not in DELETE_REQUESTED state; current state is " + string(resolvedSpace.Row.State))
	}

	caller, err := s.caller(ctx)
	if err != nil {
		return nil, err
	}

	result, err := s.queries.UndeleteSpace(ctx, db.UndeleteSpaceParams{
		ID:        resolvedSpace.ID,
		UpdatedBy: caller.String(),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apierr.FailedPrecondition("space is not in DELETE_REQUESTED state")
		}
		return nil, apierr.HandleResourceError(err, "Space", req.GetName())
	}
	return lro.DoneOperation(convert.SpaceToProto(result, resolvedOrg.Slug))
}
