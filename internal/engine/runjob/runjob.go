// Package runjob defines the River job contract for executing a workflow run.
//
// It lives in its own package — separate from the pure interpreter core in
// internal/engine and from the executor worker in internal/workers — so both
// sides of the wiring can depend on it without an import cycle: the Cloud
// Controller (internal/service/workflows) enqueues the job transactionally in
// RunWorkflow, and the Worker Process (internal/workers) executes it. Keeping
// the contract here also keeps internal/engine free of a River dependency, per
// that package's "pure, network-free interpreter" charter.
package runjob

import "github.com/google/uuid"

// Args is the River job payload for a workflow run. It carries only the run's
// id; the executor loads the run row, its pinned version definition, trigger,
// and input from the database. A minimal payload is a stable payload — a change
// to what a run needs at execution time never changes the enqueued job shape,
// so in-flight jobs never get orphaned by a field addition.
type Args struct {
	// RunID is the workflow_runs.id of the run to execute.
	RunID uuid.UUID `json:"run_id"`
}

// Kind implements river.JobArgs. Stable string — changing it would orphan
// in-flight rows.
func (Args) Kind() string { return "workflow_run" }
