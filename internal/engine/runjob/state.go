package runjob

// Workflow run lifecycle states. Stored in workflow_runs.state as strings that
// mirror the workflowsv1.State enum names, and shared here so the Cloud
// Controller (the WorkflowRuns service) and the Worker Process (the executor)
// use one definition rather than each redeclaring the vocabulary.
const (
	// StatePending is a run accepted but not yet started by the executor.
	StatePending = "PENDING"
	// StateRunning is a run the executor is actively walking.
	StateRunning = "RUNNING"
	// StateWaiting is a run parked on a signal/timer (reserved for the
	// human-in-the-loop phase; no day-1 activity drives it).
	StateWaiting = "WAITING"
	// StateSucceeded is a run that finished with no error.
	StateSucceeded = "SUCCEEDED"
	// StateFailed is a run that stopped on a terminal (non-retryable) error.
	StateFailed = "FAILED"
	// StateCancelled is a run cancelled by CancelWorkflowRun or DeleteWorkflow.
	StateCancelled = "CANCELLED"
)

// IsTerminalState reports whether a run can no longer transition. A run in a
// terminal state neither re-executes (the executor's idempotency guard) nor
// accepts a cancel (the API's precondition).
func IsTerminalState(s string) bool {
	switch s {
	case StateSucceeded, StateFailed, StateCancelled:
		return true
	default:
		return false
	}
}
