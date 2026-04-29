//go:build dev

package grpcharness

import (
	"io"
	"log/slog"
)

// testLogger returns a slog.Logger that drops every record. Tests
// don't want server-side log noise polluting test output; on
// failure, the test's own assertions surface the relevant state.
func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
