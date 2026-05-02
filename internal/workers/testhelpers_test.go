package workers

import (
	"io"
	"log/slog"
)

// silentLogger discards all log output. Workers' Work() methods log
// at INFO/ERROR through ctx-aware slog handlers; tests don't care
// about the output, just the SQL calls. Centralized here so the
// per-worker test files don't each ship their own definition.
func silentLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }
