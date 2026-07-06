package workers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	statuspb "google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/structpb"

	db "github.com/dashkan/pivox/internal/db/generated"
	"github.com/dashkan/pivox/internal/engine"
	"github.com/dashkan/pivox/internal/engine/runjob"
	workflowsv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/workflows/v1"
)

// RunWorkflowWorker executes a PENDING WorkflowRun via the pure interpreter in
// internal/engine. It is the Worker Process half of the Phase 6b wiring: the
// Cloud Controller enqueues a runjob.Args in RunWorkflow, this worker loads the
// run + its pinned version, transitions it to RUNNING, walks the definition,
// and finalizes the run's terminal state.
//
// Retry semantics are the crux. A run that fails with an [engine.IsRetryable]
// error hands the whole job back to River (the run stays RUNNING; a later
// attempt re-executes). A terminal failure marks the run FAILED and returns nil
// so River does NOT retry. A finished run (SUCCEEDED/FAILED/CANCELLED) is a
// no-op — a re-delivered job never re-executes or double-finalizes.
type RunWorkflowWorker struct {
	river.WorkerDefaults[runjob.Args]

	// Pool is the database pool. Required.
	Pool *pgxpool.Pool
	// Interpreter is the shared pure interpreter (evaluator + dispatcher).
	// Required. Constructed once in cmd/pivox-worker; safe for concurrent use
	// across jobs.
	Interpreter *engine.Interpreter
	// Logger is the structured logger. Required.
	Logger *slog.Logger
}

var _ river.Worker[runjob.Args] = (*RunWorkflowWorker)(nil)

// Work implements river.Worker[runjob.Args].
func (w *RunWorkflowWorker) Work(ctx context.Context, job *river.Job[runjob.Args]) error {
	runID := job.Args.RunID
	log := w.Logger.With("run_id", runID, "job_id", job.ID)

	// Lock the run, apply the terminal-run guard, and transition to RUNNING —
	// all under one lock so a concurrent cancel can't slip between the guard and
	// the transition.
	run, proceed, err := w.begin(ctx, runID)
	if err != nil {
		// Couldn't read/transition the run (infra fault). Return to River so the
		// job retries — nothing was executed.
		log.ErrorContext(ctx, "workflow run: begin failed", "error", err)
		return err
	}
	if !proceed {
		// Already terminal (idempotent replay of a finished/cancelled job) or the
		// run row is gone. Nothing to execute.
		log.InfoContext(ctx, "workflow run: nothing to execute", "state", run.State)
		return nil
	}

	log.InfoContext(ctx, "workflow run: running",
		"workflow_id", run.WorkflowID, "version_id", run.VersionID)

	root, err := w.loadDefinition(ctx, run.VersionID)
	if err != nil {
		var td *terminalDefinitionError
		if errors.As(err, &td) {
			// A corrupt/absent pinned definition never heals on retry — terminal.
			return w.finalizeFailed(ctx, runID, nil, err, log)
		}
		// Transient (DB fault, ctx cancellation): hand the job back to River. The
		// run stays RUNNING; do NOT mark it FAILED. Without this split a blip
		// while loading the definition would permanently fail the run — worse,
		// finalize runs on a detached context, so even a shutdown-time
		// cancellation would succeed in writing FAILED.
		log.WarnContext(ctx, "workflow run: load definition failed transiently; returning job to River", "error", err)
		return err
	}
	// A malformed trigger/input JSONB is our own persisted data — it won't heal
	// on retry, so a decode failure is terminal.
	rc, err := buildRunContext(run)
	if err != nil {
		return w.finalizeFailed(ctx, runID, nil, err, log)
	}

	reporter := newRunReporter(w.Pool, runID, log)
	result, runErr := w.Interpreter.Run(ctx, root, rc, reporter)

	// Retryable infra fault: hand the whole job back to River. The run stays
	// RUNNING; a later attempt re-executes from the top (begin sees RUNNING, not
	// terminal). Explicitly NOT marked FAILED.
	if runErr != nil && engine.IsRetryable(runErr) {
		log.WarnContext(ctx, "workflow run: retryable failure; returning job to River", "error", runErr)
		return runErr
	}

	stepsJSON := reporter.snapshotJSON()

	switch result.Status {
	case engine.RunStatusCompleted:
		return w.finalizeSucceeded(ctx, runID, result.Output, stepsJSON, log)
	case engine.RunStatusFailed:
		return w.finalizeFailed(ctx, runID, stepsJSON, runErr, log)
	case engine.RunStatusCancelled:
		// The interpreter observed ctx cancellation. Cooperative (DB-state)
		// cancellation does not cancel this ctx — nothing bridges CancelWorkflowRun
		// to the worker ctx today — so this only fires on worker shutdown or a
		// deadline. Treat it as an interruption: leave the run RUNNING and return
		// the error so River re-leases + retries. (A business cancel is handled by
		// finalize's respect-CANCELLED re-check, not here.)
		log.WarnContext(ctx, "workflow run: interrupted by context cancellation; will retry", "error", runErr)
		return runErr
	default:
		// Unreachable: Interpreter.Run only returns the three statuses above.
		return fmt.Errorf("workflow run %s: unexpected status %d", runID, result.Status)
	}
}

// beginResult carries the locked run plus whether the worker should execute it.
type beginResult struct {
	run     db.WorkflowRun
	proceed bool
}

// begin locks the run row, applies the terminal-run guard, and transitions a
// non-terminal run to RUNNING. proceed is false when the run is already
// terminal (idempotent no-op) or absent. start_time is stamped only on the
// first PENDING→RUNNING transition, so a re-delivered RUNNING job preserves the
// original start.
func (w *RunWorkflowWorker) begin(ctx context.Context, runID uuid.UUID) (db.WorkflowRun, bool, error) {
	res, err := db.RunInTx(ctx, w.Pool, func(qtx db.Querier) (beginResult, error) {
		run, err := qtx.GetWorkflowRunForUpdate(ctx, runID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return beginResult{}, nil // run is gone; proceed=false
			}
			return beginResult{}, err
		}
		if runjob.IsTerminalState(run.State) {
			return beginResult{run: run}, nil // proceed=false
		}
		var startTime pgtype.Timestamptz
		if run.State == runjob.StatePending {
			startTime = pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true}
		}
		updated, err := qtx.UpdateWorkflowRunState(ctx, db.UpdateWorkflowRunStateParams{
			ID:        runID,
			State:     runjob.StateRunning,
			StartTime: startTime,
		})
		if err != nil {
			return beginResult{}, err
		}
		return beginResult{run: updated, proceed: true}, nil
	})
	return res.run, res.proceed, err
}

// terminalDefinitionError marks a definition-load failure that will never heal
// on retry: a corrupt/unparseable definition, a version missing its root, or an
// absent pinned version. DB and context errors are deliberately NOT wrapped in
// it — those are transient and are handed back to River for retry.
type terminalDefinitionError struct{ cause error }

func (e *terminalDefinitionError) Error() string { return e.cause.Error() }
func (e *terminalDefinitionError) Unwrap() error { return e.cause }

// loadDefinition loads the run's pinned version and lifts its root Sequence out
// of the definition JSONB — symmetric with the workflows service's
// marshalDefinition write path. It distinguishes terminal errors (wrapped in
// [terminalDefinitionError]) from transient DB/ctx faults (returned bare) so the
// caller can retry the latter instead of failing the run.
func (w *RunWorkflowWorker) loadDefinition(ctx context.Context, versionID uuid.UUID) (*workflowsv1.Sequence, error) {
	ver, err := db.New(w.Pool).GetWorkflowVersion(ctx, versionID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// version_id is a NO ACTION FK while runs exist, so this is
			// essentially unreachable — but a missing version won't reappear, so
			// treat it as terminal rather than looping retries forever.
			return nil, &terminalDefinitionError{fmt.Errorf("pinned version %s not found", versionID)}
		}
		return nil, fmt.Errorf("load pinned version %s: %w", versionID, err)
	}
	var scratch workflowsv1.WorkflowVersion
	if err := protojson.Unmarshal(ver.Definition, &scratch); err != nil {
		return nil, &terminalDefinitionError{fmt.Errorf("unmarshal version %s definition: %w", versionID, err)}
	}
	root := scratch.GetRoot()
	if root == nil {
		return nil, &terminalDefinitionError{fmt.Errorf("version %s has no root sequence", versionID)}
	}
	return root, nil
}

// buildRunContext builds the interpreter's RunContext from the run's trigger and
// input JSONB. The trigger decodes to a map (e.g. {"kind":"MANUAL"}) readable as
// `trigger.<field>`; the input (a google.protobuf.Struct) becomes `params`.
func buildRunContext(run db.WorkflowRun) (*engine.RunContext, error) {
	trigger, err := jsonbToMap(run.Trigger)
	if err != nil {
		return nil, fmt.Errorf("decode run trigger: %w", err)
	}
	params, err := jsonbToMap(run.Input)
	if err != nil {
		return nil, fmt.Errorf("decode run input: %w", err)
	}
	return engine.NewRunContext(engine.RunContextConfig{
		Trigger: trigger,
		Params:  params,
	}), nil
}

// finalizeSucceeded writes SUCCEEDED with the run's output (a snapshot of vars)
// and final steps. Returns nil to River — a completed run is not retried.
func (w *RunWorkflowWorker) finalizeSucceeded(ctx context.Context, runID uuid.UUID, output map[string]any, steps []byte, log *slog.Logger) error {
	outJSON, err := marshalVars(output)
	if err != nil {
		// A non-serializable output shouldn't sink a successful run; record
		// SUCCEEDED without output rather than failing the run.
		log.WarnContext(ctx, "workflow run: output marshal failed; recording SUCCEEDED without output", "error", err)
		outJSON = nil
	}
	if err := w.finalize(ctx, runID, runjob.StateSucceeded, outJSON, steps, nil, log); err != nil {
		log.ErrorContext(ctx, "workflow run: finalize SUCCEEDED failed", "error", err)
		return err
	}
	log.InfoContext(ctx, "workflow run: succeeded")
	return nil
}

// finalizeFailed writes FAILED with the cause shaped as a google.rpc.Status and
// the steps accumulated so far. Returns nil to River — a terminal failure is
// not retried.
func (w *RunWorkflowWorker) finalizeFailed(ctx context.Context, runID uuid.UUID, steps []byte, cause error, log *slog.Logger) error {
	errJSON, mErr := marshalRunError(cause)
	if mErr != nil {
		log.WarnContext(ctx, "workflow run: error marshal failed; recording FAILED without detail", "error", mErr)
		errJSON = nil
	}
	if err := w.finalize(ctx, runID, runjob.StateFailed, nil, steps, errJSON, log); err != nil {
		log.ErrorContext(ctx, "workflow run: finalize FAILED failed", "error", err)
		return err
	}
	log.InfoContext(ctx, "workflow run: failed", "cause", cause)
	return nil
}

// finalize writes a terminal state — but only when the run is still
// non-terminal. It re-reads the row under a lock so a concurrent
// CancelWorkflowRun (state → CANCELLED) is never clobbered, and a
// double-delivered job that already finalized is a no-op. It uses a detached
// context so the write lands even when the job ctx is at/past its deadline.
func (w *RunWorkflowWorker) finalize(ctx context.Context, runID uuid.UUID, state string, output, steps, errJSON []byte, log *slog.Logger) error {
	finalCtx := context.WithoutCancel(ctx)
	return db.RunInTxVoid(finalCtx, w.Pool, func(qtx db.Querier) error {
		cur, err := qtx.GetWorkflowRunForUpdate(finalCtx, runID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil // gone; nothing to finalize
			}
			return err
		}
		if runjob.IsTerminalState(cur.State) {
			// Respect a business cancel (or a prior finalize): don't overwrite a
			// terminal state.
			log.InfoContext(finalCtx, "workflow run: already terminal at finalize; not overwriting",
				"state", cur.State, "wanted", state)
			return nil
		}
		_, err = qtx.UpdateWorkflowRunState(finalCtx, db.UpdateWorkflowRunStateParams{
			ID:      runID,
			State:   state,
			Output:  output,
			Steps:   steps,
			Error:   errJSON,
			EndTime: pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true},
		})
		return err
	})
}

// jsonbToMap decodes a JSONB object column to a Go map. Empty/NULL → nil map,
// which NewRunContext treats as empty.
func jsonbToMap(b []byte) (map[string]any, error) {
	if len(b) == 0 {
		return nil, nil
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	return m, nil
}

// marshalVars renders the run's output (a vars snapshot) as a
// google.protobuf.Struct JSONB, symmetric with convert.WorkflowRunToProto's
// read path. Empty → nil (SQL NULL).
func marshalVars(vars map[string]any) ([]byte, error) {
	if len(vars) == 0 {
		return nil, nil
	}
	s, err := structpb.NewStruct(vars)
	if err != nil {
		return nil, err
	}
	return protojson.Marshal(s)
}

// marshalRunError renders a run failure as a google.rpc.Status JSONB, symmetric
// with the read path (convert.WorkflowRunToProto unmarshals r.Error into a
// Status).
func marshalRunError(cause error) ([]byte, error) {
	return protojson.Marshal(runErrorStatus(cause))
}

// runErrorStatus shapes a terminal engine error as a google.rpc.Status. The
// engine's retry taxonomy is terminal-by-default, so a non-retryable failure is
// a bad definition or a rejected activity — FailedPrecondition. Finer-grained
// codes arrive with the richer activity error types in 6c/6d.
func runErrorStatus(cause error) *statuspb.Status {
	msg := "workflow run failed"
	if cause != nil {
		msg = cause.Error()
	}
	return &statuspb.Status{
		Code:    int32(codes.FailedPrecondition),
		Message: msg,
	}
}
