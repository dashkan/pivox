package connector

import (
	"fmt"
	"net/http"
)

// transientError marks an in-process-retryable HTTP outcome: a network/transport
// failure, or a response whose status the activity classified as retryable (5xx
// or a code in retryable_status). The http activity's retry classifier reports
// exactly this type retryable; [engine.RetryableError] (an infra fault) and
// terminal errors are NOT this type, so they pass straight through the retry
// loop.
//
// When the attempt budget is exhausted a transientError is returned unwrapped —
// so an exhausted transient outcome is terminal (it fails the run) rather than a
// job retry. Its cause is preserved so a future catch can inspect it (via
// errors.As to a [*ResponseError] when the transient was a status, or the raw
// transport error for a network failure).
type transientError struct {
	cause error
}

func (e *transientError) Error() string { return e.cause.Error() }
func (e *transientError) Unwrap() error { return e.cause }

// ResponseError is a non-success HTTP response — a status that is neither 2xx
// nor in success_status. It is terminal, and it carries the status, headers, and
// body so a future Try/catch can branch on error.status and error.body.
type ResponseError struct {
	Status  int
	Headers http.Header
	Body    []byte
}

func (e *ResponseError) Error() string {
	return fmt.Sprintf("http request returned status %d", e.Status)
}

// newResponseError snapshots a non-success [Response] into a [ResponseError].
func newResponseError(resp *Response) *ResponseError {
	return &ResponseError{
		Status:  resp.Status,
		Headers: resp.Headers,
		Body:    resp.Body,
	}
}
