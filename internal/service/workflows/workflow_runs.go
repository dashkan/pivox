package workflows

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"go.einride.tech/aip/filtering"
	expr "google.golang.org/genproto/googleapis/api/expr/v1alpha1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/structpb"
	"riverqueue.com/riverpro"

	"github.com/dashkan/pivox/internal/apierr"
	"github.com/dashkan/pivox/internal/appkey"
	"github.com/dashkan/pivox/internal/audit"
	"github.com/dashkan/pivox/internal/convert"
	db "github.com/dashkan/pivox/internal/db/generated"
	"github.com/dashkan/pivox/internal/engine/runjob"
	"github.com/dashkan/pivox/internal/filter"
	workflowsv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/workflows/v1"
	"github.com/dashkan/pivox/internal/server"
)

// WorkflowRunsServer serves the WorkflowRuns RPCs (the execution surface).
// Runs are children of a Workflow, uuid-named. This layer only manages run
// records + DB lifecycle state; the execution engine that actually advances a
// run lands in Phase 6.
type WorkflowRunsServer struct {
	workflowsv1.UnimplementedWorkflowRunsServer
	pool    db.RWPool
	queries db.Querier
	codec   *appkey.Codec
	audit   *audit.Resolver
	river   *riverpro.Client[pgx.Tx]
}

// NewWorkflowRunsServer constructs the server from cfg. Panics on a missing
// required field — including River, which this server (unlike the container /
// version servers) needs to enqueue a run's execution job.
func NewWorkflowRunsServer(cfg Config) *WorkflowRunsServer {
	cfg.validate("workflows")
	if cfg.River == nil {
		panic("workflows: Config.River is required for WorkflowRunsServer")
	}
	return &WorkflowRunsServer{
		pool:    cfg.Pool,
		queries: cfg.Queries,
		codec:   cfg.Codec,
		audit:   cfg.AuditResolver,
		river:   cfg.River,
	}
}

// parseWorkflowRunName splits a full WorkflowRun resource name
// ("organizations/{org}[/spaces/{space}]/workflows/{wf-slug}/runs/{run-uuid}")
// into the parent workflow's slug and the run's uuid (runs are system-created,
// so the run leaf stays a uuid). Any malformation is NotFound. Mirrors
// parseWorkflowVersionName's two-segment parse.
func parseWorkflowRunName(name string) (workflowSlug string, runID uuid.UUID, err error) {
	const marker = "/runs/"
	idx := strings.LastIndex(name, marker)
	if idx < 0 {
		return "", uuid.Nil, apierr.NotFound("WorkflowRun", name)
	}
	rID, perr := uuid.Parse(name[idx+len(marker):])
	if perr != nil {
		return "", uuid.Nil, apierr.NotFound("WorkflowRun", name)
	}
	// The segment before "/runs/" is the parent Workflow name; its leaf is the
	// workflow slug.
	wfName := name[:idx]
	li := strings.LastIndex(wfName, "/")
	if li < 0 || li == len(wfName)-1 {
		return "", uuid.Nil, apierr.NotFound("WorkflowRun", name)
	}
	return wfName[li+1:], rID, nil
}

func (s *WorkflowRunsServer) RunWorkflow(ctx context.Context, req *workflowsv1.RunWorkflowRequest) (*workflowsv1.WorkflowRun, error) {
	wfSlug, err := parseWorkflowName(req.GetName())
	if err != nil {
		return nil, err
	}
	orgID, spaceID, prefix := scope(ctx)
	callerID := convert.PgUUID(server.MustUserID(ctx))

	runID, err := uuid.NewV7()
	if err != nil {
		return nil, apierr.Internal(err, "generate run id")
	}
	// Manual runs carry a MANUAL trigger. Marshalled to JSONB symmetric with
	// the WorkflowRunToProto read path.
	trigger, err := protojson.Marshal(&workflowsv1.RunTrigger{Kind: workflowsv1.RunTriggerKind_MANUAL})
	if err != nil {
		return nil, apierr.Internal(err, "marshal trigger")
	}
	input, err := marshalRunInput(req.GetParameters())
	if err != nil {
		return nil, apierr.InvalidArgument(apierr.FieldViolation("parameters", err.Error()))
	}

	var (
		run    db.WorkflowRun
		verNum int64
	)
	run, err = db.RunInTxRawValidate(ctx, s.pool, req.GetValidateOnly(), func(qtx db.Querier, tx pgx.Tx) (db.WorkflowRun, error) {
		wf, err := getWorkflowByParent(ctx, qtx, orgID, spaceID, wfSlug, req.GetName())
		if err != nil {
			return db.WorkflowRun{}, err
		}
		// Manual runs are allowed regardless of `enabled` — enabled gates
		// automatic triggers, not an explicit RunWorkflow call.
		ver, err := resolveRunVersion(ctx, qtx, wf, req.GetVersion())
		if err != nil {
			return db.WorkflowRun{}, err
		}
		verNum = ver.VersionNumber
		created, err := qtx.CreateWorkflowRun(ctx, db.CreateWorkflowRunParams{
			ID:          runID,
			WorkflowID:  wf.ID,
			OrgID:       wf.OrgID,
			SpaceID:     wf.SpaceID,
			VersionID:   ver.ID,
			State:       runjob.StatePending,
			Trigger:     trigger,
			Subject:     req.GetSubject(),
			Steps:       json.RawMessage("[]"),
			Input:       input,
			TriggeredBy: callerID,
		})
		if err != nil {
			return db.WorkflowRun{}, err
		}
		// Enqueue the execution job for the Worker Process in the SAME tx as the
		// run's INSERT. On validate_only (or any rollback) the enqueue rolls back
		// with the run row — no orphaned job pointing at a run that never
		// persisted, and no persisted run that never gets picked up.
		if _, err := s.river.InsertTx(ctx, tx, runjob.Args{RunID: runID}, nil); err != nil {
			return db.WorkflowRun{}, apierr.Internal(err, "enqueue workflow run")
		}
		return created, nil
	})
	if err != nil {
		return nil, err
	}
	// The pinned version's number is known from the resolution above, so render
	// the pointer without a lookup.
	workflowName := prefix + "/workflows/" + wfSlug
	versionNumbers := map[uuid.UUID]int64{run.VersionID: verNum}
	actors := resolveActors(ctx, s.audit, runActorIDs([]db.WorkflowRun{run}))
	return convert.WorkflowRunToProto(run, workflowName, actors, versionNumbers), nil
}

// resolveRunVersion picks the WorkflowVersion a run pins: the explicitly
// requested version (which must belong to this workflow) or, when none is
// given, the workflow's promoted live version. A version of another workflow,
// or the absence of a promoted version, is FailedPrecondition.
func resolveRunVersion(ctx context.Context, qtx db.Querier, wf db.Workflow, reqVersion string) (db.WorkflowVersion, error) {
	if reqVersion != "" {
		verWfSlug, verNum, err := parseWorkflowVersionName(reqVersion)
		if err != nil {
			return db.WorkflowVersion{}, err
		}
		if verWfSlug != wf.Slug {
			return db.WorkflowVersion{}, apierr.FailedPrecondition("version does not belong to this workflow")
		}
		ver, err := qtx.GetWorkflowVersionByNumber(ctx, db.GetWorkflowVersionByNumberParams{
			WorkflowID:    wf.ID,
			VersionNumber: verNum,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return db.WorkflowVersion{}, apierr.FailedPrecondition("version does not belong to this workflow")
			}
			return db.WorkflowVersion{}, apierr.Internal(err, "load version")
		}
		return ver, nil
	}
	if !wf.Version.Valid {
		return db.WorkflowVersion{}, apierr.FailedPrecondition("workflow has no promoted version")
	}
	ver, err := qtx.GetWorkflowVersion(ctx, wf.Version.Bytes)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return db.WorkflowVersion{}, apierr.FailedPrecondition("workflow's promoted version is unavailable")
		}
		return db.WorkflowVersion{}, apierr.Internal(err, "load promoted version")
	}
	return ver, nil
}

func (s *WorkflowRunsServer) GetWorkflowRun(ctx context.Context, req *workflowsv1.GetWorkflowRunRequest) (*workflowsv1.WorkflowRun, error) {
	wfSlug, runID, err := parseWorkflowRunName(req.GetName())
	if err != nil {
		return nil, err
	}
	orgID, spaceID, prefix := scope(ctx)
	wf, err := getWorkflowByParent(ctx, s.queries, orgID, spaceID, wfSlug, req.GetName())
	if err != nil {
		return nil, err
	}
	run, err := s.queries.GetWorkflowRun(ctx, runID)
	if err != nil {
		return nil, apierr.HandleResourceError(err, "WorkflowRun", req.GetName())
	}
	// IDOR: a run uuid can't be read through another workflow's name. The
	// parent workflow already passed the scope check, so a run whose
	// workflow_id differs is NotFound (don't leak existence).
	if run.WorkflowID != wf.ID {
		return nil, apierr.NotFound("WorkflowRun", req.GetName())
	}
	return s.toProto(ctx, run, prefix+"/workflows/"+wfSlug), nil
}

func (s *WorkflowRunsServer) ListWorkflowRuns(ctx context.Context, req *workflowsv1.ListWorkflowRunsRequest) (*workflowsv1.ListWorkflowRunsResponse, error) {
	orgID, spaceID, prefix := scope(ctx)
	pageSize := clampPageSize(req.GetPageSize())
	cursor, err := decodePageToken(s.codec, req.GetPageToken())
	if err != nil {
		return nil, err
	}
	// order_by is accepted but ignored (runs are always id/creation order, the
	// keyset column); the `state` filter is honored on both the per-workflow and
	// the scope-wide paths.
	stateFilter, err := parseRunStateFilter(req.GetFilter())
	if err != nil {
		return nil, err
	}

	// AIP-159 `-` wildcard: organizations/{org}[/spaces/{space}]/workflows/-
	// lists runs across the whole scope instead of one workflow. The permission
	// interceptor already gated workflows.read at that org/space scope (via the
	// parent's scope prefix), so there is no single parent workflow to re-check.
	if isWorkflowWildcard(req.GetParent()) {
		// Space-scope wildcard: one space, so `prefix` (which already carries
		// /spaces/{space}) + each run's own workflow_id builds the name.
		if spaceID.Valid {
			rows, err := s.queries.ListWorkflowRunsBySpace(ctx, db.ListWorkflowRunsBySpaceParams{
				SpaceID:   spaceID,
				State:     stateFilter,
				Cursor:    cursor,
				PageLimit: pageSize + 1,
			})
			if err != nil {
				return nil, apierr.Internal(err, "list workflow runs")
			}
			// One space, but potentially many workflows: each run's name carries
			// its own workflow's slug, resolved (over the returned page only) in
			// one batched lookup over the page's distinct workflows (no N+1).
			return s.renderRunPage(ctx, rows, pageSize, func(ctx context.Context, page []db.WorkflowRun) ([]string, error) {
				wfSlugs, err := s.resolveWorkflowSlugs(ctx, page)
				if err != nil {
					return nil, err
				}
				names := make([]string, len(page))
				for i, r := range page {
					slug, ok := wfSlugs[r.WorkflowID]
					if !ok {
						return nil, apierr.Internal(
							fmt.Errorf("workflow %s for run %s has no row", r.WorkflowID, r.ID),
							"resolve workflow slug")
					}
					names[i] = prefix + "/workflows/" + slug
				}
				return names, nil
			})
		}

		// Org-scope wildcard: ALL runs in the org, including runs of space-scoped
		// workflows. Each run's name must reflect its actual location, so a
		// space-scoped run needs its space slug — resolved (over the returned page
		// only) in one batched lookup over the page's distinct spaces (no N+1).
		rows, err := s.queries.ListWorkflowRunsByOrg(ctx, db.ListWorkflowRunsByOrgParams{
			OrgID:     orgID,
			State:     stateFilter,
			Cursor:    cursor,
			PageLimit: pageSize + 1,
		})
		if err != nil {
			return nil, apierr.Internal(err, "list workflow runs")
		}
		return s.renderRunPage(ctx, rows, pageSize, func(ctx context.Context, page []db.WorkflowRun) ([]string, error) {
			return s.orgRunNames(ctx, page, prefix)
		})
	}

	wfSlug, err := parseWorkflowName(req.GetParent())
	if err != nil {
		return nil, err
	}
	wf, err := getWorkflowByParent(ctx, s.queries, orgID, spaceID, wfSlug, req.GetParent())
	if err != nil {
		return nil, err
	}
	rows, err := s.queries.ListWorkflowRuns(ctx, db.ListWorkflowRunsParams{
		WorkflowID: wf.ID,
		State:      stateFilter,
		Cursor:     cursor,
		PageLimit:  pageSize + 1,
	})
	if err != nil {
		return nil, apierr.Internal(err, "list workflow runs")
	}
	workflowName := prefix + "/workflows/" + wfSlug
	return s.renderRunPage(ctx, rows, pageSize, func(_ context.Context, page []db.WorkflowRun) ([]string, error) {
		names := make([]string, len(page))
		for i := range page {
			names[i] = workflowName
		}
		return names, nil
	})
}

// isWorkflowWildcard reports whether a ListWorkflowRuns parent uses the AIP-159
// `-` collection wildcard in the workflow segment
// (organizations/{org}[/spaces/{space}]/workflows/-).
func isWorkflowWildcard(parent string) bool {
	return strings.HasSuffix(parent, "/workflows/-")
}

// orgRunNames builds the resource name for each run in an org-scope wildcard
// page (index-aligned to page). Each run's name needs its workflow's slug (the
// workflow segment) and, for a space-scoped run, its space's slug — both
// resolved (over the returned page only) in one batched lookup each over the
// page's distinct workflows and spaces (no N+1). An org-direct run (space_id
// NULL) nests directly under the org; a space-scoped run nests under its space.
// orgPrefix is "organizations/{org}". A run referencing a workflow or space
// with no row is a data-integrity fault (the FK cascades, so it cannot happen
// normally) and surfaces as Internal rather than a malformed name.
func (s *WorkflowRunsServer) orgRunNames(ctx context.Context, page []db.WorkflowRun, orgPrefix string) ([]string, error) {
	wfSlugs, err := s.resolveWorkflowSlugs(ctx, page)
	if err != nil {
		return nil, err
	}

	var spaceIDs []uuid.UUID
	seen := make(map[uuid.UUID]struct{})
	for _, r := range page {
		if r.SpaceID.Valid {
			id := uuid.UUID(r.SpaceID.Bytes)
			if _, ok := seen[id]; !ok {
				seen[id] = struct{}{}
				spaceIDs = append(spaceIDs, id)
			}
		}
	}
	spaceSlugs := make(map[uuid.UUID]string, len(spaceIDs))
	if len(spaceIDs) > 0 {
		got, err := s.queries.SpaceSlugsByIDs(ctx, spaceIDs)
		if err != nil {
			return nil, apierr.Internal(err, "resolve space slugs")
		}
		for _, sr := range got {
			spaceSlugs[sr.ID] = sr.Name
		}
	}
	names := make([]string, len(page))
	for i, r := range page {
		wfSlug, ok := wfSlugs[r.WorkflowID]
		if !ok {
			return nil, apierr.Internal(
				fmt.Errorf("workflow %s for run %s has no row", r.WorkflowID, r.ID),
				"resolve workflow slug")
		}
		if !r.SpaceID.Valid {
			names[i] = orgPrefix + "/workflows/" + wfSlug
			continue
		}
		spaceSlug, ok := spaceSlugs[uuid.UUID(r.SpaceID.Bytes)]
		if !ok {
			return nil, apierr.Internal(
				fmt.Errorf("space %s for run %s has no row", uuid.UUID(r.SpaceID.Bytes), r.ID),
				"resolve space slug")
		}
		names[i] = orgPrefix + "/spaces/" + spaceSlug + "/workflows/" + wfSlug
	}
	return names, nil
}

// resolveWorkflowSlugs maps the page's distinct workflow ids to their slug (the
// workflow segment of a run's resource name) in one batched lookup. The
// scope-wide run listings return workflow_runs rows carrying only the
// workflow_id uuid FK, so the slug is resolved here rather than joined per row.
func (s *WorkflowRunsServer) resolveWorkflowSlugs(ctx context.Context, page []db.WorkflowRun) (map[uuid.UUID]string, error) {
	var ids []uuid.UUID
	seen := make(map[uuid.UUID]struct{})
	for _, r := range page {
		if _, ok := seen[r.WorkflowID]; !ok {
			seen[r.WorkflowID] = struct{}{}
			ids = append(ids, r.WorkflowID)
		}
	}
	slugs := make(map[uuid.UUID]string, len(ids))
	if len(ids) == 0 {
		return slugs, nil
	}
	got, err := s.queries.WorkflowSlugsByIDs(ctx, ids)
	if err != nil {
		return nil, apierr.Internal(err, "resolve workflow slugs")
	}
	for _, wr := range got {
		slugs[wr.ID] = wr.Slug
	}
	return slugs, nil
}

// renderRunPage trims the keyset over-fetch into a page + next-page token,
// resolves the page's version numbers and actors, and renders each run to
// proto. buildNames runs AFTER the trim over the returned page only, yielding
// one resource name per run (index-aligned) — so a dropped over-fetch row can
// never trigger a lookup or an error that poisons the response.
func (s *WorkflowRunsServer) renderRunPage(
	ctx context.Context,
	rows []db.WorkflowRun,
	pageSize int32,
	buildNames func(ctx context.Context, page []db.WorkflowRun) ([]string, error),
) (*workflowsv1.ListWorkflowRunsResponse, error) {
	var nextPageToken string
	if int32(len(rows)) > pageSize {
		// The keyset cursor is the LAST returned row's id; the next page's query
		// resumes with `id > cursor`. (Encoding the first *un*returned row here
		// would skip it, since the resume predicate is strict `>`.)
		tok, err := filter.EncodeNextPageToken(s.codec, rows[pageSize-1].ID)
		if err != nil {
			return nil, apierr.Internal(err, "encode page token")
		}
		nextPageToken = tok
		rows = rows[:pageSize]
	}
	names, err := buildNames(ctx, rows)
	if err != nil {
		return nil, err
	}
	versionNumbers := resolveRunVersionNumbers(ctx, s.queries, rows)
	actors := resolveActors(ctx, s.audit, runActorIDs(rows))
	out := make([]*workflowsv1.WorkflowRun, 0, len(rows))
	for i, r := range rows {
		out = append(out, convert.WorkflowRunToProto(r, names[i], actors, versionNumbers))
	}
	return &workflowsv1.ListWorkflowRunsResponse{WorkflowRuns: out, NextPageToken: nextPageToken}, nil
}

// parseRunStateFilter parses the ListWorkflowRuns AIP-160 filter. The only
// supported shape is `state = "VALUE"` (matching the proto's documented
// example); an empty filter yields an unset (match-all) column value. Any other
// shape, or an unknown run state, is InvalidArgument rather than a silently
// ignored or empty result.
func parseRunStateFilter(filterExpr string) (pgtype.Text, error) {
	if strings.TrimSpace(filterExpr) == "" {
		return pgtype.Text{}, nil
	}
	unsupported := apierr.InvalidArgument(apierr.FieldViolation("filter",
		`only 'state = "VALUE"' is supported`))

	var parser filtering.Parser
	parser.Init(filterExpr)
	parsed, err := parser.Parse()
	if err != nil {
		return pgtype.Text{}, apierr.InvalidArgument(apierr.FieldViolation("filter", "invalid filter expression"))
	}
	call := parsed.GetExpr().GetCallExpr()
	if call == nil || call.GetFunction() != filtering.FunctionEquals || len(call.GetArgs()) != 2 {
		return pgtype.Text{}, unsupported
	}
	if call.GetArgs()[0].GetIdentExpr().GetName() != "state" {
		return pgtype.Text{}, unsupported
	}
	state, ok := filterStringValue(call.GetArgs()[1])
	if !ok {
		return pgtype.Text{}, unsupported
	}
	if !runjob.IsValidState(state) {
		return pgtype.Text{}, apierr.InvalidArgument(apierr.FieldViolation("filter",
			"unknown run state "+strconv.Quote(state)))
	}
	return pgtype.Text{String: state, Valid: true}, nil
}

// filterStringValue extracts a string from an AIP-160 value expression — either
// a quoted string constant (`state = "RUNNING"`) or a bare identifier
// (`state = RUNNING`), both of which the einride parser accepts.
func filterStringValue(e *expr.Expr) (string, bool) {
	switch v := e.GetExprKind().(type) {
	case *expr.Expr_ConstExpr:
		if s, ok := v.ConstExpr.GetConstantKind().(*expr.Constant_StringValue); ok {
			return s.StringValue, true
		}
	case *expr.Expr_IdentExpr:
		return v.IdentExpr.GetName(), true
	}
	return "", false
}

func (s *WorkflowRunsServer) CancelWorkflowRun(ctx context.Context, req *workflowsv1.CancelWorkflowRunRequest) (*workflowsv1.WorkflowRun, error) {
	wfSlug, runID, err := parseWorkflowRunName(req.GetName())
	if err != nil {
		return nil, err
	}
	orgID, spaceID, prefix := scope(ctx)

	// CancelWorkflowRunRequest carries no validate_only field (the proto omits
	// it), so this mutation always commits.
	run, err := db.RunInTx(ctx, s.pool, func(qtx db.Querier) (db.WorkflowRun, error) {
		// Resolve the parent inside the tx so the scope resolution and the state
		// write serialize against a concurrent parent mutation.
		wf, err := getWorkflowByParent(ctx, qtx, orgID, spaceID, wfSlug, req.GetName())
		if err != nil {
			return db.WorkflowRun{}, err
		}
		existing, err := qtx.GetWorkflowRunForUpdate(ctx, runID)
		if err != nil {
			return db.WorkflowRun{}, apierr.HandleResourceError(err, "WorkflowRun", req.GetName())
		}
		// IDOR: the run must belong to the named (scope-checked) workflow.
		if existing.WorkflowID != wf.ID {
			return db.WorkflowRun{}, apierr.NotFound("WorkflowRun", req.GetName())
		}
		if runjob.IsTerminalState(existing.State) {
			return db.WorkflowRun{}, apierr.FailedPrecondition("run is not active")
		}
		// Phase 6: also stop the River job backing the run.
		updated, err := qtx.UpdateWorkflowRunState(ctx, db.UpdateWorkflowRunStateParams{
			ID:      runID,
			State:   runjob.StateCancelled,
			EndTime: pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true},
		})
		if err != nil {
			return db.WorkflowRun{}, apierr.HandleResourceError(err, "WorkflowRun", req.GetName())
		}
		return updated, nil
	})
	if err != nil {
		return nil, err
	}
	return s.toProto(ctx, run, prefix+"/workflows/"+wfSlug), nil
}

// toProto renders a single run, resolving its pinned version's number and its
// triggered_by actor.
func (s *WorkflowRunsServer) toProto(ctx context.Context, run db.WorkflowRun, workflowName string) *workflowsv1.WorkflowRun {
	rows := []db.WorkflowRun{run}
	versionNumbers := resolveRunVersionNumbers(ctx, s.queries, rows)
	actors := resolveActors(ctx, s.audit, runActorIDs(rows))
	return convert.WorkflowRunToProto(run, workflowName, actors, versionNumbers)
}

// marshalRunInput renders a run's initial input (a google.protobuf.Struct) as
// JSONB. A nil/empty input maps to a nil []byte → SQL NULL (the input column
// is nullable), symmetric with WorkflowRunToProto's read path.
func marshalRunInput(in *structpb.Struct) ([]byte, error) {
	if in == nil || len(in.GetFields()) == 0 {
		return nil, nil
	}
	return protojson.Marshal(in)
}

// resolveRunVersionNumbers maps each page row's pinned version_id to its
// monotonic version_number, so WorkflowRunToProto can render the numbered
// version pointer without an N+1 per-row lookup.
func resolveRunVersionNumbers(ctx context.Context, queries db.Querier, rows []db.WorkflowRun) map[uuid.UUID]int64 {
	if len(rows) == 0 {
		return nil
	}
	ids := make([]uuid.UUID, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.VersionID)
	}
	got, err := queries.WorkflowVersionNumbersByIDs(ctx, ids)
	if err != nil {
		slog.WarnContext(ctx, "workflows: run version-number resolution failed; version pointers omitted", "error", err)
		return nil
	}
	m := make(map[uuid.UUID]int64, len(got))
	for _, v := range got {
		m[v.ID] = v.VersionNumber
	}
	return m
}

// runActorIDs collects the triggered_by ids across a page of run rows (NULL for
// system triggers — Phase 5 always sets it to the caller).
func runActorIDs(rows []db.WorkflowRun) []uuid.UUID {
	ids := make([]uuid.UUID, 0, len(rows))
	for _, r := range rows {
		if r.TriggeredBy.Valid {
			ids = append(ids, r.TriggeredBy.Bytes)
		}
	}
	return ids
}
