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
	"time"

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

// parseWorkflowName extracts the slug leaf from a full Workflow resource name
// ("organizations/{org}[/spaces/{space}]/workflows/{slug}"). The slug is the
// user-assigned id, unique within the workflow's parent scope; the handler
// resolves it to the internal uuid via a scoped (org + space + slug) lookup, so
// a name that names another org's workflow simply finds no row in this scope
// (NotFound, no cross-scope leak). A name with no leaf is NotFound.
func parseWorkflowName(name string) (string, error) {
	idx := strings.LastIndex(name, "/")
	if idx < 0 || idx == len(name)-1 {
		return "", apierr.NotFound("Workflow", name)
	}
	return name[idx+1:], nil
}

// parseWorkflowVersionName splits a full WorkflowVersion resource name
// ("organizations/{org}[/spaces/{space}]/workflows/{wf-slug}/versions/{n}")
// into the parent workflow's slug and the monotonic version number. Any
// malformation is NotFound.
func parseWorkflowVersionName(name string) (workflowSlug string, versionNumber int64, err error) {
	const marker = "/versions/"
	idx := strings.LastIndex(name, marker)
	if idx < 0 {
		return "", 0, apierr.NotFound("WorkflowVersion", name)
	}
	num, perr := strconv.ParseInt(name[idx+len(marker):], 10, 64)
	if perr != nil || num <= 0 {
		return "", 0, apierr.NotFound("WorkflowVersion", name)
	}
	// The segment before "/versions/" is the parent Workflow name; its leaf
	// is the workflow slug.
	wfName := name[:idx]
	li := strings.LastIndex(wfName, "/")
	if li < 0 || li == len(wfName)-1 {
		return "", 0, apierr.NotFound("WorkflowVersion", name)
	}
	return wfName[li+1:], num, nil
}

// getWorkflowByParent resolves a Workflow by its parent scope + slug (the
// resource-name leaf). A missing row is NotFound attributed to name — the
// scoped lookup is itself the cross-scope guard (another org's workflow isn't
// in this scope). Callers use the returned row's ID for child-table FKs.
func getWorkflowByParent(ctx context.Context, q db.Querier, orgID uuid.UUID, spaceID pgtype.UUID, slug, name string) (db.Workflow, error) {
	wf, err := q.GetWorkflowByParent(ctx, db.GetWorkflowByParentParams{OrgID: orgID, SpaceID: spaceID, Slug: slug})
	if err != nil {
		return db.Workflow{}, apierr.HandleResourceError(err, "Workflow", name)
	}
	return wf, nil
}

// lockWorkflowByParent resolves AND row-locks a Workflow by parent scope + slug,
// for update/promote/delete/version-create txs (the etag check and the write
// serialize against a concurrent mutation).
func lockWorkflowByParent(ctx context.Context, q db.Querier, orgID uuid.UUID, spaceID pgtype.UUID, slug, name string) (db.Workflow, error) {
	wf, err := q.GetWorkflowByParentForUpdate(ctx, db.GetWorkflowByParentForUpdateParams{OrgID: orgID, SpaceID: spaceID, Slug: slug})
	if err != nil {
		return db.Workflow{}, apierr.HandleResourceError(err, "Workflow", name)
	}
	return wf, nil
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
// root + error_sequence) as JSONB via a scratch WorkflowVersion carrying only
// those fields — symmetric with convert.WorkflowVersionToProto's read path.
func marshalDefinition(in *workflowsv1.WorkflowVersion) (json.RawMessage, error) {
	scratch := &workflowsv1.WorkflowVersion{
		Parameters:    in.GetParameters(),
		Trigger:       in.GetTrigger(),
		Root:          in.GetRoot(),
		ErrorSequence: in.GetErrorSequence(),
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

// ListWorkflows is a dynamic AIP-160 filtered + AIP-132 sorted + compound-cursor
// keyset list, structurally identical to ListConnectors/ListSecrets. The
// interceptor-resolved (org, space) is the NON-NEGOTIABLE base scope, ANDed as
// the base of the query; the request's filter/order_by layer ON TOP of it and
// can only narrow, never widen. Every value (scope ids, filter operands, cursor
// values, page size) is bound as a $N parameter by filter.BuildListQuery —
// nothing is string-interpolated — and column/direction come only from
// WorkflowFilter's whitelist.
//
// The base scope uses `space_id IS NOT DISTINCT FROM $` (NULL-matchable),
// preserving the pre-migration ListWorkflowsByParent semantics: an org-level
// parent lists only the org-direct workflows (space_id NULL), a space-level
// parent lists that space's — this is deliberately NOT the connectors-style
// org-level rollup. See docs/aip-list-transpiler-procedure.md.
func (s *WorkflowsServer) ListWorkflows(ctx context.Context, req *workflowsv1.ListWorkflowsRequest) (*workflowsv1.ListWorkflowsResponse, error) {
	orgID, spaceID, prefix := scope(ctx)
	rf := filter.WorkflowFilter()
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

	// Base scope: the interceptor-resolved org (always) + the workflow's org+space
	// leveling. `space_id IS NOT DISTINCT FROM $` treats NULL (org-scoped) as a
	// matchable value, so it selects exactly the parent's level — org-direct or
	// one space — never a rollup. The AIP filter layers on top and can only narrow.
	base := []filter.Predicate{
		{SQL: "org_id = %s", Arg: orgID},
		{SQL: "space_id IS NOT DISTINCT FROM %s", Arg: spaceID},
	}
	sql, args, err := filter.BuildListQuery(filter.ListQuery{
		Resource: rf,
		Base:     base,
		Filter:   req.GetFilter(),
		Order:    plan,
		PageSize: pageSize,
		Cursor:   cursor,
	})
	if err != nil {
		// The only error source is the filter transpiler (bad user filter) —
		// surface it as InvalidArgument on "filter".
		return nil, apierr.InvalidArgument(apierr.FieldViolation("filter", err.Error()))
	}

	pgxRows, err := s.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, apierr.Internal(err, "list workflows")
	}
	rows, err := filter.ScanWorkflows(pgxRows)
	if err != nil {
		return nil, apierr.Internal(err, "list workflows")
	}

	// filter.Paginate trims the over-fetched result to pageSize and derives the
	// next-page token from the LAST RETURNED row (see the connectors comment for
	// the off-by-one this closes).
	rows, nextPageToken, err := filter.Paginate(rows, int(pageSize), func(last db.Workflow) (string, error) {
		return filter.EncodeCursor(s.codec, plan, workflowSortValue(plan, last), last.ID)
	})
	if err != nil {
		return nil, apierr.Internal(err, "encode page token")
	}

	actors := resolveActors(ctx, s.audit, workflowActorIDs(rows))
	versionNumbers := resolveVersionNumbers(ctx, s.queries, rows)
	out := make([]*workflowsv1.Workflow, 0, len(rows))
	for _, r := range rows {
		out = append(out, convert.WorkflowToProto(r, prefix, actors, versionNumbers))
	}
	return &workflowsv1.ListWorkflowsResponse{Workflows: out, NextPageToken: nextPageToken}, nil
}

// workflowSortValue renders the primary order_by column's value for the given
// row as the string the compound page token carries. Timestamps use RFC3339Nano
// so filter.DecodeCursor can parse them back to an exact time.Time. For the
// default id ordering (plan.Field == "") the value is unused (EncodeCursor emits
// the id-only token), so "" is returned. Mirrors connectorSortValue.
func workflowSortValue(plan filter.OrderByPlan, r db.Workflow) string {
	switch plan.Field {
	case "displayName":
		return r.DisplayName
	case "createTime":
		return r.CreateTime.UTC().Format(time.RFC3339Nano)
	case "updateTime":
		return r.UpdateTime.UTC().Format(time.RFC3339Nano)
	default:
		return ""
	}
}

func (s *WorkflowsServer) GetWorkflow(ctx context.Context, req *workflowsv1.GetWorkflowRequest) (*workflowsv1.Workflow, error) {
	slug, err := parseWorkflowName(req.GetName())
	if err != nil {
		return nil, err
	}
	orgID, spaceID, prefix := scope(ctx)
	row, err := getWorkflowByParent(ctx, s.queries, orgID, spaceID, slug, req.GetName())
	if err != nil {
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
		return nil, apierr.Internal(err, "generate workflow id")
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
		Slug:        workflowID,
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
	slug, err := parseWorkflowName(in.GetName())
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

	// ID is filled in-tx from the scope-resolved row (the name leaf is the slug).
	params := db.UpdateWorkflowParams{
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
		existing, err := lockWorkflowByParent(ctx, qtx, orgID, spaceID, slug, in.GetName())
		if err != nil {
			return err
		}
		if etag := in.GetEtag(); etag != "" && etag != existing.Etag {
			return apierr.Aborted("Workflow", in.GetName(), "etag mismatch")
		}
		params.ID = existing.ID
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
	slug, err := parseWorkflowName(req.GetName())
	if err != nil {
		return nil, err
	}
	orgID, spaceID, _ := scope(ctx)
	err = db.RunInTxVoidValidate(ctx, s.pool, req.GetValidateOnly(), func(qtx db.Querier) error {
		existing, err := lockWorkflowByParent(ctx, qtx, orgID, spaceID, slug, req.GetName())
		if err != nil {
			return err
		}
		if etag := req.GetEtag(); etag != "" && etag != existing.Etag {
			return apierr.Aborted("Workflow", req.GetName(), "etag mismatch")
		}
		active, err := qtx.CountActiveWorkflowRuns(ctx, existing.ID)
		if err != nil {
			return apierr.Internal(err, "check active runs")
		}
		if active > 0 {
			if !req.GetForce() {
				return apierr.FailedPrecondition("workflow has active runs; cancel them or use force")
			}
			// force: cancel the active runs (DB state only) before the delete
			// cascades them away.
			// Phase 6: also stop the River job backing each cancelled run.
			if err := qtx.CancelActiveWorkflowRuns(ctx, existing.ID); err != nil {
				return apierr.Internal(err, "cancel active runs")
			}
		}
		// Deleting the workflow cascades its versions + runs (the version_id
		// NO ACTION FK is checked at end-of-statement, so the cascade is
		// internally consistent).
		return qtx.DeleteWorkflow(ctx, existing.ID)
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
	srcSlug, err := parseWorkflowName(req.GetName())
	if err != nil {
		return nil, err
	}
	// scope(ctx) is resolved from the request's `parent` (the fork's
	// destination) via the permission interceptor's scope_field. The source
	// must live in that same scope — the scoped by-slug lookup enforces it (a
	// source in another scope isn't found).
	orgID, spaceID, prefix := scope(ctx)
	callerID := convert.PgUUID(server.MustUserID(ctx))

	newID, err := uuid.NewV7()
	if err != nil {
		return nil, apierr.Internal(err, "generate workflow id")
	}
	verID, err := uuid.NewV7()
	if err != nil {
		return nil, apierr.Internal(err, "generate version id")
	}

	row, err := db.RunInTxValidate(ctx, s.pool, req.GetValidateOnly(), func(qtx db.Querier) (db.Workflow, error) {
		src, err := getWorkflowByParent(ctx, qtx, orgID, spaceID, srcSlug, req.GetName())
		if err != nil {
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
			Slug:        workflowID,
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
		svWfSlug, svNum, err := parseWorkflowVersionName(sourceVersion)
		if err != nil {
			return db.WorkflowVersion{}, err
		}
		if svWfSlug != src.Slug {
			return db.WorkflowVersion{}, apierr.InvalidArgument(apierr.FieldViolation(
				"source_version", "must be a version of the source workflow"))
		}
		ver, err := qtx.GetWorkflowVersionByNumber(ctx, db.GetWorkflowVersionByNumberParams{
			WorkflowID:    src.ID,
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
		return db.WorkflowVersion{}, apierr.Internal(err, "load source version")
	}
	return ver, nil
}

func (s *WorkflowsServer) PromoteWorkflowVersion(ctx context.Context, req *workflowsv1.PromoteWorkflowVersionRequest) (*workflowsv1.Workflow, error) {
	wfSlug, err := parseWorkflowName(req.GetName())
	if err != nil {
		return nil, err
	}
	verWfSlug, verNum, err := parseWorkflowVersionName(req.GetVersion())
	if err != nil {
		return nil, err
	}
	orgID, spaceID, prefix := scope(ctx)
	callerID := convert.PgUUID(server.MustUserID(ctx))

	row, err := db.RunInTxValidate(ctx, s.pool, req.GetValidateOnly(), func(qtx db.Querier) (db.Workflow, error) {
		wf, err := lockWorkflowByParent(ctx, qtx, orgID, spaceID, wfSlug, req.GetName())
		if err != nil {
			return db.Workflow{}, err
		}
		// The version name must name this same workflow; a version of another
		// workflow can't be promoted here.
		if verWfSlug != wfSlug {
			return db.Workflow{}, apierr.FailedPrecondition("version does not belong to this workflow")
		}
		// Resolve the named version's uuid under this workflow. SetWorkflowVersion's
		// join (v.workflow_id = w.id) is the authoritative belonging backstop.
		ver, err := qtx.GetWorkflowVersionByNumber(ctx, db.GetWorkflowVersionByNumberParams{
			WorkflowID:    wf.ID,
			VersionNumber: verNum,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return db.Workflow{}, apierr.FailedPrecondition("version does not belong to this workflow")
			}
			return db.Workflow{}, apierr.Internal(err, "load version")
		}
		updated, err := qtx.SetWorkflowVersion(ctx, db.SetWorkflowVersionParams{
			ID:        wf.ID,
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
	wfSlug, err := parseWorkflowName(req.GetParent())
	if err != nil {
		return nil, err
	}
	orgID, spaceID, prefix := scope(ctx)
	wf, err := getWorkflowByParent(ctx, s.queries, orgID, spaceID, wfSlug, req.GetParent())
	if err != nil {
		return nil, err
	}
	pageSize := clampPageSize(req.GetPageSize())

	cursor, err := decodePageToken(s.codec, req.GetPageToken())
	if err != nil {
		return nil, err
	}

	rows, err := s.queries.ListWorkflowVersions(ctx, db.ListWorkflowVersionsParams{
		WorkflowID: wf.ID,
		Cursor:     cursor,
		PageLimit:  pageSize + 1,
	})
	if err != nil {
		return nil, apierr.Internal(err, "list workflow versions")
	}

	rows, nextPageToken, err := filter.Paginate(rows, int(pageSize), func(last db.WorkflowVersion) (string, error) {
		return filter.EncodeNextPageToken(s.codec, last.ID)
	})
	if err != nil {
		return nil, apierr.Internal(err, "encode page token")
	}

	workflowName := prefix + "/workflows/" + wfSlug
	actors := resolveActors(ctx, s.audit, versionActorIDs(rows))
	out := make([]*workflowsv1.WorkflowVersion, 0, len(rows))
	for _, r := range rows {
		out = append(out, convert.WorkflowVersionToProto(r, workflowName, actors))
	}
	return &workflowsv1.ListWorkflowVersionsResponse{WorkflowVersions: out, NextPageToken: nextPageToken}, nil
}

func (s *WorkflowVersionsServer) GetWorkflowVersion(ctx context.Context, req *workflowsv1.GetWorkflowVersionRequest) (*workflowsv1.WorkflowVersion, error) {
	wfSlug, verNum, err := parseWorkflowVersionName(req.GetName())
	if err != nil {
		return nil, err
	}
	orgID, spaceID, prefix := scope(ctx)
	wf, err := getWorkflowByParent(ctx, s.queries, orgID, spaceID, wfSlug, req.GetName())
	if err != nil {
		return nil, err
	}
	ver, err := s.queries.GetWorkflowVersionByNumber(ctx, db.GetWorkflowVersionByNumberParams{
		WorkflowID:    wf.ID,
		VersionNumber: verNum,
	})
	if err != nil {
		return nil, apierr.HandleResourceError(err, "WorkflowVersion", req.GetName())
	}
	workflowName := prefix + "/workflows/" + wfSlug
	actors := resolveActors(ctx, s.audit, versionActorIDs([]db.WorkflowVersion{ver}))
	return convert.WorkflowVersionToProto(ver, workflowName, actors), nil
}

func (s *WorkflowVersionsServer) CreateWorkflowVersion(ctx context.Context, req *workflowsv1.CreateWorkflowVersionRequest) (*workflowsv1.WorkflowVersion, error) {
	wfSlug, err := parseWorkflowName(req.GetParent())
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
		return nil, apierr.Internal(err, "generate version id")
	}
	callerID := convert.PgUUID(server.MustUserID(ctx))

	ver, err := db.RunInTxValidate(ctx, s.pool, req.GetValidateOnly(), func(qtx db.Querier) (db.WorkflowVersion, error) {
		// Lock the parent so concurrent version creates serialize — the
		// NextWorkflowVersionNumber contract (its MAX+1 must not race).
		wf, err := lockWorkflowByParent(ctx, qtx, orgID, spaceID, wfSlug, req.GetParent())
		if err != nil {
			return db.WorkflowVersion{}, err
		}
		next, err := qtx.NextWorkflowVersionNumber(ctx, wf.ID)
		if err != nil {
			return db.WorkflowVersion{}, apierr.Internal(err, "allocate version number")
		}
		row, err := qtx.CreateWorkflowVersion(ctx, db.CreateWorkflowVersionParams{
			ID:            verID,
			WorkflowID:    wf.ID,
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
	workflowName := prefix + "/workflows/" + wfSlug
	actors := resolveActors(ctx, s.audit, versionActorIDs([]db.WorkflowVersion{ver}))
	return convert.WorkflowVersionToProto(ver, workflowName, actors), nil
}

func (s *WorkflowVersionsServer) DeleteWorkflowVersion(ctx context.Context, req *workflowsv1.DeleteWorkflowVersionRequest) (*emptypb.Empty, error) {
	wfSlug, verNum, err := parseWorkflowVersionName(req.GetName())
	if err != nil {
		return nil, err
	}
	orgID, spaceID, _ := scope(ctx)
	err = db.RunInTxVoidValidate(ctx, s.pool, req.GetValidateOnly(), func(qtx db.Querier) error {
		// Lock the parent so the promoted-pointer check serializes against a
		// concurrent promote.
		wf, err := lockWorkflowByParent(ctx, qtx, orgID, spaceID, wfSlug, req.GetName())
		if err != nil {
			return err
		}
		ver, err := qtx.GetWorkflowVersionByNumber(ctx, db.GetWorkflowVersionByNumberParams{
			WorkflowID:    wf.ID,
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
