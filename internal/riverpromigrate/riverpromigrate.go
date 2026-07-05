// Package riverpromigrate applies River's database migrations, including
// the River Pro lines.
//
// River Pro ships migrations across multiple "lines" (the base `main`
// line plus a `pro` line that adds workflow/signal/timer/producer
// tables). rivermigrate applies a single line per call, so establishing
// the full schema requires iterating the driver's default lines — which
// is exactly what Up does. Callers that only ran the `main` line will
// hit "relation river.river_producer does not exist" the first time the
// Pro client inserts a job.
package riverpromigrate

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/riverqueue/river/rivermigrate"
	"riverqueue.com/riverpro/driver/riverpropgxv5"
)

// Up applies every default River migration line (base `main` + `pro`)
// into schema, in order. Idempotent — already-applied versions are
// skipped. logger may be nil (rivermigrate defaults to stdout at warn).
func Up(ctx context.Context, driver *riverpropgxv5.Driver, schema string, logger *slog.Logger) error {
	for _, line := range driver.GetMigrationDefaultLines() {
		migrator, err := rivermigrate.New(driver, &rivermigrate.Config{
			Line:   line,
			Schema: schema,
			Logger: logger,
		})
		if err != nil {
			return fmt.Errorf("river migrator (line %q): %w", line, err)
		}
		if _, err := migrator.Migrate(ctx, rivermigrate.DirectionUp, nil); err != nil {
			return fmt.Errorf("river migrate up (line %q): %w", line, err)
		}
	}
	return nil
}
