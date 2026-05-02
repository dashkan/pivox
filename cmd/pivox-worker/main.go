// Pivox worker process — hosts every background goroutine that
// would otherwise live inside pivox-cloud. Splits the gRPC API
// surface (pivox-cloud) from the work surface (pivox-worker) so
// the two can scale independently and a heavy LRO can't starve
// request handlers.
//
// All scheduled / queued work runs through River. River's leader
// election handles multi-replica coordination — there is no
// hand-rolled advisory-lock dance here. River's tables live in
// the `river` Postgres schema; pivox-worker runs River's own
// migrations idempotently on boot so the schema state is always
// caught up to whatever River version the binary was built with.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"
	"github.com/spf13/cobra"

	"github.com/dashkan/pivox/internal/workers"

	db "github.com/dashkan/pivox/internal/db/generated"
)

const (
	// riverSchema is the Postgres schema River's tables live in.
	// Kept separate from `public` so River's schema-owner identity
	// is visible at the DB level — see CLAUDE.md "no FK across
	// schemas-we-don't-own."
	riverSchema = "river"

	// purgeOrgsInterval mirrors the pre-River PurgeWorker cadence.
	// Steady-state load is at most a handful of orgs per tick;
	// bumping this won't materially help and would just spread the
	// cascade work across more wakeups.
	purgeOrgsInterval = 5 * time.Minute
)

var version = "dev"

func main() {
	rootCmd := &cobra.Command{
		Use:     "pivox-worker",
		Short:   "Pivox background worker (River-driven)",
		Version: version,
		RunE:    serve,
	}
	f := rootCmd.Flags()
	f.String("database-url", envOrDefault("PIVOX_DATABASE_URL", "postgres://localhost:5432/pivox?sslmode=disable"), "PostgreSQL connection URL")
	f.String("log-level", envOrDefault("PIVOX_LOG_LEVEL", "info"), "Log level (debug, info, warn, error)")
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func envOrDefault(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}

func mustString(s string, _ error) string { return s }

func serve(cmd *cobra.Command, _ []string) error {
	f := cmd.Flags()
	databaseURL := mustString(f.GetString("database-url"))
	logLevel := mustString(f.GetString("log-level"))

	var level slog.Level
	switch logLevel {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level}))
	slog.SetDefault(logger)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return fmt.Errorf("connect to database: %w", err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		return fmt.Errorf("ping database: %w", err)
	}
	logger.Info("connected to database")

	driver := riverpgxv5.New(pool)

	// Run River's own migrations into `river` schema. Idempotent —
	// only un-applied versions are executed. This is how River's
	// schema (river_job, river_queue, river_leader, ...) gets created
	// on a fresh DB and how it stays current after a binary upgrade.
	migrator, err := rivermigrate.New(driver, &rivermigrate.Config{
		Logger: logger,
		Schema: riverSchema,
	})
	if err != nil {
		return fmt.Errorf("river migrator: %w", err)
	}
	if _, err := migrator.Migrate(ctx, rivermigrate.DirectionUp, nil); err != nil {
		return fmt.Errorf("river migrate up: %w", err)
	}

	queries := db.New(pool)

	// Worker registry. Each tick worker we used to host inline in
	// pivox-cloud becomes a river.Worker registered here; River's
	// scheduler drives invocation via the periodic-job table.
	riverWorkers := river.NewWorkers()
	river.AddWorker(riverWorkers, &workers.PurgeOrgsWorker{Queries: queries, Logger: logger})

	// Periodic job registrations. RunOnStart=true so a freshly-booted
	// replica does useful work immediately rather than waiting one
	// interval before the first tick.
	periodic := []*river.PeriodicJob{
		river.NewPeriodicJob(
			river.PeriodicInterval(purgeOrgsInterval),
			func() (river.JobArgs, *river.InsertOpts) { return workers.PurgeOrgsArgs{}, nil },
			&river.PeriodicJobOpts{RunOnStart: true},
		),
	}

	client, err := river.NewClient(driver, &river.Config{
		Logger: logger,
		Queues: map[string]river.QueueConfig{
			river.QueueDefault: {MaxWorkers: 10},
		},
		Schema:       riverSchema,
		Workers:      riverWorkers,
		PeriodicJobs: periodic,
	})
	if err != nil {
		return fmt.Errorf("river client: %w", err)
	}

	if err := client.Start(ctx); err != nil {
		return fmt.Errorf("river start: %w", err)
	}
	logger.Info("pivox-worker running", "queues", []string{river.QueueDefault}, "periodic_jobs", len(periodic))

	// Block until signal. River's Start goroutine runs until the
	// context cancels or Stop is called.
	<-ctx.Done()
	logger.Info("shutting down...")

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer stopCancel()
	if err := client.Stop(stopCtx); err != nil {
		logger.Warn("river stop returned error", "error", err)
	}
	logger.Info("pivox-worker stopped")
	return nil
}
