package engine

import (
	"context"
	"sync"
	"time"
)

// StepStatus is the lifecycle state of a step as observed by a [StepReporter].
type StepStatus int

const (
	// StepStatusUnspecified is the zero value; never emitted.
	StepStatusUnspecified StepStatus = iota
	// StepStatusRunning is emitted when a step begins.
	StepStatusRunning
	// StepStatusSucceeded is emitted when a step finishes with an output.
	StepStatusSucceeded
	// StepStatusFailed is emitted when a step fails with an error.
	StepStatusFailed
)

// StepState is a point-in-time record of one step's lifecycle. The in-memory
// reporter accumulates these; 6b persists the equivalent to WorkflowRun.steps.
type StepState struct {
	ID         string
	Status     StepStatus
	Output     any
	Err        error
	StartedAt  time.Time
	FinishedAt time.Time
}

// StepReporter receives per-step lifecycle events as the interpreter walks a
// workflow. It is the seam by which 6b persists run progress incrementally; the
// interpreter itself never touches a database. Only leaf activity steps report
// (Condition and Parallel are structural), which keeps reported ids aligned
// with the `steps.<id>.output` data model.
//
// Implementations MUST be safe for concurrent calls: Parallel branches report
// from separate goroutines.
type StepReporter interface {
	// StepStarted is called when an activity step begins.
	StepStarted(ctx context.Context, stepID string, startedAt time.Time)
	// StepFinished is called when an activity step succeeds, carrying the
	// output stored under steps.<id>.output.
	StepFinished(ctx context.Context, stepID string, output any, startedAt, finishedAt time.Time)
	// StepFailed is called when an activity step fails.
	StepFailed(ctx context.Context, stepID string, cause error, startedAt, finishedAt time.Time)
}

// InMemoryReporter is a [StepReporter] that accumulates [StepState]s in order
// of event receipt. It is intended for tests. It is safe for concurrent use.
type InMemoryReporter struct {
	mu     sync.Mutex
	states []StepState
}

var _ StepReporter = (*InMemoryReporter)(nil)

// NewInMemoryReporter returns a ready-to-use in-memory reporter.
func NewInMemoryReporter() *InMemoryReporter {
	return &InMemoryReporter{}
}

// StepStarted implements [StepReporter].
func (r *InMemoryReporter) StepStarted(_ context.Context, stepID string, startedAt time.Time) {
	r.append(StepState{
		ID:        stepID,
		Status:    StepStatusRunning,
		StartedAt: startedAt,
	})
}

// StepFinished implements [StepReporter].
func (r *InMemoryReporter) StepFinished(_ context.Context, stepID string, output any, startedAt, finishedAt time.Time) {
	r.append(StepState{
		ID:         stepID,
		Status:     StepStatusSucceeded,
		Output:     output,
		StartedAt:  startedAt,
		FinishedAt: finishedAt,
	})
}

// StepFailed implements [StepReporter].
func (r *InMemoryReporter) StepFailed(_ context.Context, stepID string, cause error, startedAt, finishedAt time.Time) {
	r.append(StepState{
		ID:         stepID,
		Status:     StepStatusFailed,
		Err:        cause,
		StartedAt:  startedAt,
		FinishedAt: finishedAt,
	})
}

// States returns a copy of the accumulated step states in receipt order.
func (r *InMemoryReporter) States() []StepState {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]StepState, len(r.states))
	copy(out, r.states)
	return out
}

func (r *InMemoryReporter) append(s StepState) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.states = append(r.states, s)
}
