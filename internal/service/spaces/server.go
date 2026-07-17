package spaces

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"cloud.google.com/go/longrunning/autogen/longrunningpb"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dashkan/pivox/internal/apierr"
	"github.com/dashkan/pivox/internal/appkey"
	"github.com/dashkan/pivox/internal/audit"
	"github.com/dashkan/pivox/internal/convert"
	db "github.com/dashkan/pivox/internal/db/generated"
	"github.com/dashkan/pivox/internal/filter"
	"github.com/dashkan/pivox/internal/lro"
	"github.com/dashkan/pivox/internal/permission"
	apiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/api/v1"
	typespb "github.com/dashkan/pivox/internal/pkg/gen/pivox/types"
	"github.com/dashkan/pivox/internal/server"
	"github.com/dashkan/pivox/internal/workers"
)

type SpacesServer struct {
	apiv1.UnimplementedSpacesServer
	pool       db.RWPool
	queries    db.Querier
	filter     *filter.ResourceFilter
	codec      *appkey.Codec
	resolver   *permission.Resolver
	audit      *audit.Resolver
	lroManager *lro.Manager
}

// Config is the constructor input for SpacesServer. `Resolver` is
// only consumed by the IAM-shaped handlers (TestIamPermissions and
// space-scope Member CRUD). `Pool` is used both for filter reads
// (db.DBTX) and tx-wrapped writes (TxBeginner); *pgxpool.Pool
// satisfies both. Tests that need to mock the tx surface build a
// SpacesServer literal directly with the local TxBeginner interface,
// mirroring the OrganizationsServer test pattern.
type Config struct {
	// Pool is the database pool. Required.
	Pool *pgxpool.Pool
	// Queries is the sqlc query interface. Required.
	Queries db.Querier
	// Codec opaque-encodes resource names. Required.
	Codec *appkey.Codec
	// Resolver gates per-resource permission checks. Optional;
	// nil is acceptable in unit tests that don't exercise the
	// permission paths.
	Resolver *permission.Resolver
	// AuditResolver inflates audit-field UUIDs into Actor protos.
	// Optional; nil leaves Actor fields unset.
	AuditResolver *audit.Resolver
	// LROManager drives the async orchestrators for
	// DeleteSpace/UndeleteSpace. Optional in tests that don't
	// exercise lifecycle paths.
	LROManager *lro.Manager
}

// NewSpacesServer constructs the server from cfg. Panics on a
// missing required field — a startup-time programmer error rather
// than a runtime failure.
func NewSpacesServer(cfg Config) *SpacesServer {
	if cfg.Pool == nil {
		panic("spaces: Config.Pool is required")
	}
	if cfg.Queries == nil {
		panic("spaces: Config.Queries is required")
	}
	if cfg.Codec == nil {
		panic("spaces: Config.Codec is required")
	}
	return &SpacesServer{
		pool:       cfg.Pool,
		queries:    cfg.Queries,
		filter:     filter.SpaceFilter(),
		codec:      cfg.Codec,
		resolver:   cfg.Resolver,
		audit:      cfg.AuditResolver,
		lroManager: cfg.LROManager,
	}
}

// resolveSpaceActors gathers the union of *_by UUIDs across the page
// and resolves them in a single batched call. Returns nil when no
// audit resolver is wired.
func (s *SpacesServer) resolveSpaceActors(ctx context.Context, spaces []db.Space) (map[uuid.UUID]*typespb.Actor, error) {
	if s.audit == nil {
		return nil, nil
	}
	ids := make([]uuid.UUID, 0, len(spaces)*3)
	for _, p := range spaces {
		if p.CreatedBy.Valid {
			ids = append(ids, p.CreatedBy.Bytes)
		}
		if p.UpdatedBy.Valid {
			ids = append(ids, p.UpdatedBy.Bytes)
		}
		if p.DeletedBy.Valid {
			ids = append(ids, p.DeletedBy.Bytes)
		}
	}
	actors, err := s.audit.Resolve(ctx, ids)
	if err != nil {
		slog.ErrorContext(ctx, "resolve space actors failed", "error", err)
		return nil, apierr.Internal(err, "resolve actors")
	}
	return actors, nil
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
	actors, err := s.resolveSpaceActors(ctx, []db.Space{resolvedSpace.Row})
	if err != nil {
		return nil, err
	}
	return convert.SpaceToProto(resolvedSpace.Row, resolvedOrg.Slug, actors), nil
}

// ListSpaces is a dynamic AIP-160 filtered + AIP-132 sorted + compound-cursor
// keyset list. The interceptor-resolved org is the NON-NEGOTIABLE base scope
// (org_id = $), applied as the base of the query; the request's filter/order_by
// layer ON TOP of it and can only narrow, never widen. Every value (org id,
// filter operands, cursor values, page size) is bound as a $N parameter by
// filter.BuildListQuery — nothing is string-interpolated — and column/direction
// come only from SpaceFilter's whitelist.
//
// This replaced the legacy id-only filter.Query path, which paired an id-only
// cursor with NON-id sortable columns: an order_by=displayName produced
// `ORDER BY display_name` but resumed on `id > cursor`, so sort and keyset
// disagreed and rows dropped/duplicated across page boundaries. The compound
// (sortCol, id) cursor via PlanOrderBy/EncodeCursor/DecodeCursor fixes that.
// See docs/aip-list-transpiler-procedure.md.
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

	rf := s.filter
	pageSize := filter.ClampPageSize(rf, req.GetPageSize())

	// Resolve order_by against the sortable whitelist (default: id). The plan
	// also tells the cursor codec whether the sort value is a timestamp.
	plan, err := filter.PlanOrderBy(rf, req.GetOrderBy())
	if err != nil {
		return nil, apierr.InvalidArgument(apierr.FieldViolation("order_by", err.Error()))
	}
	cursor, err := filter.DecodeCursor(s.codec, plan, req.GetPageToken())
	if err != nil {
		return nil, apierr.InvalidArgument(apierr.FieldViolation("page_token", "invalid or malformed"))
	}

	sql, args, err := filter.BuildListQuery(filter.ListQuery{
		Resource:    rf,
		Base:        []filter.Predicate{{SQL: "org_id = %s", Arg: resolvedOrg.ID}},
		Filter:      req.GetFilter(),
		Order:       plan,
		PageSize:    pageSize,
		Cursor:      cursor,
		ShowDeleted: req.GetShowDeleted(),
	})
	if err != nil {
		// The only error source is the filter transpiler (bad user filter).
		return nil, apierr.InvalidArgument(apierr.FieldViolation("filter", err.Error()))
	}

	pgxRows, err := s.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, apierr.Internal(err, "list spaces")
	}
	results, err := filter.ScanSpaces(pgxRows)
	if err != nil {
		return nil, apierr.Internal(err, "list spaces")
	}

	// filter.Paginate trims the over-fetched result to pageSize and derives the
	// next-page token from the LAST RETURNED row via the compound cursor —
	// encoding (sortValue, id) so the resume predicate matches the ORDER BY.
	results, nextPageToken, err := filter.Paginate(results, int(pageSize), func(last db.Space) (string, error) {
		return filter.EncodeCursor(s.codec, plan, spaceSortValue(plan, last), last.ID)
	})
	if err != nil {
		return nil, apierr.Internal(err, "encode page token")
	}

	actors, err := s.resolveSpaceActors(ctx, results)
	if err != nil {
		return nil, err
	}
	spaces := make([]*apiv1.Space, 0, len(results))
	for _, r := range results {
		spaces = append(spaces, convert.SpaceToProto(r, resolvedOrg.Slug, actors))
	}

	return &apiv1.ListSpacesResponse{
		Spaces:        spaces,
		NextPageToken: nextPageToken,
	}, nil
}

// spaceSortValue renders the active order_by column's value for the given row
// as the string the compound page token carries. Timestamps use RFC3339Nano so
// filter.DecodeCursor can parse them back to an exact time.Time. For the default
// id ordering (plan.Field == "") the value is unused (EncodeCursor emits the
// id-only token), so "" is returned.
func spaceSortValue(plan filter.OrderByPlan, r db.Space) string {
	switch plan.Field {
	case "displayName":
		return r.DisplayName
	case "name":
		return r.Name
	case "createTime":
		return r.CreateTime.UTC().Format(time.RFC3339Nano)
	default:
		return ""
	}
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

	callerID := server.MustUserID(ctx)

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

	// Post-Phase-7 the founder's principal_id IS the caller's
	// identity_id — no per-org `users` row to resolve.
	// `callerID` is the identities.id (the universal user UUID,
	// set by AuthInterceptor from the verified token's
	// `sub`).
	founderID := callerID
	createdBy := convert.PgUUID(callerID)

	// validate_only runs the whole bootstrap tx (space insert + owner
	// binding) against real constraints and rolls it back, so a would-fail
	// request (e.g. duplicate slug) returns the same error a live one would
	// while persisting nothing.
	result, err := db.RunInTxValidate(ctx, s.pool, req.GetValidateOnly(), func(qtx db.Querier) (db.Space, error) {
		result, err := qtx.CreateSpace(ctx, db.CreateSpaceParams{
			ID:          uuid.New(),
			OrgID:       resolvedOrg.ID,
			Name:        spaceName,
			DisplayName: space.GetDisplayName(),
			Labels:      labelsJSON,
			CreatedBy:   createdBy,
		})
		if err != nil {
			return db.Space{}, apierr.HandleResourceError(err, "Space", "")
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
			return db.Space{}, apierr.Internal(err, "resolve owner role")
		}
		if _, err := qtx.CreateSpaceUserMember(ctx, db.CreateSpaceUserMemberParams{
			ID:        uuid.New(),
			SpaceID:   result.ID,
			RoleID:    ownerRole.ID,
			UserID:    convert.PgUUID(founderID),
			CreatedBy: createdBy,
		}); err != nil {
			slog.ErrorContext(ctx, "create space: seed founder owner binding failed",
				"space_id", result.ID, "error", err)
			return db.Space{}, apierr.Internal(err, "seed founder owner binding")
		}
		return result, nil
	})
	if err != nil {
		return nil, err
	}

	// Best-effort enrichment after commit: state has landed, don't
	// fail the create on a transient identity lookup error.
	actors, resolveErr := s.resolveSpaceActors(ctx, []db.Space{result})
	if resolveErr != nil {
		slog.WarnContext(ctx, "create space: actor resolution failed; returning proto without audit actors",
			"space_id", result.ID, "error", resolveErr)
		actors = nil
	}
	return lro.DoneOperation(convert.SpaceToProto(result, resolvedOrg.Slug, actors))
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

	caller := server.MustUserID(ctx)

	updateParams := db.UpdateSpaceParams{
		ID:        resolvedSpace.ID,
		UpdatedBy: convert.PgUUID(caller),
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
					return nil, apierr.Internal(err, "failed to marshal labels")
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

	// validate_only runs the UPDATE against real constraints and rolls it
	// back, so a would-fail request returns the same error a live one would
	// while persisting nothing.
	result, err := db.RunInTxValidate(ctx, s.pool, req.GetValidateOnly(), func(qtx db.Querier) (db.Space, error) {
		return qtx.UpdateSpace(ctx, updateParams)
	})
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Space", space.GetName())
	}

	// Best-effort enrichment after commit.
	actors, resolveErr := s.resolveSpaceActors(ctx, []db.Space{result})
	if resolveErr != nil {
		slog.WarnContext(ctx, "update space: actor resolution failed; returning proto without audit actors",
			"space_id", result.ID, "error", resolveErr)
		actors = nil
	}
	return lro.DoneOperation(convert.SpaceToProto(result, resolvedOrg.Slug, actors))
}

// DeleteSpace soft-deletes a space (force=false) or hard-deletes it
// with a synchronous cascade (force=true). Mirrors the
// DeleteOrganization shape: state validation + etag pinning at the
// handler, phase-tracked LRO for the actual work. The purge worker
// (workers.SpacePurgeWorker) runs the soft-delete cascade after
// `purge_time` for force=false.
//
// force=true requires a non-empty etag pinning the row revision the
// client read — destructive ops that bypass the grace window must
// match the caller's view.
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
	if req.GetForce() && req.GetEtag() == "" {
		return nil, apierr.FailedPrecondition(
			"force=true requires a non-empty etag pinning the space revision")
	}
	if req.GetEtag() != "" && req.GetEtag() != resolvedSpace.Row.Etag {
		return nil, apierr.FailedPrecondition(
			"etag mismatch; refresh the space and retry")
	}

	caller := server.MustUserID(ctx)

	spaceRsrc := "organizations/" + resolvedOrg.Slug + "/spaces/" + resolvedSpace.Slug
	force := req.GetForce()
	expectedEtag := resolvedSpace.Row.Etag

	initialMeta := &apiv1.DeleteSpaceMetadata{
		Phase: apiv1.DeleteSpaceMetadata_VALIDATING,
		Space: spaceRsrc,
	}

	// River-backed: pivox-cloud enqueues the job + creates the
	// operations row in one tx; pivox-worker's DeleteSpaceWorker
	// runs the SQL action and marks the operation done atomically.
	opID := uuid.New()
	return s.lroManager.NewLro(ctx, spaceRsrc, lro.NewLroOpts{
		OperationID:  opID,
		SpaceID:      convert.PgUUID(resolvedSpace.ID),
		CreatedBy:    convert.PgUUID(caller),
		ValidateOnly: req.GetValidateOnly(),
		JobArgs: workers.DeleteSpaceArgs{
			OperationID:  opID,
			SpaceID:      resolvedSpace.ID,
			OrgSlug:      resolvedOrg.Slug,
			Resource:     spaceRsrc,
			DeletedBy:    caller,
			Force:        force,
			ExpectedEtag: expectedEtag,
		},
		Metadata: initialMeta,
	})
}

// UndeleteSpace restores a soft-deleted space back to ACTIVE. Only
// callable during the 30-day grace window — once `purge_time`
// elapses the row is purged by SpacePurgeWorker and there's no way
// back. Returns an LRO mirroring the AIP-164 shape; the actual work
// is a single UPDATE so the LRO completes promptly.
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
	if req.GetEtag() != "" && req.GetEtag() != resolvedSpace.Row.Etag {
		return nil, apierr.FailedPrecondition(
			"etag mismatch; refresh the space and retry")
	}

	caller := server.MustUserID(ctx)

	spaceID := resolvedSpace.ID
	orgSlug := resolvedOrg.Slug
	spaceRsrc := "organizations/" + orgSlug + "/spaces/" + resolvedSpace.Slug
	initialMeta := &apiv1.UndeleteSpaceMetadata{Space: spaceRsrc}

	// River-backed: pivox-cloud enqueues the job + creates the
	// operations row in one tx; pivox-worker's UndeleteSpaceWorker
	// runs the SQL action and marks the operation done.
	opID := uuid.New()
	return s.lroManager.NewLro(ctx, spaceRsrc, lro.NewLroOpts{
		OperationID:  opID,
		SpaceID:      convert.PgUUID(spaceID),
		CreatedBy:    convert.PgUUID(caller),
		ValidateOnly: req.GetValidateOnly(),
		JobArgs: workers.UndeleteSpaceArgs{
			OperationID: opID,
			SpaceID:     spaceID,
			OrgSlug:     orgSlug,
			UpdatedBy:   caller,
		},
		Metadata: initialMeta,
	})
}
