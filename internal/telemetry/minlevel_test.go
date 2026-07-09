package telemetry

import (
	"bytes"
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMinLevelHandler(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	base := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	logger := slog.New(minLevelHandler{Handler: base, min: slog.LevelWarn})

	logger.Debug("dropped_debug")
	logger.Info("dropped_info")
	// WithAttrs/WithGroup must preserve the floor, not unwrap back to base.
	logger.With("k", "v").Info("dropped_info_withattrs")
	logger.WithGroup("g").Info("dropped_info_withgroup")
	logger.Warn("kept_warn")
	logger.Error("kept_error")

	out := buf.String()
	assert.NotContains(t, out, "dropped_debug")
	assert.NotContains(t, out, "dropped_info")
	assert.NotContains(t, out, "dropped_info_withattrs")
	assert.NotContains(t, out, "dropped_info_withgroup")
	assert.Contains(t, out, "kept_warn")
	assert.Contains(t, out, "kept_error")
}

// The Handle-level floor is defensive depth — in the slog.Logger path Enabled
// already gates it. Exercise Handle directly (as a fan-out handler that doesn't
// re-check Enabled would) to prove a below-min record never reaches the wrapped
// handler.
func TestMinLevelHandlerHandleDropsBelowMin(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	base := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	h := minLevelHandler{Handler: base, min: slog.LevelWarn}

	below := slog.NewRecord(time.Time{}, slog.LevelInfo, "below_min", 0)
	require.NoError(t, h.Handle(context.Background(), below))
	assert.Empty(t, buf.String(), "Handle must drop a record below min even when called directly")

	at := slog.NewRecord(time.Time{}, slog.LevelWarn, "at_min", 0)
	require.NoError(t, h.Handle(context.Background(), at))
	assert.Contains(t, buf.String(), "at_min")
}
