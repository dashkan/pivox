package engine

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRetryableTaxonomy(t *testing.T) {
	t.Parallel()

	cause := errors.New("connection reset")

	t.Run("plain error is terminal", func(t *testing.T) {
		t.Parallel()
		assert.False(t, IsRetryable(cause))
	})

	t.Run("wrapped error is retryable", func(t *testing.T) {
		t.Parallel()
		err := Retryable(cause)
		require.Error(t, err)
		assert.True(t, IsRetryable(err))
		assert.ErrorIs(t, err, cause)
	})

	t.Run("retryable survives further wrapping", func(t *testing.T) {
		t.Parallel()
		err := fmt.Errorf("engine: activity failed: %w", Retryable(cause))
		assert.True(t, IsRetryable(err))
	})

	t.Run("nil cause yields nil", func(t *testing.T) {
		t.Parallel()
		assert.NoError(t, Retryable(nil))
		assert.False(t, IsRetryable(nil))
	})
}
