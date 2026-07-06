package workers

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	db "github.com/dashkan/pivox/internal/db/generated"
	"github.com/dashkan/pivox/internal/engine"
	workflowsv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/workflows/v1"
)

// runReporter is the DB-backed engine.StepReporter. It accumulates one proto
// StepState per step and checkpoints the whole set into workflow_runs.steps on
// every lifecycle event, so an observer can watch a run advance step by step.
//
// The checkpoint query (UpdateWorkflowRunSteps) is guarded on state='RUNNING',
// so a checkpoint that races a terminal finalize or a cancel is silently
// dropped by the DB — it can never resurrect a terminal run. Lost checkpoints
// are harmless: the terminal finalize writes the final steps snapshot.
//
// The interpreter reports from multiple goroutines (Parallel branches), so the
// accumulator maps are mutex-guarded. The map is snapshotted under the lock and
// the DB round-trip runs outside it — the mutex is never held across I/O.
type runReporter struct {
	pool   *pgxpool.Pool
	runID  uuid.UUID
	logger *slog.Logger

	mu    sync.Mutex
	order []string // step ids in first-seen order, for stable rendering
	byID  map[string]*workflowsv1.StepState
}

var _ engine.StepReporter = (*runReporter)(nil)

func newRunReporter(pool *pgxpool.Pool, runID uuid.UUID, logger *slog.Logger) *runReporter {
	return &runReporter{
		pool:   pool,
		runID:  runID,
		logger: logger,
		byID:   make(map[string]*workflowsv1.StepState),
	}
}

// StepStarted implements engine.StepReporter.
func (r *runReporter) StepStarted(ctx context.Context, stepID string, startedAt time.Time) {
	r.mu.Lock()
	ss := r.ensureLocked(stepID)
	ss.State = workflowsv1.State_RUNNING
	ss.StartTime = timestamppb.New(startedAt)
	payload, err := r.marshalLocked()
	r.mu.Unlock()
	r.checkpoint(ctx, payload, err)
}

// StepFinished implements engine.StepReporter.
func (r *runReporter) StepFinished(ctx context.Context, stepID string, output any, startedAt, finishedAt time.Time) {
	r.mu.Lock()
	ss := r.ensureLocked(stepID)
	ss.State = workflowsv1.State_SUCCEEDED
	ss.StartTime = timestamppb.New(startedAt)
	ss.EndTime = timestamppb.New(finishedAt)
	ss.Output = structFromOutput(output)
	payload, err := r.marshalLocked()
	r.mu.Unlock()
	r.checkpoint(ctx, payload, err)
}

// StepFailed implements engine.StepReporter.
func (r *runReporter) StepFailed(ctx context.Context, stepID string, cause error, startedAt, finishedAt time.Time) {
	r.mu.Lock()
	ss := r.ensureLocked(stepID)
	ss.State = workflowsv1.State_FAILED
	ss.StartTime = timestamppb.New(startedAt)
	ss.EndTime = timestamppb.New(finishedAt)
	ss.Error = runErrorStatus(cause)
	payload, err := r.marshalLocked()
	r.mu.Unlock()
	r.checkpoint(ctx, payload, err)
}

// snapshotJSON returns the current steps as the JSONB array persisted in
// workflow_runs.steps. Used by the terminal finalize so the terminal row
// carries the complete steps set even if the last live checkpoint was dropped
// by the RUNNING guard.
func (r *runReporter) snapshotJSON() []byte {
	r.mu.Lock()
	payload, err := r.marshalLocked()
	r.mu.Unlock()
	if err != nil {
		r.logger.Warn("workflow run: step snapshot marshal failed", "run_id", r.runID, "error", err)
		return nil
	}
	return payload
}

// ensureLocked returns the StepState for stepID, creating it (and recording its
// order) on first sight. Caller holds r.mu.
func (r *runReporter) ensureLocked(stepID string) *workflowsv1.StepState {
	ss, ok := r.byID[stepID]
	if !ok {
		ss = &workflowsv1.StepState{StepId: stepID}
		r.byID[stepID] = ss
		r.order = append(r.order, stepID)
	}
	return ss
}

// marshalLocked renders the accumulated StepStates as a JSON array of
// protojson-encoded elements, symmetric with convert.WorkflowRunToProto's read
// path. Caller holds r.mu.
func (r *runReporter) marshalLocked() ([]byte, error) {
	elems := make([]json.RawMessage, 0, len(r.order))
	for _, id := range r.order {
		b, err := protojson.Marshal(r.byID[id])
		if err != nil {
			return nil, err
		}
		elems = append(elems, b)
	}
	return json.Marshal(elems)
}

// checkpoint writes the current steps snapshot. Best-effort: a lost write
// (transient fault or a cancelled ctx) is logged, not surfaced — the run's
// correctness rests on the terminal finalize, not on any single checkpoint.
func (r *runReporter) checkpoint(ctx context.Context, payload []byte, marshalErr error) {
	if marshalErr != nil {
		r.logger.WarnContext(ctx, "workflow run: step checkpoint marshal failed", "run_id", r.runID, "error", marshalErr)
		return
	}
	if err := db.New(r.pool).UpdateWorkflowRunSteps(ctx, db.UpdateWorkflowRunStepsParams{
		ID:    r.runID,
		Steps: payload,
	}); err != nil {
		r.logger.WarnContext(ctx, "workflow run: step checkpoint failed", "run_id", r.runID, "error", err)
	}
}

// structFromOutput converts a step's output to a proto Struct for persistence.
// Set-activity outputs are always maps; a non-map output (or one carrying a
// non-JSON value) leaves the persisted Output unset rather than failing — the
// authoritative value already lives in the run context.
func structFromOutput(output any) *structpb.Struct {
	m, ok := output.(map[string]any)
	if !ok {
		return nil
	}
	s, err := structpb.NewStruct(m)
	if err != nil {
		return nil
	}
	return s
}
