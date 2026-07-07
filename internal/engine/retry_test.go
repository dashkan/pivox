package engine

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeSleeper records the backoff durations it is asked to wait, returning
// immediately so tests never sleep real time. An optional cancelAfter aborts
// the loop by returning a context error on the Nth call.
type fakeSleeper struct {
	waits       []time.Duration
	cancelAfter int // 0 = never cancel
}

func (s *fakeSleeper) sleep(_ context.Context, d time.Duration) error {
	s.waits = append(s.waits, d)
	if s.cancelAfter > 0 && len(s.waits) >= s.cancelAfter {
		return context.Canceled
	}
	return nil
}

func alwaysRetryable(error) bool { return true }

func TestWithRetry_SucceedsFirstTryNoSleep(t *testing.T) {
	t.Parallel()

	sl := &fakeSleeper{}
	calls := 0
	got, err := WithRetry(context.Background(),
		RetryPolicy{MaxAttempts: 3},
		sl.sleep, alwaysRetryable,
		func(context.Context) (int, error) {
			calls++
			return 42, nil
		})

	require.NoError(t, err)
	assert.Equal(t, 42, got)
	assert.Equal(t, 1, calls)
	assert.Empty(t, sl.waits, "no sleep on immediate success")
}

func TestWithRetry_RetriesThenSucceeds(t *testing.T) {
	t.Parallel()

	sl := &fakeSleeper{}
	calls := 0
	transient := errors.New("boom")
	got, err := WithRetry(context.Background(),
		RetryPolicy{MaxAttempts: 4, InitialBackoff: 10 * time.Millisecond, MaxBackoff: time.Second, Multiplier: 2},
		sl.sleep, alwaysRetryable,
		func(context.Context) (string, error) {
			calls++
			if calls < 3 {
				return "", transient
			}
			return "ok", nil
		})

	require.NoError(t, err)
	assert.Equal(t, "ok", got)
	assert.Equal(t, 3, calls)
	// Two sleeps (before attempt 2 and 3), backoff doubling.
	assert.Equal(t, []time.Duration{10 * time.Millisecond, 20 * time.Millisecond}, sl.waits)
}

func TestWithRetry_ExhaustsAndReturnsLastError(t *testing.T) {
	t.Parallel()

	sl := &fakeSleeper{}
	calls := 0
	transient := errors.New("still failing")
	_, err := WithRetry(context.Background(),
		RetryPolicy{MaxAttempts: 3, InitialBackoff: 5 * time.Millisecond, Multiplier: 3},
		sl.sleep, alwaysRetryable,
		func(context.Context) (int, error) {
			calls++
			return 0, transient
		})

	require.ErrorIs(t, err, transient)
	assert.Equal(t, 3, calls)
	assert.Len(t, sl.waits, 2, "sleeps between the 3 attempts")
	// The exhausted error is returned as-is, never wrapped as retryable.
	assert.False(t, IsRetryable(err))
}

func TestWithRetry_NonRetryableStopsImmediately(t *testing.T) {
	t.Parallel()

	sl := &fakeSleeper{}
	calls := 0
	terminal := errors.New("bad request")
	_, err := WithRetry(context.Background(),
		RetryPolicy{MaxAttempts: 5, InitialBackoff: time.Millisecond},
		sl.sleep,
		func(error) bool { return false }, // nothing is retryable
		func(context.Context) (int, error) {
			calls++
			return 0, terminal
		})

	require.ErrorIs(t, err, terminal)
	assert.Equal(t, 1, calls, "no retry on a non-retryable error")
	assert.Empty(t, sl.waits)
}

func TestWithRetry_BackoffCappedAtMax(t *testing.T) {
	t.Parallel()

	sl := &fakeSleeper{}
	_, _ = WithRetry(context.Background(),
		RetryPolicy{MaxAttempts: 5, InitialBackoff: 100 * time.Millisecond, MaxBackoff: 250 * time.Millisecond, Multiplier: 10},
		sl.sleep, alwaysRetryable,
		func(context.Context) (int, error) { return 0, errors.New("x") })

	// 100ms, then 100*10=1000 capped to 250, then 250 (already at cap), 250.
	assert.Equal(t, []time.Duration{
		100 * time.Millisecond,
		250 * time.Millisecond,
		250 * time.Millisecond,
		250 * time.Millisecond,
	}, sl.waits)
}

func TestWithRetry_ContextCancelDuringSleep(t *testing.T) {
	t.Parallel()

	sl := &fakeSleeper{cancelAfter: 1}
	calls := 0
	_, err := WithRetry(context.Background(),
		RetryPolicy{MaxAttempts: 5, InitialBackoff: time.Second},
		sl.sleep, alwaysRetryable,
		func(context.Context) (int, error) {
			calls++
			return 0, errors.New("transient")
		})

	require.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, 1, calls, "loop aborts when the backoff sleep is cancelled")
}

func TestWithRetry_DefaultsSingleAttempt(t *testing.T) {
	t.Parallel()

	sl := &fakeSleeper{}
	calls := 0
	_, err := WithRetry(context.Background(),
		RetryPolicy{}, // zero policy → one attempt, no retry
		sl.sleep, alwaysRetryable,
		func(context.Context) (int, error) {
			calls++
			return 0, errors.New("boom")
		})

	require.Error(t, err)
	assert.Equal(t, 1, calls)
	assert.Empty(t, sl.waits)
}
