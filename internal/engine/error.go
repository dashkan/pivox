// Package engine is the pure interpreter core of the Pivox workflow engine.
//
// It tree-walks a workflowsv1.Sequence against a CEL-backed run context,
// dispatching leaf activities and evaluating conditions and parallel blocks.
// The core is deliberately free of River, database, and network dependencies
// so it is unit-testable in complete isolation; later phases wire those
// concerns onto the seams defined here (the [Activity] interface, the
// [StepReporter] sink, and the [RetryableError] taxonomy).
package engine

import "errors"

// RetryableError marks an error as infra-transient: the underlying failure is
// expected to succeed on a later attempt (network blip, upstream 5xx,
// connection reset). The 6b River worker inspects propagated errors with
// [IsRetryable] and re-runs the whole workflow when one is present.
//
// The retry taxonomy is deliberately terminal-by-default:
//
//   - Every error is TERMINAL unless explicitly wrapped in RetryableError.
//     A terminal error fails the run with no retry.
//   - CEL compile or evaluation failures are ALWAYS terminal — a bad
//     definition does not become valid on retry.
//   - An activity's validation failure (bad config, wrong activity kind,
//     missing required field) is ALWAYS terminal for the same reason.
//   - Only genuinely transient infrastructure faults should be wrapped with
//     [Retryable] — and, in this phase, nothing does: HTTP (6c) and
//     run_workflow (6d) are the first producers of retryable errors.
type RetryableError struct {
	cause error
}

// Retryable wraps cause to signal that the failure is infra-transient and the
// run may be retried. It returns nil when cause is nil so it composes cleanly
// with early-return error plumbing.
func Retryable(cause error) error {
	if cause == nil {
		return nil
	}
	return &RetryableError{cause: cause}
}

// Error implements the error interface.
func (e *RetryableError) Error() string {
	return e.cause.Error()
}

// Unwrap exposes the wrapped cause for errors.Is/errors.As traversal.
func (e *RetryableError) Unwrap() error {
	return e.cause
}

// IsRetryable reports whether err (or anything in its chain) is a
// [RetryableError]. Callers use it to decide whether to re-run the workflow.
func IsRetryable(err error) bool {
	var re *RetryableError
	return errors.As(err, &re)
}
