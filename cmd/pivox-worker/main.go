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
	"github.com/dashkan/pivox/internal/telemetry"
	"github.com/dashkan/pivox/internal/telemetry/rivertrace"
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
	// Web sessions lazy-expire on read, so this is pure GC of rows for
	// sessions never read again before their 30-day horizon — hourly is
	// ample; a tighter cadence would only churn the table.
	purgeWebSessionsInterval = 1 * time.Hour
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
	f.String("sessions-database-url", envOrDefault("PIVOX_SESSIONS_DATABASE_URL", "postgres://localhost:5432/sessions?sslmode=disable"), "PostgreSQL connection URL for the BFF-owned web_sessions store (purge_web_sessions job)")
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
	sessionsURL := mustString(f.GetString("sessions-database-url"))
	logLevel := mustString(f.GetString("log-level"))

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// Logger + OpenTelemetry (traces + metrics + logs) in one bootstrap. OTel
	// is a no-op unless an OTLP endpoint is configured (the Aspire AppHost
	// injects it); the logger always writes JSON to stdout.
	logger, otelShutdown, err := telemetry.Setup(ctx, telemetry.Config{
		ServiceName: "pivox-worker",
		LogLevel:    logLevel,
	})
	if err != nil {
		return fmt.Errorf("setup telemetry: %w", err)
	}
	defer func() {
		if err := otelShutdown(context.Background()); err != nil {
			logger.Warn("telemetry shutdown", "error", err)
		}
	}()

	// db.NewPool wires the otelpgx query tracer + pgvector per-connection type
	// registration (required to decode `vector` columns like assets.embedding,
	// which worker jobs touch) — shared with cloud + the test harness.
	pool, err := db.NewPool(ctx, databaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	logger.Info("connected to database")

	// Separate pool for the BFF-owned `sessions` database (web_sessions store),
	// which the purge_web_sessions job GCs. Plain pgxpool.New — NOT db.NewPool —
	// because that DB has no pgvector/`vector` columns, so the per-connection
	// pgvector type registration db.NewPool does would be pointless here. The
	// BFF owns + creates this schema; the worker only deletes from it.
	sessionsPool, err := pgxpool.New(ctx, sessionsURL)
	if err != nil {
		return fmt.Errorf("connect sessions database: %w", err)
	}
	defer sessionsPool.Close()
	logger.Info("connected to sessions database")

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
	river.AddWorker(riverWorkers, &workers.PurgeWebSessionsWorker{Pool: sessionsPool, Logger: logger})
	// LRO (on-demand) workers — invoked by lro.Manager.NewLro from
	// pivox-cloud's RPC handlers. Each handles one logical step;
	// multi-step LROs (DeleteOrganization, etc.) are single workers
	// today and migrate to River Pro Workflows + Activities later.
	river.AddWorker(riverWorkers, &workers.UndeleteOrgWorker{Pool: pool, Audit: auditResolver, Logger: logger})
	river.AddWorker(riverWorkers, &workers.UndeleteSpaceWorker{Pool: pool, Audit: auditResolver, Logger: logger})
	river.AddWorker(riverWorkers, &workers.DeleteSpaceWorker{Pool: pool, Audit: auditResolver, Logger: logger})
	river.AddWorker(riverWorkers, &workers.DeleteOrgWorker{Pool: pool, Audit: auditResolver, Logger: logger})
	river.AddWorker(riverWorkers, &workers.DeleteAccountWorker{Pool: pool, Audit: auditResolver, Logger: logger})
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
		river.NewPeriodicJob(
			river.PeriodicInterval(purgeWebSessionsInterval),
			func() (river.JobArgs, *river.InsertOpts) { return workers.PurgeWebSessionsArgs{}, nil },
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
		// rivertrace (outer) restores the enqueuing request's trace context
		// from job metadata; otelriver then opens river.work as a child of
		// it — so api insert and worker execution share one distributed trace.
		// The ordering is load-bearing, so the slice is built in one place.
		Middleware: rivertrace.Middlewares(),
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
