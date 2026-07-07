package engine

import (
	"context"
	"errors"
)

// This file holds the engine's control-flow signals and the error taxonomy the
// Try/catch and error_sequence machinery inspects. Three kinds of non-nil error
// flow up the walk, distinguished here:
//
//   - endSignal — a SUCCESS-terminate raised by an `end` activity. It unwinds
//     every enclosing block (cancelling Parallel siblings, exactly like a real
//     error) but classifies the run as COMPLETED. A Try never catches it and the
//     error_sequence never runs for it.
//   - failError — a catchable business failure raised by a `fail` activity.
//   - thrownError — the wrapper the interpreter puts around any catchable error
//     as it leaves the step that raised it, so a catch / error_sequence can
//     report which step threw (error.step).
//
// Context cancellation ([context.Canceled] / [context.DeadlineExceeded]) is the
// fourth kind; it is neither caught by a Try nor handled by error_sequence — the
// run is CANCELLED and the worker decides whether to retry.

// endSignal is the sentinel raised by an `end` activity. It is a value type so
// every instance compares equal, letting [errors.Is] match it even if a layer
// ever wraps it. It satisfies error only so it can travel the (T, error) return
// path the walk already uses; it is not a failure.
type endSignal struct{}

// Error implements error. The string never surfaces to a user — the interpreter
// classifies the signal to COMPLETED before it could become a run error.
func (endSignal) Error() string { return "engine: end signal" }

// errEnd is the single end-of-run signal value raised by the `end` activity and
// recognized by [isEndSignal].
var errEnd error = endSignal{}

// isEndSignal reports whether err is the success-terminate signal.
func isEndSignal(err error) bool {
	return errors.Is(err, errEnd)
}

// isContextError reports whether err is a context cancellation or deadline —
// the uncatchable interruption class. A Try does not catch it and the
// error_sequence does not run for it.
func isContextError(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

// failError is the catchable error raised by a `fail` activity. It carries the
// author-supplied message and classifies as code "FAIL" in the error scope.
type failError struct {
	message string
}

// Error implements error.
func (e *failError) Error() string { return e.message }

// thrownError wraps a catchable error with the id of the step that raised it, so
// a catch block or the error_sequence can populate error.step. It preserves the
// cause for [errors.Is]/[errors.As] traversal, so retryability and typed
// inspection (e.g. the http *ResponseError) still work through it.
type thrownError struct {
	stepID string
	cause  error
}

// Error implements error, delegating to the cause so existing message-based
// assertions and the run-error surface are unchanged by the wrapping.
func (e *thrownError) Error() string { return e.cause.Error() }

// Unwrap exposes the cause for chain traversal.
func (e *thrownError) Unwrap() error { return e.cause }

// httpErrorDetail is the optional-behavior interface an error may satisfy to
// expose HTTP response detail in the error scope (error.status / error.body).
// It is defined here — in the consumer — so engine never imports connector;
// connector.ResponseError implements it structurally, and [buildErrorValue]
// discovers it via [errors.As].
type httpErrorDetail interface {
	HTTPStatus() int
	HTTPBody() []byte
}

// Error-scope classification codes exposed as error.code.
const (
	// errorCodeActivityFailed is the default classification for a catchable
	// activity or CEL failure.
	errorCodeActivityFailed = "ACTIVITY_FAILED"
	// errorCodeFail classifies a failure raised by a `fail` activity.
	errorCodeFail = "FAIL"
)

// errorScopeKey is the unexported context key under which a catch / error
// sequence carries its scoped `error` record. Using a private key type prevents
// collisions with any other context value.
type errorScopeKey struct{}

// withErrorScope returns a context carrying ev, so CEL evaluated under it can
// read `error` (and only then). The run-context env has no `error` root, so an
// expression referencing it in a normal step fails to compile — the scope is
// enforced at compile time, not merely by omitting the value.
func withErrorScope(ctx context.Context, ev map[string]any) context.Context {
	return context.WithValue(ctx, errorScopeKey{}, ev)
}

// errorScopeFrom returns the scoped error record when ctx is inside a catch /
// error_sequence, and reports whether one is present.
func errorScopeFrom(ctx context.Context) (map[string]any, bool) {
	ev, ok := ctx.Value(errorScopeKey{}).(map[string]any)
	return ev, ok
}

// buildErrorValue projects a caught error into the CEL `error` record:
// { code, message, step[, status, body] }. `step` comes from the [thrownError]
// wrapper (empty when the error did not originate at a step), `code` is FAIL for
// a `fail` activity and ACTIVITY_FAILED otherwise, and `status`/`body` are added
// only when the underlying error carries HTTP detail.
func buildErrorValue(err error) map[string]any {
	ev := map[string]any{
		"code":    errorCodeActivityFailed,
		"message": err.Error(),
		"step":    "",
	}

	var thrown *thrownError
	if errors.As(err, &thrown) {
		ev["step"] = thrown.stepID
	}

	var fail *failError
	if errors.As(err, &fail) {
		ev["code"] = errorCodeFail
	}

	var httpErr httpErrorDetail
	if errors.As(err, &httpErr) {
		// int64 and string match the JSON-shaped native types the rest of the
		// run context uses, so `error.status` and `error.body` behave like any
		// other CEL scalar.
		ev["status"] = int64(httpErr.HTTPStatus())
		ev["body"] = string(httpErr.HTTPBody())
	}

	return ev
}
