package lro

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/dashkan/pivox/internal/testutil/mocks"
)

func TestNewReaper(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	r := NewReaper(ReaperConfig{Queries: mockQ, Interval: 1 * time.Hour, Logger: logger})
	require.NotNil(t, r)
	assert.Equal(t, 1*time.Hour, r.interval)
	assert.NotNil(t, r.queries)
	assert.NotNil(t, r.logger)
}

func TestReaper_Run(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	// Use a very short interval so the ticker fires quickly.
	r := NewReaper(ReaperConfig{Queries: mockQ, Interval: 10 * time.Millisecond, Logger: logger})

	called := make(chan struct{}, 10)
	mockQ.On("DeleteExpiredOperations", mock.Anything).Return(nil).Run(func(_ mock.Arguments) {
		select {
		case called <- struct{}{}:
		default:
		}
	})

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	err := r.Run(ctx)
	require.Error(t, err) // context.DeadlineExceeded or context.Canceled
	assert.ErrorIs(t, err, context.DeadlineExceeded)

	// Verify it was called at least once
	assert.NotEmpty(t, called, "DeleteExpiredOperations should have been called at least once")
}
