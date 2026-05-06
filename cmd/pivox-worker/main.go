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

	"github.com/dashkan/pivox/internal/audit"
	"github.com/dashkan/pivox/internal/firebase"
	"github.com/dashkan/pivox/internal/workers"

	db "github.com/dashkan/pivox/internal/db/generated"
)

const (
	// riverSchema is the Postgres schema River's tables live in.
	// Kept separate from `public` so River's schema-owner identity
	// is visible at the DB level — see CLAUDE.md "no FK across
	// schemas-we-don't-own."
	riverSchema = "river"

	// Periodic-job cadences. Mirror the pre-River tick intervals so
	// behavior matches the old workers exactly — only the scheduler
	// changed, not the work rate. Tune individually if real-world
	// load motivates it.
	purgeOrgsInterval      = 5 * time.Minute
	purgeSpacesInterval    = 5 * time.Minute
	verifyDomainsInterval  = 2 * time.Minute
	reapOperationsInterval = 5 * time.Minute
	cleanupAuthInterval    = 1 * time.Minute
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

	// Audit resolver — shared across LRO workers that need to inflate
	// audit-field UUIDs (created_by/updated_by/deleted_by) into Actor
	// protos for operations.result blobs. Same resolver shape as
	// pivox-cloud uses for live RPCs; LRU + TTL cache shared across
	// jobs in this process.
	auditResolver := audit.NewResolver(audit.Config{Queries: queries})

	// Firebase Auth — required by DeleteAccountWorker for the final
	// phase (auth.DeleteUser). Same Application Default Credentials
	// chain pivox-cloud uses; no Pivox-named config knobs.
	firebaseAuth, err := firebase.NewAuthService(ctx)
	if err != nil {
		return fmt.Errorf("init firebase auth: %w", err)
	}

	// Worker registry. Each tick worker we used to host inline in
	// pivox-cloud becomes a river.Worker registered here; River's
	// scheduler drives invocation via the periodic-job table. The
	// pre-River workers package's hand-rolled tick loop + advisory
	// lock + Worker interface are gone — leader election is River's
	// job, scheduling is the periodic-job table.
	dnsResolver := workers.NewStubDNSResolver(logger)
	riverWorkers := river.NewWorkers()
	// Periodic (tick) workers:
	river.AddWorker(riverWorkers, &workers.PurgeOrgsWorker{Queries: queries, Logger: logger})
	river.AddWorker(riverWorkers, &workers.PurgeSpacesWorker{Queries: queries, Logger: logger})
	river.AddWorker(riverWorkers, &workers.VerifyDomainsWorker{Queries: queries, Resolver: dnsResolver, Logger: logger})
	river.AddWorker(riverWorkers, &workers.ReapOperationsWorker{Queries: queries, Logger: logger})
	river.AddWorker(riverWorkers, &workers.CleanupAuthWorker{Queries: queries, Logger: logger})
	// LRO (on-demand) workers — invoked by lro.Manager.NewLro from
	// pivox-cloud's RPC handlers. Each handles one logical step;
	// multi-step LROs (DeleteOrganization, etc.) are single workers
	// today and migrate to River Pro Workflows + Activities later.
	river.AddWorker(riverWorkers, &workers.UndeleteOrgWorker{Pool: pool, Audit: auditResolver, Logger: logger})
	river.AddWorker(riverWorkers, &workers.UndeleteSpaceWorker{Pool: pool, Audit: auditResolver, Logger: logger})
	river.AddWorker(riverWorkers, &workers.DeleteSpaceWorker{Pool: pool, Audit: auditResolver, Logger: logger})
	river.AddWorker(riverWorkers, &workers.DeleteOrgWorker{Pool: pool, Audit: auditResolver, Logger: logger})
	river.AddWorker(riverWorkers, &workers.DeleteAccountWorker{Pool: pool, Auth: firebaseAuth, Audit: auditResolver, Logger: logger})
	river.AddWorker(riverWorkers, &workers.VerifyDomainWorker{Pool: pool, Logger: logger})

	// Periodic job registrations. RunOnStart=true so a freshly-booted
	// replica does useful work immediately rather than waiting one
	// interval before the first tick. Cadences mirror the pre-River
	// tick intervals exactly.
	periodic := []*river.PeriodicJob{
		river.NewPeriodicJob(
			river.PeriodicInterval(purgeOrgsInterval),
			func() (river.JobArgs, *river.InsertOpts) { return workers.PurgeOrgsArgs{}, nil },
			&river.PeriodicJobOpts{RunOnStart: true},
		),
		river.NewPeriodicJob(
			river.PeriodicInterval(purgeSpacesInterval),
			func() (river.JobArgs, *river.InsertOpts) { return workers.PurgeSpacesArgs{}, nil },
			&river.PeriodicJobOpts{RunOnStart: true},
		),
		river.NewPeriodicJob(
			river.PeriodicInterval(verifyDomainsInterval),
			func() (river.JobArgs, *river.InsertOpts) { return workers.VerifyDomainsArgs{}, nil },
			&river.PeriodicJobOpts{RunOnStart: true},
		),
		river.NewPeriodicJob(
			river.PeriodicInterval(reapOperationsInterval),
			func() (river.JobArgs, *river.InsertOpts) { return workers.ReapOperationsArgs{}, nil },
			&river.PeriodicJobOpts{RunOnStart: true},
		),
		river.NewPeriodicJob(
			river.PeriodicInterval(cleanupAuthInterval),
			func() (river.JobArgs, *river.InsertOpts) { return workers.CleanupAuthArgs{}, nil },
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
