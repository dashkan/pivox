package engine

import (
	"context"
	"time"
)

// Retry-policy defaults, applied by RetryPolicy.normalized to any unset field.
const (
	defaultInitialBackoff = 100 * time.Millisecond
	defaultMaxBackoff     = 30 * time.Second
	defaultMultiplier     = 2.0
)

// RetryPolicy configures the exponential backoff used by [WithRetry]. The zero
// value means "try once, no retry". Callers translate a proto
// workflowsv1.RetryPolicy into this plain struct so the retry helper stays
// free of proto types and is reusable by any activity (http today; db/ai
// later).
type RetryPolicy struct {
	// MaxAttempts is the total number of tries (the first plus retries).
	// Normalized to at least 1.
	MaxAttempts int
	// InitialBackoff is the wait before the first retry. Defaults to 100ms.
	InitialBackoff time.Duration
	// MaxBackoff caps any single backoff. Defaults to 30s.
	MaxBackoff time.Duration
	// Multiplier grows the backoff after each attempt. Defaults to 2.0.
	Multiplier float64
}

// normalized returns p with every unset/invalid field replaced by its default.
func (p RetryPolicy) normalized() RetryPolicy {
	if p.MaxAttempts < 1 {
		p.MaxAttempts = 1
	}
	if p.InitialBackoff <= 0 {
		p.InitialBackoff = defaultInitialBackoff
	}
	if p.MaxBackoff <= 0 {
		p.MaxBackoff = defaultMaxBackoff
	}
	if p.Multiplier <= 0 {
		p.Multiplier = defaultMultiplier
	}
	return p
}

// SleepFunc pauses for d or until ctx is done, returning ctx.Err() on
// cancellation. Tests inject a fake so a retry loop never sleeps real time.
type SleepFunc func(ctx context.Context, d time.Duration) error

// realSleep is the production [SleepFunc]: a ctx-aware timer.
func realSleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// WithRetry runs fn under policy's exponential backoff, retrying only while
// isRetryable reports the returned error retryable AND attempts remain. It is
// the single in-process retry primitive shared by activities so none hand-rolls
// a backoff loop.
//
// The taxonomy is deliberate and load-bearing for the engine's two-level retry
// model:
//
//   - A retryable error that EXHAUSTS the attempt budget is returned AS-IS —
//     never re-wrapped. For the http activity that means an exhausted transient
//     outcome stays a plain (terminal) error, so the run FAILs rather than
//     handing the whole job back to River.
//   - An error isRetryable reports NON-retryable returns immediately. Callers
//     use this to short-circuit an infra fault they want to escalate to a job
//     retry: return it wrapped in [Retryable] and give an isRetryable that
//     reports false for it, so WithRetry passes it straight through and the
//     worker's [IsRetryable] catches it.
//
// sleep may be nil (defaults to a real ctx-aware sleep). A nil isRetryable
// treats every error as non-retryable (single attempt effectively).
func WithRetry[T any](
	ctx context.Context,
	policy RetryPolicy,
	sleep SleepFunc,
	isRetryable func(error) bool,
	fn func(ctx context.Context) (T, error),
) (T, error) {
	if sleep == nil {
		sleep = realSleep
	}
	if isRetryable == nil {
		isRetryable = func(error) bool { return false }
	}
	p := policy.normalized()

	var zero T
	backoff := p.InitialBackoff
	for attempt := 1; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return zero, err
		}

		result, err := fn(ctx)
		if err == nil {
			return result, nil
		}
		// Terminal for this loop: either the error is not retryable, or the
		// budget is spent. Returned unwrapped so exhaustion stays terminal.
		if attempt >= p.MaxAttempts || !isRetryable(err) {
			return zero, err
		}

		if serr := sleep(ctx, backoff); serr != nil {
			return zero, serr
		}
		backoff = nextBackoff(backoff, p)
	}
}

// nextBackoff multiplies cur by the policy multiplier, capped at MaxBackoff.
func nextBackoff(cur time.Duration, p RetryPolicy) time.Duration {
	next := time.Duration(float64(cur) * p.Multiplier)
	if next > p.MaxBackoff {
		next = p.MaxBackoff
	}
	return next
}
