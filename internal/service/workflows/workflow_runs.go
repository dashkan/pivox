package workflows

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
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
// ("organizations/{org}[/spaces/{space}]/workflows/{wf-uuid}/runs/{run-uuid}")
// into the parent workflow's uuid and the run's uuid. Any malformation is
// NotFound. Mirrors parseWorkflowVersionName's two-segment parse.
func parseWorkflowRunName(name string) (workflowID, runID uuid.UUID, err error) {
	const marker = "/runs/"
	idx := strings.LastIndex(name, marker)
	if idx < 0 {
		return uuid.Nil, uuid.Nil, apierr.NotFound("WorkflowRun", name)
	}
	rID, perr := uuid.Parse(name[idx+len(marker):])
	if perr != nil {
		return uuid.Nil, uuid.Nil, apierr.NotFound("WorkflowRun", name)
	}
	// The segment before "/runs/" is the parent Workflow name; its leaf is the
	// workflow uuid.
	wfName := name[:idx]
	li := strings.LastIndex(wfName, "/")
	if li < 0 {
		return uuid.Nil, uuid.Nil, apierr.NotFound("WorkflowRun", name)
	}
	wfID, perr := uuid.Parse(wfName[li+1:])
	if perr != nil {
		return uuid.Nil, uuid.Nil, apierr.NotFound("WorkflowRun", name)
	}
	return wfID, rID, nil
}

func (s *WorkflowRunsServer) RunWorkflow(ctx context.Context, req *workflowsv1.RunWorkflowRequest) (*workflowsv1.WorkflowRun, error) {
	wfID, err := parseWorkflowName(req.GetName())
	if err != nil {
		return nil, err
	}
	orgID, spaceID, prefix := scope(ctx)
	callerID := convert.PgUUID(server.MustUserID(ctx))

	runID, err := uuid.NewV7()
	if err != nil {
		return nil, apierr.Internal("generate run id")
	}
	// Manual runs carry a MANUAL trigger. Marshalled to JSONB symmetric with
	// the WorkflowRunToProto read path.
	trigger, err := protojson.Marshal(&workflowsv1.RunTrigger{Kind: workflowsv1.RunTriggerKind_MANUAL})
	if err != nil {
		return nil, apierr.Internal("marshal trigger")
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
		wf, err := qtx.GetWorkflow(ctx, wfID)
		if err != nil {
			return db.WorkflowRun{}, apierr.HandleResourceError(err, "Workflow", req.GetName())
		}
		if err := checkWorkflowScope(wf, orgID, spaceID, req.GetName()); err != nil {
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
			WorkflowID:  wfID,
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
			return db.WorkflowRun{}, apierr.Internal("enqueue workflow run")
		}
		return created, nil
	})
	if err != nil {
		return nil, err
	}
	// The pinned version's number is known from the resolution above, so render
	// the pointer without a lookup.
	workflowName := prefix + "/workflows/" + wfID.String()
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
		verWfID, verNum, err := parseWorkflowVersionName(reqVersion)
		if err != nil {
			return db.WorkflowVersion{}, err
		}
		if verWfID != wf.ID {
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
			return db.WorkflowVersion{}, apierr.Internal("load version")
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
		return db.WorkflowVersion{}, apierr.Internal("load promoted version")
	}
	return ver, nil
}

func (s *WorkflowRunsServer) GetWorkflowRun(ctx context.Context, req *workflowsv1.GetWorkflowRunRequest) (*workflowsv1.WorkflowRun, error) {
	wfID, runID, err := parseWorkflowRunName(req.GetName())
	if err != nil {
		return nil, err
	}
	orgID, spaceID, prefix := scope(ctx)
	if err := s.checkParentWorkflow(ctx, wfID, orgID, spaceID, req.GetName()); err != nil {
		return nil, err
	}
	run, err := s.queries.GetWorkflowRun(ctx, runID)
	if err != nil {
		return nil, apierr.HandleResourceError(err, "WorkflowRun", req.GetName())
	}
	// IDOR: a run uuid can't be read through another workflow's name. The
	// parent workflow already passed the scope check, so a run whose
	// workflow_id differs is NotFound (don't leak existence).
	if run.WorkflowID != wfID {
		return nil, apierr.NotFound("WorkflowRun", req.GetName())
	}
	return s.toProto(ctx, run, prefix+"/workflows/"+wfID.String()), nil
}

func (s *WorkflowRunsServer) ListWorkflowRuns(ctx context.Context, req *workflowsv1.ListWorkflowRunsRequest) (*workflowsv1.ListWorkflowRunsResponse, error) {
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

	// filter/order_by are accepted on the request but ignored here (as in the
	// sibling Workflows/WorkflowVersions services) — runs are returned in id
	// (creation) order only.
	rows, err := s.queries.ListWorkflowRuns(ctx, db.ListWorkflowRunsParams{
		WorkflowID: wfID,
		Cursor:     cursor,
		PageLimit:  pageSize + 1,
	})
	if err != nil {
		return nil, apierr.Internal("list workflow runs")
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
	versionNumbers := resolveRunVersionNumbers(ctx, s.queries, rows)
	actors := resolveActors(ctx, s.audit, runActorIDs(rows))
	out := make([]*workflowsv1.WorkflowRun, 0, len(rows))
	for _, r := range rows {
		out = append(out, convert.WorkflowRunToProto(r, workflowName, actors, versionNumbers))
	}
	return &workflowsv1.ListWorkflowRunsResponse{WorkflowRuns: out, NextPageToken: nextPageToken}, nil
}

func (s *WorkflowRunsServer) CancelWorkflowRun(ctx context.Context, req *workflowsv1.CancelWorkflowRunRequest) (*workflowsv1.WorkflowRun, error) {
	wfID, runID, err := parseWorkflowRunName(req.GetName())
	if err != nil {
		return nil, err
	}
	orgID, spaceID, prefix := scope(ctx)

	// CancelWorkflowRunRequest carries no validate_only field (the proto omits
	// it), so this mutation always commits.
	run, err := db.RunInTx(ctx, s.pool, func(qtx db.Querier) (db.WorkflowRun, error) {
		// Scope-check the parent inside the tx so the check and the state write
		// serialize against a concurrent parent mutation.
		wf, err := qtx.GetWorkflow(ctx, wfID)
		if err != nil {
			return db.WorkflowRun{}, apierr.HandleResourceError(err, "Workflow", req.GetName())
		}
		if err := checkWorkflowScope(wf, orgID, spaceID, req.GetName()); err != nil {
			return db.WorkflowRun{}, err
		}
		existing, err := qtx.GetWorkflowRunForUpdate(ctx, runID)
		if err != nil {
			return db.WorkflowRun{}, apierr.HandleResourceError(err, "WorkflowRun", req.GetName())
		}
		// IDOR: the run must belong to the named (scope-checked) workflow.
		if existing.WorkflowID != wfID {
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
	return s.toProto(ctx, run, prefix+"/workflows/"+wfID.String()), nil
}

// checkParentWorkflow fetches the parent workflow by uuid and verifies it
// belongs to the caller's scope. A missing or out-of-scope parent is NotFound
// (attributed to the run/parent resource name the caller used).
func (s *WorkflowRunsServer) checkParentWorkflow(ctx context.Context, wfID, orgID uuid.UUID, spaceID pgtype.UUID, name string) error {
	wf, err := s.queries.GetWorkflow(ctx, wfID)
	if err != nil {
		return apierr.HandleResourceError(err, "Workflow", name)
	}
	return checkWorkflowScope(wf, orgID, spaceID, name)
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
