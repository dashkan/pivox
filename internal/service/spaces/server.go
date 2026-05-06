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

	"google.golang.org/protobuf/proto"
)

type SpacesServer struct {
	apiv1.UnimplementedSpacesServer
	pool       db.RWPool
	queries    db.Querier
	filter     *filter.ResourceFilter
	codec      *appkey.Codec
	resolver   *permission.Resolver
	caller     server.CallerIdentityResolver
	audit      *audit.Resolver
	lroManager *lro.Manager
}

// Config is the constructor input for SpacesServer. `resolver` and
// `caller` are only consumed by the IAM-shaped handlers
// (TestIamPermissions and space-scope Member CRUD). `Pool` is used
// both for filter reads (db.DBTX) and tx-wrapped writes (TxBeginner);
// *pgxpool.Pool satisfies both. Tests that need to mock the tx
// surface build a SpacesServer literal directly with the local
// TxBeginner interface, mirroring the OrganizationsServer test
// pattern.
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
	// Caller resolves the caller identity. Required in production;
	// unit tests stub via struct literal.
	Caller server.CallerIdentityResolver
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
	if cfg.Caller == nil {
		panic("spaces: Config.Caller is required")
	}
	return &SpacesServer{
		pool:       cfg.Pool,
		queries:    cfg.Queries,
		filter:     filter.SpaceFilter(),
		codec:      cfg.Codec,
		resolver:   cfg.Resolver,
		caller:     cfg.Caller,
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
		return nil, apierr.Internal("resolve actors")
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

	rows, err := filter.Query(ctx, s.pool, s.filter, filter.QueryParams{
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

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		slog.ErrorContext(ctx, "create space: begin tx failed", "error", err)
		return nil, apierr.Internal("begin transaction")
	}
	defer func() { _ = tx.Rollback(ctx) }()

	qtx := db.New(tx)
	// Post-Phase-7 the founder's principal_id IS the caller's
	// identity_id — no per-org `users` row to resolve.
	// `callerFirebaseID` is the identities.id (resolved
	// via s.caller(ctx) which itself reads the verified token).
	founderID := callerFirebaseID
	createdBy := convert.PgUUID(callerFirebaseID)

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
	if _, err := qtx.CreateSpaceUserMember(ctx, db.CreateSpaceUserMemberParams{
		ID:        uuid.New(),
		SpaceID:   result.ID,
		RoleID:    ownerRole.ID,
		UserID:    convert.PgUUID(founderID),
		CreatedBy: createdBy,
	}); err != nil {
		slog.ErrorContext(ctx, "create space: seed founder owner binding failed",
			"space_id", result.ID, "error", err)
		return nil, apierr.Internal("seed founder owner binding")
	}

	if err := tx.Commit(ctx); err != nil {
		slog.ErrorContext(ctx, "create space: commit failed", "space_id", result.ID, "error", err)
		return nil, apierr.Internal("commit transaction")
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

	caller, err := s.caller(ctx)
	if err != nil {
		return nil, err
	}

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

	caller, err := s.caller(ctx)
	if err != nil {
		return nil, err
	}

	spaceRsrc := "organizations/" + resolvedOrg.Slug + "/spaces/" + resolvedSpace.Slug
	deletedBy := convert.PgUUID(caller)
	force := req.GetForce()
	expectedEtag := resolvedSpace.Row.Etag

	initialMeta := &apiv1.DeleteSpaceMetadata{
		Phase: apiv1.DeleteSpaceMetadata_VALIDATING,
		Space: spaceRsrc,
	}

	return s.lroManager.CreateAndRun(ctx, spaceRsrc, initialMeta,
		func(workCtx context.Context, progress lro.Progress) (proto.Message, error) {
			return s.runDeleteSpace(workCtx, progress, resolvedSpace.ID, resolvedOrg.Slug, spaceRsrc, deletedBy, force, expectedEtag)
		})
}

// runDeleteSpace orchestrates the DeleteSpace LRO. force=false drives
// MARKING_DELETED → COMPLETED; force=true drives PURGING → COMPLETED.
// The proto enum reserves CANCELLING_OPERATIONS for a future
// space-scoped-LRO cancellation phase, but no such LROs exist today
// so this orchestrator does not emit it.
func (s *SpacesServer) runDeleteSpace(
	ctx context.Context,
	progress lro.Progress,
	spaceID uuid.UUID,
	orgSlug, spaceRsrc string, deletedBy pgtype.UUID,
	force bool,
	expectedEtag string,
) (proto.Message, error) {
	updatePhase := func(phase apiv1.DeleteSpaceMetadata_Phase) {
		progress.Update(ctx, &apiv1.DeleteSpaceMetadata{
			Phase: phase,
			Space: spaceRsrc,
		})
	}

	if force {
		// PURGING: hard-delete the space. FK ON DELETE CASCADE
		// removes space_members, assets, and asset_requests
		// transitively. Etag-guarded so a concurrent state mutation
		// between handler validation and LRO worker execution
		// refuses to purge a row the caller didn't approve.
		updatePhase(apiv1.DeleteSpaceMetadata_PURGING)
		if _, err := s.queries.PurgeSpace(ctx, db.PurgeSpaceParams{
			ID:   spaceID,
			Etag: expectedEtag,
		}); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, apierr.FailedPrecondition(
					"space revision changed since delete was requested; refresh and retry")
			}
			slog.ErrorContext(ctx, "delete space: purge failed", "space", spaceRsrc, "error", err)
			return nil, apierr.Internal("purge space")
		}
		updatePhase(apiv1.DeleteSpaceMetadata_COMPLETED)
		// Force path: row is gone. Surface the resource name only
		// (no row to return).
		return &apiv1.Space{Name: spaceRsrc}, nil
	}

	// MARKING_DELETED: soft-delete path. SoftDeleteSpace refuses to
	// fire on a non-ACTIVE row (delete_time IS NULL guard), so a
	// race with a concurrent delete surfaces no-rows.
	updatePhase(apiv1.DeleteSpaceMetadata_MARKING_DELETED)
	updated, err := s.queries.SoftDeleteSpace(ctx, db.SoftDeleteSpaceParams{
		ID:        spaceID,
		DeletedBy: deletedBy,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apierr.FailedPrecondition(
				"space state changed; cannot soft-delete (was it already deleted?)")
		}
		slog.ErrorContext(ctx, "delete space: soft-delete failed", "space", spaceRsrc, "error", err)
		return nil, apierr.Internal("soft-delete space")
	}
	updatePhase(apiv1.DeleteSpaceMetadata_COMPLETED)
	// Best-effort enrichment after commit.
	actors, resolveErr := s.resolveSpaceActors(ctx, []db.Space{updated})
	if resolveErr != nil {
		slog.WarnContext(ctx, "delete space: actor resolution failed; returning proto without audit actors",
			"space", spaceRsrc, "error", resolveErr)
		actors = nil
	}
	return convert.SpaceToProto(updated, orgSlug, actors), nil
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

	caller, err := s.caller(ctx)
	if err != nil {
		return nil, err
	}

	spaceID := resolvedSpace.ID
	orgSlug := resolvedOrg.Slug
	updatedBy := convert.PgUUID(caller)
	spaceRsrc := "organizations/" + orgSlug + "/spaces/" + resolvedSpace.Slug
	initialMeta := &apiv1.UndeleteSpaceMetadata{Space: spaceRsrc}

	return s.lroManager.CreateAndRun(ctx, spaceRsrc, initialMeta,
		func(workCtx context.Context, _ lro.Progress) (proto.Message, error) {
			updated, err := s.queries.UndeleteSpace(workCtx, db.UndeleteSpaceParams{
				ID:        spaceID,
				UpdatedBy: updatedBy,
			})
			if err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					// Either left DELETE_REQUESTED in a race or
					// purge_time elapsed before the worker fired.
					return nil, apierr.FailedPrecondition(
						"space is no longer in DELETE_REQUESTED state (was it purged or restored concurrently?)")
				}
				slog.ErrorContext(workCtx, "undelete space failed", "space", spaceRsrc, "error", err)
				return nil, apierr.Internal("undelete space")
			}
			// Best-effort enrichment after commit.
			actors, resolveErr := s.resolveSpaceActors(workCtx, []db.Space{updated})
			if resolveErr != nil {
				slog.WarnContext(workCtx, "undelete space: actor resolution failed; returning proto without audit actors",
					"space", spaceRsrc, "error", resolveErr)
				actors = nil
			}
			return convert.SpaceToProto(updated, orgSlug, actors), nil
		})
}
