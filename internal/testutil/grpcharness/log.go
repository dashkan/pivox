package grpcharness

import (
	"io"
	"log/slog"
)

// SilentLogger returns a slog.Logger that drops every record. Tests
// don't want server-side log noise polluting test output; on
// failure, the test's own assertions surface the relevant state.
//
// Exported so call sites that pass loggers into worker structs
// (e.g. workers.DeleteOrgWorker.Logger) don't each ship their own
// io.Discard literal.
func SilentLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
