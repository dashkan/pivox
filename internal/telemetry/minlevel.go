package telemetry

import (
	"context"
	"log/slog"
)

// minLevelHandler drops records below min before delegating to the wrapped
// handler. It gives the OTel log bridge its own floor (PIVOX_OTEL_LOG_LEVEL)
// independent of the stdout handler's level (PIVOX_LOG_LEVEL) — so verbose
// debug logging can stay local while the dashboard holds at info+.
//
// It gates in BOTH Enabled (so slog skips record construction when nothing
// downstream wants the level) and Handle (defensive: correct even if a parent
// fan-out handler calls Handle without re-checking Enabled). WithAttrs/WithGroup
// re-wrap so the floor survives derived loggers.
type minLevelHandler struct {
	slog.Handler
	min slog.Level
}

func (h minLevelHandler) Enabled(ctx context.Context, l slog.Level) bool {
	return l >= h.min && h.Handler.Enabled(ctx, l)
}

func (h minLevelHandler) Handle(ctx context.Context, r slog.Record) error {
	if r.Level < h.min {
		return nil
	}
	return h.Handler.Handle(ctx, r)
}

func (h minLevelHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return minLevelHandler{Handler: h.Handler.WithAttrs(attrs), min: h.min}
}

func (h minLevelHandler) WithGroup(name string) slog.Handler {
	return minLevelHandler{Handler: h.Handler.WithGroup(name), min: h.min}
}
