// Package workflows implements the Workflows and WorkflowVersions gRPC
// services — the workflow-engine definition layer. A Workflow is a container
// (metadata + persistent parameter config + a pointer to the promoted live
// version); its behavior lives in immutable WorkflowVersions. Workflows are
// uuid-named (the workflow_id is a user-assigned uniqueness slug, not part of
// the name); versions are children named by their monotonic version_number.
//
// This layer owns no plaintext credentials (those are Secrets, referenced by
// Connectors), so neither server takes an Encryptor.
package workflows

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"slices"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/structpb"
	"riverqueue.com/riverpro"

	"github.com/dashkan/pivox/internal/apierr"
	"github.com/dashkan/pivox/internal/appkey"
	"github.com/dashkan/pivox/internal/audit"
	"github.com/dashkan/pivox/internal/convert"
	db "github.com/dashkan/pivox/internal/db/generated"
	"github.com/dashkan/pivox/internal/filter"
	typespb "github.com/dashkan/pivox/internal/pkg/gen/pivox/types"
	workflowsv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/workflows/v1"
	"github.com/dashkan/pivox/internal/server"
)

// originOwned is the DB origin value for a customer-owned workflow. MANAGED
// workflows are Pivox-provisioned and never created through these RPCs.
const originOwned = "OWNED"

// Config is the constructor input for both servers in this package.
type Config struct {
	// Pool is the database pool (DBTX for reads, TxBeginner for tx writes).
	// Required.
	Pool db.RWPool
	// Queries is the sqlc query interface. Required.
	Queries db.Querier
	// Codec opaque-encodes page tokens. Required.
	Codec *appkey.Codec
	// AuditResolver inflates audit-field UUIDs into Actor protos. Optional.
	AuditResolver *audit.Resolver
	// River enqueues a run's execution job transactionally with the run's
	// INSERT (see WorkflowRunsServer.RunWorkflow). Required by
	// NewWorkflowRunsServer; unused by the container/version servers, so the
	// shared validate() does not check it.
	River *riverpro.Client[pgx.Tx]
}

func (cfg Config) validate(pkg string) {
	if cfg.Pool == nil {
		panic(pkg + ": Config.Pool is required")
	}
	if cfg.Queries == nil {
		panic(pkg + ": Config.Queries is required")
	}
	if cfg.Codec == nil {
		panic(pkg + ": Config.Codec is required")
	}
}

// WorkflowsServer serves the Workflows RPCs (the container layer).
type WorkflowsServer struct {
	workflowsv1.UnimplementedWorkflowsServer
	pool    db.RWPool
	queries db.Querier
	codec   *appkey.Codec
	audit   *audit.Resolver
}

// NewWorkflowsServer constructs the server from cfg. Panics on a missing
// required field.
func NewWorkflowsServer(cfg Config) *WorkflowsServer {
	cfg.validate("workflows")
	return &WorkflowsServer{
		pool:    cfg.Pool,
		queries: cfg.Queries,
		codec:   cfg.Codec,
		audit:   cfg.AuditResolver,
	}
}

// WorkflowVersionsServer serves the WorkflowVersions RPCs (the immutable
// definition layer, children of a Workflow).
type WorkflowVersionsServer struct {
	workflowsv1.UnimplementedWorkflowVersionsServer
	pool    db.RWPool
	queries db.Querier
	codec   *appkey.Codec
	audit   *audit.Resolver
}

// NewWorkflowVersionsServer constructs the server from cfg. Panics on a
// missing required field.
func NewWorkflowVersionsServer(cfg Config) *WorkflowVersionsServer {
	cfg.validate("workflows")
	return &WorkflowVersionsServer{
		pool:    cfg.Pool,
		queries: cfg.Queries,
		codec:   cfg.Codec,
		audit:   cfg.AuditResolver,
	}
}

// scope reads the interceptor-resolved org (always set) and space (set when
// the resource is space-scoped) from ctx. Returns the org id, the nullable
// space id, and the parent resource-name prefix.
func scope(ctx context.Context) (orgID uuid.UUID, spaceID pgtype.UUID, namePrefix string) {
	org := server.MustResolvedOrgFromContext(ctx)
	orgID = org.ID
	namePrefix = "organizations/" + org.Slug
	if space, ok := server.ResolvedSpaceFromContext(ctx); ok {
		spaceID = convert.PgUUID(space.ID)
		namePrefix += "/spaces/" + space.Slug
	}
	return orgID, spaceID, namePrefix
}

// parseWorkflowName extracts the leaf UUID from a full Workflow resource name
// ("organizations/{org}[/spaces/{space}]/workflows/{uuid}"). A malformed name
// or non-UUID leaf is NotFound (the named workflow doesn't exist).
func parseWorkflowName(name string) (uuid.UUID, error) {
	idx := strings.LastIndex(name, "/")
	if idx < 0 {
		return uuid.Nil, apierr.NotFound("Workflow", name)
	}
	id, err := uuid.Parse(name[idx+1:])
	if err != nil {
		return uuid.Nil, apierr.NotFound("Workflow", name)
	}
	return id, nil
}

// parseWorkflowVersionName splits a full WorkflowVersion resource name
// ("organizations/{org}[/spaces/{space}]/workflows/{wf-uuid}/versions/{n}")
// into the parent workflow's uuid and the monotonic version number. Any
// malformation is NotFound.
func parseWorkflowVersionName(name string) (workflowID uuid.UUID, versionNumber int64, err error) {
	const marker = "/versions/"
	idx := strings.LastIndex(name, marker)
	if idx < 0 {
		return uuid.Nil, 0, apierr.NotFound("WorkflowVersion", name)
	}
	num, perr := strconv.ParseInt(name[idx+len(marker):], 10, 64)
	if perr != nil || num <= 0 {
		return uuid.Nil, 0, apierr.NotFound("WorkflowVersion", name)
	}
	// The segment before "/versions/" is the parent Workflow name; its leaf
	// is the workflow uuid.
	wfName := name[:idx]
	li := strings.LastIndex(wfName, "/")
	if li < 0 {
		return uuid.Nil, 0, apierr.NotFound("WorkflowVersion", name)
	}
	id, perr := uuid.Parse(wfName[li+1:])
	if perr != nil {
		return uuid.Nil, 0, apierr.NotFound("WorkflowVersion", name)
	}
	return id, num, nil
}

// checkWorkflowScope enforces that a workflow fetched by its (global) uuid
// actually belongs to the caller's interceptor-resolved scope. Without this, a
// caller with access to org A could read/mutate a workflow in org B by
// crafting "organizations/A/workflows/{B-uuid}" — the interceptor gates on A
// but never verifies the uuid is A's. A mismatch is NotFound (don't leak
// existence).
func checkWorkflowScope(w db.Workflow, orgID uuid.UUID, spaceID pgtype.UUID, name string) error {
	if w.OrgID != orgID || w.SpaceID != spaceID {
		return apierr.NotFound("Workflow", name)
	}
	return nil
}

// marshalAnnotations renders the labels map as JSONB. Empty map → "{}" (the
// column is NOT NULL DEFAULT '{}').
func marshalAnnotations(m map[string]string) (json.RawMessage, error) {
	if len(m) == 0 {
		return json.RawMessage("{}"), nil
	}
	return json.Marshal(m)
}

// marshalWorkflowConfig renders the container's persistent parameter config
// (a google.protobuf.Struct) as JSONB. Nil/empty → "{}" (matches the column
// default), symmetric with convert.WorkflowToProto's read path.
func marshalWorkflowConfig(cfg *structpb.Struct) (json.RawMessage, error) {
	if cfg == nil || len(cfg.GetFields()) == 0 {
		return json.RawMessage("{}"), nil
	}
	b, err := protojson.Marshal(cfg)
	if err != nil {
		return nil, err
	}
	return b, nil
}

// marshalDefinition renders a version's definition (parameters + trigger +
// root) as JSONB via a scratch WorkflowVersion carrying only those fields —
// symmetric with convert.WorkflowVersionToProto's read path.
func marshalDefinition(in *workflowsv1.WorkflowVersion) (json.RawMessage, error) {
	scratch := &workflowsv1.WorkflowVersion{
		Parameters: in.GetParameters(),
		Trigger:    in.GetTrigger(),
		Root:       in.GetRoot(),
	}
	b, err := protojson.Marshal(scratch)
	if err != nil {
		return nil, err
	}
	return b, nil
}

// resolveActors batch-resolves the given identity ids into Actor protos.
// Returns nil when there's no resolver or nothing to resolve (leaving Actor
// fields unset rather than failing the whole response).
func resolveActors(ctx context.Context, resolver *audit.Resolver, ids []uuid.UUID) map[uuid.UUID]*typespb.Actor {
	if resolver == nil || len(ids) == 0 {
		return nil
	}
	actors, err := resolver.Resolve(ctx, ids)
	if err != nil {
		slog.WarnContext(ctx, "workflows: actor resolution failed; returning without actors", "error", err)
		return nil
	}
	return actors
}

// workflowActorIDs collects the created_by/updated_by ids across a page of
// workflow rows.
func workflowActorIDs(rows []db.Workflow) []uuid.UUID {
	ids := make([]uuid.UUID, 0, len(rows)*2)
	for _, r := range rows {
		if r.CreatedBy.Valid {
			ids = append(ids, r.CreatedBy.Bytes)
		}
		if r.UpdatedBy.Valid {
			ids = append(ids, r.UpdatedBy.Bytes)
		}
	}
	return ids
}

// resolveVersionNumbers maps each page row's promoted `version` uuid to its
// monotonic version_number, so WorkflowToProto can render the numbered
// resource name of the live version without an N+1 per-row lookup.
func resolveVersionNumbers(ctx context.Context, queries db.Querier, rows []db.Workflow) map[uuid.UUID]int64 {
	ids := make([]uuid.UUID, 0, len(rows))
	for _, r := range rows {
		if r.Version.Valid {
			ids = append(ids, r.Version.Bytes)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	got, err := queries.WorkflowVersionNumbersByIDs(ctx, ids)
	if err != nil {
		slog.WarnContext(ctx, "workflows: version-number resolution failed; version pointers omitted", "error", err)
		return nil
	}
	m := make(map[uuid.UUID]int64, len(got))
	for _, v := range got {
		m[v.ID] = v.VersionNumber
	}
	return m
}

// decodePageToken decodes an opaque keyset cursor into a pgtype.UUID.
func decodePageToken(codec *appkey.Codec, tok string) (pgtype.UUID, error) {
	var cursor pgtype.UUID
	if tok == "" {
		return cursor, nil
	}
	raw, err := codec.Decrypt(tok)
	if err != nil || len(raw) != 16 {
		return cursor, apierr.InvalidArgument(apierr.FieldViolation("page_token", "invalid or malformed"))
	}
	var id uuid.UUID
	copy(id[:], raw)
	return convert.PgUUID(id), nil
}

func clampPageSize(pageSize int32) int32 {
	if pageSize <= 0 {
		return 100
	}
	if pageSize > 1000 {
		return 1000
	}
	return pageSize
}

// ============================================================================
// WorkflowsServer
// ============================================================================

func (s *WorkflowsServer) ListWorkflows(ctx context.Context, req *workflowsv1.ListWorkflowsRequest) (*workflowsv1.ListWorkflowsResponse, error) {
	orgID, spaceID, prefix := scope(ctx)
	pageSize := clampPageSize(req.GetPageSize())

	cursor, err := decodePageToken(s.codec, req.GetPageToken())
	if err != nil {
		return nil, err
	}

	rows, err := s.queries.ListWorkflowsByParent(ctx, db.ListWorkflowsByParentParams{
		OrgID:     orgID,
		SpaceID:   spaceID,
		Cursor:    cursor,
		PageLimit: pageSize + 1,
	})
	if err != nil {
		return nil, apierr.Internal("list workflows")
	}

	var nextPageToken string
	if int32(len(rows)) > pageSize {
		nextPageToken, err = filter.EncodeNextPageToken(s.codec, rows[pageSize].ID)
		if err != nil {
			return nil, apierr.Internal("encode page token")
		}
		rows = rows[:pageSize]
	}

	actors := resolveActors(ctx, s.audit, workflowActorIDs(rows))
	versionNumbers := resolveVersionNumbers(ctx, s.queries, rows)
	out := make([]*workflowsv1.Workflow, 0, len(rows))
	for _, r := range rows {
		out = append(out, convert.WorkflowToProto(r, prefix, actors, versionNumbers))
	}
	return &workflowsv1.ListWorkflowsResponse{Workflows: out, NextPageToken: nextPageToken}, nil
}

func (s *WorkflowsServer) GetWorkflow(ctx context.Context, req *workflowsv1.GetWorkflowRequest) (*workflowsv1.Workflow, error) {
	id, err := parseWorkflowName(req.GetName())
	if err != nil {
		return nil, err
	}
	orgID, spaceID, prefix := scope(ctx)
	row, err := s.queries.GetWorkflow(ctx, id)
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Workflow", req.GetName())
	}
	if err := checkWorkflowScope(row, orgID, spaceID, req.GetName()); err != nil {
		return nil, err
	}
	return s.toProto(ctx, row, prefix), nil
}

func (s *WorkflowsServer) CreateWorkflow(ctx context.Context, req *workflowsv1.CreateWorkflowRequest) (*workflowsv1.Workflow, error) {
	workflowID := req.GetWorkflowId()
	if workflowID == "" {
		return nil, apierr.InvalidArgument(apierr.FieldViolation("workflow_id", "is required"))
	}
	in := req.GetWorkflow()
	// MANAGED workflows are Pivox-provisioned; a customer cannot create one.
	if in.GetOrigin() == workflowsv1.WorkflowOrigin_MANAGED {
		return nil, apierr.InvalidArgument(apierr.FieldViolation("workflow.origin",
			"MANAGED workflows are provisioned by Pivox and cannot be created"))
	}
	orgID, spaceID, prefix := scope(ctx)

	id, err := uuid.NewV7()
	if err != nil {
		return nil, apierr.Internal("generate workflow id")
	}
	config, err := marshalWorkflowConfig(in.GetConfig())
	if err != nil {
		return nil, apierr.InvalidArgument(apierr.FieldViolation("workflow.config", err.Error()))
	}
	annotations, err := marshalAnnotations(in.GetAnnotations())
	if err != nil {
		return nil, apierr.InvalidArgument(apierr.FieldViolation("workflow.annotations", err.Error()))
	}

	// A workflow is created version-less (version pointer NULL) and always
	// OWNED. validate_only rolls the insert back but still runs it, so a
	// would-fail request (e.g. duplicate workflow_id) fails identically.
	params := db.CreateWorkflowParams{
		ID:          id,
		OrgID:       orgID,
		SpaceID:     spaceID,
		WorkflowID:  workflowID,
		DisplayName: in.GetDisplayName(),
		Description: in.GetDescription(),
		Enabled:     in.GetEnabled(),
		Config:      config,
		Origin:      originOwned,
		Annotations: annotations,
		CreatedBy:   convert.PgUUID(server.MustUserID(ctx)),
	}
	row, err := db.RunInTxValidate(ctx, s.pool, req.GetValidateOnly(), func(qtx db.Querier) (db.Workflow, error) {
		return qtx.CreateWorkflow(ctx, params)
	})
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Workflow", workflowID)
	}
	return s.toProto(ctx, row, prefix), nil
}

func (s *WorkflowsServer) UpdateWorkflow(ctx context.Context, req *workflowsv1.UpdateWorkflowRequest) (*workflowsv1.Workflow, error) {
	in := req.GetWorkflow()
	id, err := parseWorkflowName(in.GetName())
	if err != nil {
		return nil, err
	}
	orgID, spaceID, prefix := scope(ctx)

	mask := req.GetUpdateMask()
	// An empty mask means all updatable container fields are significant (AIP
	// full-replace). `version` (promote) and `origin` are never set here.
	inScope := func(field string) bool {
		if mask == nil || len(mask.GetPaths()) == 0 {
			return true
		}
		return slices.Contains(mask.GetPaths(), field)
	}

	params := db.UpdateWorkflowParams{
		ID:        id,
		UpdatedBy: convert.PgUUID(server.MustUserID(ctx)),
	}
	if inScope("display_name") {
		params.DisplayName = pgtype.Text{String: in.GetDisplayName(), Valid: true}
	}
	if inScope("description") {
		params.Description = pgtype.Text{String: in.GetDescription(), Valid: true}
	}
	if inScope("enabled") {
		params.Enabled = pgtype.Bool{Bool: in.GetEnabled(), Valid: true}
	}
	if inScope("config") {
		config, err := marshalWorkflowConfig(in.GetConfig())
		if err != nil {
			return nil, apierr.InvalidArgument(apierr.FieldViolation("workflow.config", err.Error()))
		}
		params.Config = config
	}
	if inScope("annotations") {
		annotations, err := marshalAnnotations(in.GetAnnotations())
		if err != nil {
			return nil, apierr.InvalidArgument(apierr.FieldViolation("workflow.annotations", err.Error()))
		}
		params.Annotations = annotations
	}

	var row db.Workflow
	err = db.RunInTxVoidValidate(ctx, s.pool, req.GetValidateOnly(), func(qtx db.Querier) error {
		existing, err := qtx.GetWorkflowForUpdate(ctx, id)
		if err != nil {
			return apierr.HandleResourceError(err, "Workflow", in.GetName())
		}
		if err := checkWorkflowScope(existing, orgID, spaceID, in.GetName()); err != nil {
			return err
		}
		if etag := in.GetEtag(); etag != "" && etag != existing.Etag {
			return apierr.Aborted("Workflow", in.GetName(), "etag mismatch")
		}
		row, err = qtx.UpdateWorkflow(ctx, params)
		if err != nil {
			return apierr.HandleResourceError(err, "Workflow", in.GetName())
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.toProto(ctx, row, prefix), nil
}

func (s *WorkflowsServer) DeleteWorkflow(ctx context.Context, req *workflowsv1.DeleteWorkflowRequest) (*emptypb.Empty, error) {
	id, err := parseWorkflowName(req.GetName())
	if err != nil {
		return nil, err
	}
	orgID, spaceID, _ := scope(ctx)
	err = db.RunInTxVoidValidate(ctx, s.pool, req.GetValidateOnly(), func(qtx db.Querier) error {
		existing, err := qtx.GetWorkflowForUpdate(ctx, id)
		if err != nil {
			return apierr.HandleResourceError(err, "Workflow", req.GetName())
		}
		if err := checkWorkflowScope(existing, orgID, spaceID, req.GetName()); err != nil {
			return err
		}
		if etag := req.GetEtag(); etag != "" && etag != existing.Etag {
			return apierr.Aborted("Workflow", req.GetName(), "etag mismatch")
		}
		active, err := qtx.CountActiveWorkflowRuns(ctx, id)
		if err != nil {
			return apierr.Internal("check active runs")
		}
		if active > 0 {
			if !req.GetForce() {
				return apierr.FailedPrecondition("workflow has active runs; cancel them or use force")
			}
			// force: cancel the active runs (DB state only) before the delete
			// cascades them away.
			// Phase 6: also stop the River job backing each cancelled run.
			if err := qtx.CancelActiveWorkflowRuns(ctx, id); err != nil {
				return apierr.Internal("cancel active runs")
			}
		}
		// Deleting the workflow cascades its versions + runs (the version_id
		// NO ACTION FK is checked at end-of-statement, so the cascade is
		// internally consistent).
		return qtx.DeleteWorkflow(ctx, id)
	})
	if err != nil {
		return nil, err
	}
	return &emptypb.Empty{}, nil
}

func (s *WorkflowsServer) ForkWorkflow(ctx context.Context, req *workflowsv1.ForkWorkflowRequest) (*workflowsv1.Workflow, error) {
	workflowID := req.GetWorkflowId()
	if workflowID == "" {
		return nil, apierr.InvalidArgument(apierr.FieldViolation("workflow_id", "is required"))
	}
	srcID, err := parseWorkflowName(req.GetName())
	if err != nil {
		return nil, err
	}
	// scope(ctx) is resolved from the request's `parent` (the fork's
	// destination) via the permission interceptor's scope_field. The source
	// must live in that same scope — checkWorkflowScope enforces it.
	orgID, spaceID, prefix := scope(ctx)
	callerID := convert.PgUUID(server.MustUserID(ctx))

	newID, err := uuid.NewV7()
	if err != nil {
		return nil, apierr.Internal("generate workflow id")
	}
	verID, err := uuid.NewV7()
	if err != nil {
		return nil, apierr.Internal("generate version id")
	}

	row, err := db.RunInTxValidate(ctx, s.pool, req.GetValidateOnly(), func(qtx db.Querier) (db.Workflow, error) {
		src, err := qtx.GetWorkflow(ctx, srcID)
		if err != nil {
			return db.Workflow{}, apierr.HandleResourceError(err, "Workflow", req.GetName())
		}
		if err := checkWorkflowScope(src, orgID, spaceID, req.GetName()); err != nil {
			return db.Workflow{}, err
		}

		srcVer, err := resolveForkSource(ctx, qtx, src, req.GetSourceVersion())
		if err != nil {
			return db.Workflow{}, err
		}

		// New OWNED workflow under the destination parent, carrying the
		// source's config; left version-less (a DRAFT) — the user promotes
		// when ready.
		newWf, err := qtx.CreateWorkflow(ctx, db.CreateWorkflowParams{
			ID:          newID,
			OrgID:       orgID,
			SpaceID:     spaceID,
			WorkflowID:  workflowID,
			DisplayName: req.GetDisplayName(),
			Description: "",
			Enabled:     false,
			Config:      src.Config,
			Origin:      originOwned,
			Annotations: json.RawMessage("{}"),
			CreatedBy:   callerID,
		})
		if err != nil {
			return db.Workflow{}, apierr.HandleResourceError(err, "Workflow", workflowID)
		}
		// Clone the source version's definition into the fork's version 1.
		if _, err := qtx.CreateWorkflowVersion(ctx, db.CreateWorkflowVersionParams{
			ID:            verID,
			WorkflowID:    newWf.ID,
			VersionNumber: 1,
			Note:          "",
			Definition:    srcVer.Definition,
			CreatedBy:     callerID,
		}); err != nil {
			return db.Workflow{}, apierr.HandleResourceError(err, "WorkflowVersion", workflowID)
		}
		return newWf, nil
	})
	if err != nil {
		return nil, err
	}
	// The fork has no promoted version, so no version-number lookup is needed.
	return s.toProto(ctx, row, prefix), nil
}

// resolveForkSource loads the version a fork clones from: the explicitly
// requested source_version (which must belong to the source workflow) or, when
// none is given, the source's promoted live version.
func resolveForkSource(ctx context.Context, qtx db.Querier, src db.Workflow, sourceVersion string) (db.WorkflowVersion, error) {
	if sourceVersion != "" {
		svWfID, svNum, err := parseWorkflowVersionName(sourceVersion)
		if err != nil {
			return db.WorkflowVersion{}, err
		}
		if svWfID != src.ID {
			return db.WorkflowVersion{}, apierr.InvalidArgument(apierr.FieldViolation(
				"source_version", "must be a version of the source workflow"))
		}
		ver, err := qtx.GetWorkflowVersionByNumber(ctx, db.GetWorkflowVersionByNumberParams{
			WorkflowID:    svWfID,
			VersionNumber: svNum,
		})
		if err != nil {
			return db.WorkflowVersion{}, apierr.HandleResourceError(err, "WorkflowVersion", sourceVersion)
		}
		return ver, nil
	}
	if !src.Version.Valid {
		return db.WorkflowVersion{}, apierr.FailedPrecondition(
			"source workflow has no promoted version; specify source_version")
	}
	ver, err := qtx.GetWorkflowVersion(ctx, src.Version.Bytes)
	if err != nil {
		// GetWorkflow above is a plain read, so the source's promoted version
		// can be deleted out from under this fallback between the two reads —
		// a stale pointer, not an internal fault.
		if errors.Is(err, pgx.ErrNoRows) {
			return db.WorkflowVersion{}, apierr.FailedPrecondition(
				"source workflow's promoted version is unavailable; specify source_version")
		}
		return db.WorkflowVersion{}, apierr.Internal("load source version")
	}
	return ver, nil
}

func (s *WorkflowsServer) PromoteWorkflowVersion(ctx context.Context, req *workflowsv1.PromoteWorkflowVersionRequest) (*workflowsv1.Workflow, error) {
	wfID, err := parseWorkflowName(req.GetName())
	if err != nil {
		return nil, err
	}
	verWfID, verNum, err := parseWorkflowVersionName(req.GetVersion())
	if err != nil {
		return nil, err
	}
	orgID, spaceID, prefix := scope(ctx)
	callerID := convert.PgUUID(server.MustUserID(ctx))

	row, err := db.RunInTxValidate(ctx, s.pool, req.GetValidateOnly(), func(qtx db.Querier) (db.Workflow, error) {
		wf, err := qtx.GetWorkflowForUpdate(ctx, wfID)
		if err != nil {
			return db.Workflow{}, apierr.HandleResourceError(err, "Workflow", req.GetName())
		}
		if err := checkWorkflowScope(wf, orgID, spaceID, req.GetName()); err != nil {
			return db.Workflow{}, err
		}
		// Resolve the named version's uuid. Then SetWorkflowVersion's join
		// (v.workflow_id = w.id) is the authoritative belonging check: a
		// version of another workflow yields no row → FailedPrecondition.
		ver, err := qtx.GetWorkflowVersionByNumber(ctx, db.GetWorkflowVersionByNumberParams{
			WorkflowID:    verWfID,
			VersionNumber: verNum,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return db.Workflow{}, apierr.FailedPrecondition("version does not belong to this workflow")
			}
			return db.Workflow{}, apierr.Internal("load version")
		}
		updated, err := qtx.SetWorkflowVersion(ctx, db.SetWorkflowVersionParams{
			ID:        wfID,
			UpdatedBy: callerID,
			Version:   ver.ID,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return db.Workflow{}, apierr.FailedPrecondition("version does not belong to this workflow")
			}
			return db.Workflow{}, apierr.HandleResourceError(err, "Workflow", req.GetName())
		}
		return updated, nil
	})
	if err != nil {
		return nil, err
	}
	// The promoted version's number is known (parsed from the request), so
	// render the pointer without a lookup.
	versionNumbers := map[uuid.UUID]int64{}
	if row.Version.Valid {
		versionNumbers[row.Version.Bytes] = verNum
	}
	actors := resolveActors(ctx, s.audit, workflowActorIDs([]db.Workflow{row}))
	return convert.WorkflowToProto(row, prefix, actors, versionNumbers), nil
}

// toProto renders a single workflow, resolving its actors and (if promoted)
// its live version's number.
func (s *WorkflowsServer) toProto(ctx context.Context, row db.Workflow, prefix string) *workflowsv1.Workflow {
	rows := []db.Workflow{row}
	actors := resolveActors(ctx, s.audit, workflowActorIDs(rows))
	versionNumbers := resolveVersionNumbers(ctx, s.queries, rows)
	return convert.WorkflowToProto(row, prefix, actors, versionNumbers)
}

// ============================================================================
// WorkflowVersionsServer
// ============================================================================

func (s *WorkflowVersionsServer) ListWorkflowVersions(ctx context.Context, req *workflowsv1.ListWorkflowVersionsRequest) (*workflowsv1.ListWorkflowVersionsResponse, error) {
	wfID, err := parseWorkflowName(req.GetParent())
	if err != nil {
		return nil, err
	}
	orgID, spaceID, prefix := scope(ctx)
	if err := s.checkParentWorkflow(ctx, wfID, orgID, spaceID, req.GetParent()); err != nil {
		return nil, err
	}
	pageSize := clampPageSize(req.GetPageSize())

	cursor, err := decodePageToken(s.codec, req.GetPageToken())
	if err != nil {
		return nil, err
	}

	rows, err := s.queries.ListWorkflowVersions(ctx, db.ListWorkflowVersionsParams{
		WorkflowID: wfID,
		Cursor:     cursor,
		PageLimit:  pageSize + 1,
	})
	if err != nil {
		return nil, apierr.Internal("list workflow versions")
	}

	var nextPageToken string
	if int32(len(rows)) > pageSize {
		nextPageToken, err = filter.EncodeNextPageToken(s.codec, rows[pageSize].ID)
		if err != nil {
			return nil, apierr.Internal("encode page token")
		}
		rows = rows[:pageSize]
	}

	workflowName := prefix + "/workflows/" + wfID.String()
	actors := resolveActors(ctx, s.audit, versionActorIDs(rows))
	out := make([]*workflowsv1.WorkflowVersion, 0, len(rows))
	for _, r := range rows {
		out = append(out, convert.WorkflowVersionToProto(r, workflowName, actors))
	}
	return &workflowsv1.ListWorkflowVersionsResponse{WorkflowVersions: out, NextPageToken: nextPageToken}, nil
}

func (s *WorkflowVersionsServer) GetWorkflowVersion(ctx context.Context, req *workflowsv1.GetWorkflowVersionRequest) (*workflowsv1.WorkflowVersion, error) {
	wfID, verNum, err := parseWorkflowVersionName(req.GetName())
	if err != nil {
		return nil, err
	}
	orgID, spaceID, prefix := scope(ctx)
	if err := s.checkParentWorkflow(ctx, wfID, orgID, spaceID, req.GetName()); err != nil {
		return nil, err
	}
	ver, err := s.queries.GetWorkflowVersionByNumber(ctx, db.GetWorkflowVersionByNumberParams{
		WorkflowID:    wfID,
		VersionNumber: verNum,
	})
	if err != nil {
		return nil, apierr.HandleResourceError(err, "WorkflowVersion", req.GetName())
	}
	workflowName := prefix + "/workflows/" + wfID.String()
	actors := resolveActors(ctx, s.audit, versionActorIDs([]db.WorkflowVersion{ver}))
	return convert.WorkflowVersionToProto(ver, workflowName, actors), nil
}

func (s *WorkflowVersionsServer) CreateWorkflowVersion(ctx context.Context, req *workflowsv1.CreateWorkflowVersionRequest) (*workflowsv1.WorkflowVersion, error) {
	wfID, err := parseWorkflowName(req.GetParent())
	if err != nil {
		return nil, err
	}
	in := req.GetWorkflowVersion()
	if in.GetRoot() == nil {
		return nil, apierr.InvalidArgument(apierr.FieldViolation("workflow_version.root", "is required"))
	}
	orgID, spaceID, prefix := scope(ctx)

	definition, err := marshalDefinition(in)
	if err != nil {
		return nil, apierr.InvalidArgument(apierr.FieldViolation("workflow_version", err.Error()))
	}
	verID, err := uuid.NewV7()
	if err != nil {
		return nil, apierr.Internal("generate version id")
	}
	callerID := convert.PgUUID(server.MustUserID(ctx))

	ver, err := db.RunInTxValidate(ctx, s.pool, req.GetValidateOnly(), func(qtx db.Querier) (db.WorkflowVersion, error) {
		// Lock the parent so concurrent version creates serialize — the
		// NextWorkflowVersionNumber contract (its MAX+1 must not race).
		wf, err := qtx.GetWorkflowForUpdate(ctx, wfID)
		if err != nil {
			return db.WorkflowVersion{}, apierr.HandleResourceError(err, "Workflow", req.GetParent())
		}
		if err := checkWorkflowScope(wf, orgID, spaceID, req.GetParent()); err != nil {
			return db.WorkflowVersion{}, err
		}
		next, err := qtx.NextWorkflowVersionNumber(ctx, wfID)
		if err != nil {
			return db.WorkflowVersion{}, apierr.Internal("allocate version number")
		}
		row, err := qtx.CreateWorkflowVersion(ctx, db.CreateWorkflowVersionParams{
			ID:            verID,
			WorkflowID:    wfID,
			VersionNumber: int64(next),
			Note:          in.GetNote(),
			Definition:    definition,
			CreatedBy:     callerID,
		})
		if err != nil {
			// The UNIQUE(workflow_id, version_number) backstop is unreachable
			// under the parent lock, but the point of a backstop is the
			// unforeseen — surface it as a typed error, not codes.Unknown.
			return db.WorkflowVersion{}, apierr.HandleResourceError(err, "WorkflowVersion", req.GetParent())
		}
		return row, nil
	})
	if err != nil {
		return nil, err
	}
	workflowName := prefix + "/workflows/" + wfID.String()
	actors := resolveActors(ctx, s.audit, versionActorIDs([]db.WorkflowVersion{ver}))
	return convert.WorkflowVersionToProto(ver, workflowName, actors), nil
}

func (s *WorkflowVersionsServer) DeleteWorkflowVersion(ctx context.Context, req *workflowsv1.DeleteWorkflowVersionRequest) (*emptypb.Empty, error) {
	wfID, verNum, err := parseWorkflowVersionName(req.GetName())
	if err != nil {
		return nil, err
	}
	orgID, spaceID, _ := scope(ctx)
	err = db.RunInTxVoidValidate(ctx, s.pool, req.GetValidateOnly(), func(qtx db.Querier) error {
		// Lock the parent so the promoted-pointer check serializes against a
		// concurrent promote.
		wf, err := qtx.GetWorkflowForUpdate(ctx, wfID)
		if err != nil {
			return apierr.HandleResourceError(err, "Workflow", req.GetName())
		}
		if err := checkWorkflowScope(wf, orgID, spaceID, req.GetName()); err != nil {
			return err
		}
		ver, err := qtx.GetWorkflowVersionByNumber(ctx, db.GetWorkflowVersionByNumberParams{
			WorkflowID:    wfID,
			VersionNumber: verNum,
		})
		if err != nil {
			return apierr.HandleResourceError(err, "WorkflowVersion", req.GetName())
		}
		// Refuse to delete the promoted version. The FK is ON DELETE SET NULL,
		// so the DB wouldn't block it — this app-level guard keeps a live
		// workflow from silently losing its definition pointer.
		if wf.Version.Valid && wf.Version.Bytes == ver.ID {
			return apierr.FailedPrecondition("version is the workflow's promoted version; promote another first")
		}
		if err := qtx.DeleteWorkflowVersion(ctx, ver.ID); err != nil {
			// workflow_runs.version_id is NO ACTION: a version with runs can't
			// be deleted. Surface that as FailedPrecondition rather than a raw
			// DB error (HandleResourceError would mis-map 23503 to NotFound).
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == apierr.PgForeignKeyViolation {
				return apierr.FailedPrecondition("version has runs; it cannot be deleted")
			}
			return apierr.HandleResourceError(err, "WorkflowVersion", req.GetName())
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &emptypb.Empty{}, nil
}

// checkParentWorkflow fetches the parent workflow by uuid and verifies it
// belongs to the caller's scope. A missing or out-of-scope parent is NotFound
// on the Workflow (attributed to the version resource name the caller used).
func (s *WorkflowVersionsServer) checkParentWorkflow(ctx context.Context, wfID uuid.UUID, orgID uuid.UUID, spaceID pgtype.UUID, name string) error {
	wf, err := s.queries.GetWorkflow(ctx, wfID)
	if err != nil {
		return apierr.HandleResourceError(err, "Workflow", name)
	}
	return checkWorkflowScope(wf, orgID, spaceID, name)
}

// versionActorIDs collects the created_by ids across a page of version rows
// (versions are immutable — no updated_by).
func versionActorIDs(rows []db.WorkflowVersion) []uuid.UUID {
	ids := make([]uuid.UUID, 0, len(rows))
	for _, r := range rows {
		if r.CreatedBy.Valid {
			ids = append(ids, r.CreatedBy.Bytes)
		}
	}
	return ids
}
