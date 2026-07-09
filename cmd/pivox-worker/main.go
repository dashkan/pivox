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
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/dashkan/pivox/internal/riverpromigrate"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/spf13/cobra"
	"riverqueue.com/riverpro"
	"riverqueue.com/riverpro/driver/riverpropgxv5"

	"github.com/dashkan/pivox/internal/audit"
	"github.com/dashkan/pivox/internal/crypto"
	"github.com/dashkan/pivox/internal/engine"
	"github.com/dashkan/pivox/internal/engine/connector"
	"github.com/dashkan/pivox/internal/identitysync"
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
	// Web sessions lazy-expire on read, so this is pure GC of rows for
	// sessions never read again before their 30-day horizon — hourly is
	// ample; a tighter cadence would only churn the table.
	purgeWebSessionsInterval = 1 * time.Hour
	// defaultWorkflowReaperInterval is the cadence of the stranded-run reaper —
	// the backstop that finalizes runs whose River job was discarded but whose
	// discard ErrorHandler didn't finalize them. Fast (1m) because a stranded run
	// is stuck in a non-terminal state until reaped; overridable via
	// --workflow-reaper-interval / PIVOX_WORKFLOW_REAPER_INTERVAL.
	defaultWorkflowReaperInterval = 1 * time.Minute

	// River fetch-poll cadence + its bounds. River picks up newly-inserted jobs
	// immediately via LISTEN/NOTIFY, so this poll is only the fallback for missed
	// notifications — so a long default keeps the worker quiet (an idle poll is a
	// DB query, i.e. an otelpgx span + log every tick) at no job-latency cost.
	// Overridable via --river-poll-interval / PIVOX_WORKER_RIVER_POLL_INTERVAL,
	// clamped to [min, max] so a stray value can't disable the fallback (too long)
	// or hammer the DB (too short).
	defaultRiverPollInterval = 5 * time.Minute
	minRiverPollInterval     = 30 * time.Second
	maxRiverPollInterval     = 10 * time.Minute
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
	f.String("kafka-brokers", envOrDefault("PIVOX_KAFKA_BROKERS", "localhost:9092"), "Comma-separated Kafka seed brokers for the Keycloak identity-sync consumer")
	f.String("kc-realm", envOrDefault("PIVOX_KC_REALM", "pivox"), "Keycloak realm whose events the identity-sync consumer provisions")
	f.String("encryption-provider", envOrDefault("PIVOX_ENCRYPTION_PROVIDER", "local"), "At-rest encryption backend: local (cleartext Tink keyset) or gcp (Cloud KMS)")
	f.String("encryption-local-keyset", envOrDefault("PIVOX_ENCRYPTION_LOCAL_KEYSET", ""), "base64 cleartext Tink keyset; required when encryption-provider=local")
	f.String("encryption-gcp-kms-key-name", envOrDefault("PIVOX_ENCRYPTION_GCP_KMS_KEY_NAME", ""), "Cloud KMS key resource name; required when encryption-provider=gcp")
	f.String("log-level", envOrDefault("PIVOX_LOG_LEVEL", "info"), "Log level (debug, info, warn, error)")
	f.Bool("workflow-allow-internal-networks", envOrBool("PIVOX_WORKFLOW_ALLOW_INTERNAL_NETWORKS", false), "Allow workflow HTTP activities to reach internal/private network addresses (loopback, link-local, private, metadata). Default false — REQUIRED for shared multi-tenant cloud. Set true only for single-tenant on-prem where the worker legitimately reaches internal systems.")
	f.Int64("workflow-http-max-response-size", envOrInt64("PIVOX_WORKFLOW_HTTP_MAX_RESPONSE_SIZE", 1<<20), "Max bytes a workflow HTTP activity reads from a response body. Default 1048576 (1 MiB); clamped to [524288 (512 KiB), 10485760 (10 MiB)].")
	f.Duration("workflow-reaper-interval", envOrDuration("PIVOX_WORKFLOW_REAPER_INTERVAL", defaultWorkflowReaperInterval), "How often the stranded-run reaper scans for discarded workflow_run jobs whose run is still non-terminal and finalizes them FAILED. Default 1m.")
	f.Duration("river-poll-interval", envOrDuration("PIVOX_WORKER_RIVER_POLL_INTERVAL", defaultRiverPollInterval), "River fetch-poll interval — the FALLBACK cadence for picking up jobs (new jobs arrive instantly via LISTEN/NOTIFY, so a long value only delays recovery from a missed notification, and keeps the worker quiet). Default 5m; clamped to [30s, 10m].")
	telemetry.RegisterOtelFlags(f)
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

func envOrBool(key string, defaultVal bool) bool {
	if v := os.Getenv(key); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return defaultVal
}

func envOrInt64(key string, defaultVal int64) int64 {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			return n
		}
	}
	return defaultVal
}

func envOrDuration(key string, defaultVal time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return defaultVal
}

func mustString(s string, _ error) string { return s }

func mustBool(b bool, _ error) bool { return b }

func mustInt64(n int64, _ error) int64 { return n }

func mustDuration(d time.Duration, _ error) time.Duration { return d }

// clampRiverPollInterval bounds the River fetch-poll interval to
// [minRiverPollInterval, maxRiverPollInterval] so an out-of-range flag/env value
// can neither disable the poll fallback (too long) nor hammer the DB (too short).
func clampRiverPollInterval(d time.Duration) time.Duration {
	return min(max(d, minRiverPollInterval), maxRiverPollInterval)
}

func serve(cmd *cobra.Command, _ []string) error {
	f := cmd.Flags()
	databaseURL := mustString(f.GetString("database-url"))
	sessionsURL := mustString(f.GetString("sessions-database-url"))
	kafkaBrokers := mustString(f.GetString("kafka-brokers"))
	kcRealm := mustString(f.GetString("kc-realm"))
	encryptionProvider := mustString(f.GetString("encryption-provider"))
	encryptionLocalKeyset := mustString(f.GetString("encryption-local-keyset"))
	encryptionGCPKMSKeyName := mustString(f.GetString("encryption-gcp-kms-key-name"))
	logLevel := mustString(f.GetString("log-level"))
	allowInternalNetworks := mustBool(f.GetBool("workflow-allow-internal-networks"))
	maxResponseBytes := mustInt64(f.GetInt64("workflow-http-max-response-size"))
	workflowReaperInterval := mustDuration(f.GetDuration("workflow-reaper-interval"))
	riverPollInterval := clampRiverPollInterval(mustDuration(f.GetDuration("river-poll-interval")))

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// Logger + OpenTelemetry (traces + metrics + logs) in one bootstrap. OTel
	// is a no-op unless an OTLP endpoint is configured (the Aspire AppHost
	// injects it); the logger always writes JSON to stdout.
	logger, otelShutdown, err := telemetry.Setup(ctx, telemetry.Config{
		ServiceName: "pivox-worker",
		LogLevel:    logLevel,
		Otel:        telemetry.OtelConfigFromFlags(f),
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

	driver := riverpropgxv5.New(pool)

	// Run River's own migrations into `river` schema. Idempotent —
	// only un-applied versions are executed. This is how River's
	// schema (river_job, river_queue, river_leader, ...) gets created
	// on a fresh DB and how it stays current after a binary upgrade.
	if err := riverpromigrate.Up(ctx, driver, riverSchema, logger); err != nil {
		return fmt.Errorf("river migrate: %w", err)
	}

	queries := db.New(pool)

	// Audit resolver — shared across LRO workers that need to inflate
	// audit-field UUIDs (created_by/updated_by/deleted_by) into Actor
	// protos for operations.result blobs. Same resolver shape as
	// pivox-cloud uses for live RPCs; LRU + TTL cache shared across
	// jobs in this process.
	auditResolver := audit.NewResolver(audit.Config{Queries: queries})

	// At-rest encryptor — the worker needs it (unlike the pre-6c worker) because
	// the http activity's connector broker decrypts vault Secrets to inject them
	// into outbound requests. Constructed identically to pivox-cloud so both
	// processes decrypt under the same key.
	enc, err := crypto.NewEncryptor(crypto.EncryptorConfig{
		Provider:       crypto.Provider(encryptionProvider),
		LocalKeysetB64: encryptionLocalKeyset,
		GCPKMSKeyName:  encryptionGCPKMSKeyName,
	})
	if err != nil {
		return fmt.Errorf("initialize encryptor: %w", err)
	}

	// Workflow engine interpreter — the pure, network-free core shared across
	// every run job. Constructed once (thread-safe: the CEL evaluator caches
	// compiled programs under a mutex, the dispatcher is immutable). The
	// dispatcher wires `set`, `http`, and `run_workflow` (the last recurses back
	// into this same interpreter in-process for sub-workflows).
	//
	// The connector broker is the single secret-injecting execution path: it
	// resolves a connector's credentialed config, decrypts referenced Secrets via
	// enc, and performs the outbound call. The http activity drives it under an
	// in-process retry loop.
	evaluator, err := engine.NewEvaluator()
	if err != nil {
		return fmt.Errorf("build workflow evaluator: %w", err)
	}
	broker := connector.NewBroker(connector.Config{
		Queries:               queries,
		Encryptor:             enc,
		AllowInternalNetworks: allowInternalNetworks,
		MaxResponseBytes:      maxResponseBytes,
		Logger:                logger,
	})
	interpreter := engine.NewInterpreter(engine.InterpreterConfig{
		Evaluator: evaluator,
		Dispatcher: engine.NewDispatcher(engine.DispatcherConfig{
			Set: engine.NewSetActivity(engine.SetActivityConfig{Evaluator: evaluator}),
			HTTP: connector.NewHTTPActivity(connector.ActivityConfig{
				Evaluator: evaluator,
				Broker:    broker,
				Store:     queries,
			}),
			// run_workflow recurses back into this same interpreter in-process
			// (via a sub-run capability installed on ctx by Interpreter.Run), so
			// no interpreter reference is needed at construction. Store=queries
			// resolves the target workflow/version.
			RunWorkflow: engine.NewRunWorkflowActivity(engine.RunWorkflowActivityConfig{
				Evaluator: evaluator,
				Store:     queries,
			}),
		}),
	})

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
	// Workflow-run executor (Phase 6b). Enqueued by the Cloud Controller's
	// RunWorkflow; runs the pinned version's definition through the interpreter.
	river.AddWorker(riverWorkers, &workers.RunWorkflowWorker{Pool: pool, Interpreter: interpreter, Logger: logger})
	// Stranded-run reaper (periodic backstop). Finalizes runs whose workflow_run
	// job was DISCARDED but whose discard ErrorHandler didn't finalize them. The
	// immediate path is the ErrorHandler wired into the client Config below.
	river.AddWorker(riverWorkers, &workers.ReapStrandedRunsWorker{Pool: pool, Logger: logger})

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
			river.PeriodicInterval(purgeWebSessionsInterval),
			func() (river.JobArgs, *river.InsertOpts) { return workers.PurgeWebSessionsArgs{}, nil },
			&river.PeriodicJobOpts{RunOnStart: true},
		),
		river.NewPeriodicJob(
			river.PeriodicInterval(workflowReaperInterval),
			func() (river.JobArgs, *river.InsertOpts) { return workers.ReapStrandedRunsArgs{}, nil },
			&river.PeriodicJobOpts{RunOnStart: true},
		),
	}

	client, err := riverpro.NewClient(driver, &riverpro.Config{
		Config: river.Config{
			Logger: logger,
			// Fallback fetch cadence — LISTEN/NOTIFY picks up new jobs instantly, so
			// a long interval keeps idle-poll spans/logs down at no latency cost.
			FetchPollInterval: riverPollInterval,
			Queues: map[string]river.QueueConfig{
				river.QueueDefault: {MaxWorkers: 10},
			},
			Schema:       riverSchema,
			Workers:      riverWorkers,
			PeriodicJobs: periodic,
			// Immediate stranded-run defense: when a workflow_run job's final
			// attempt errors/panics (River about to discard it), finalize the run
			// FAILED so it never dangles non-terminal. The periodic reaper above is
			// the backstop for whatever this handler misses.
			ErrorHandler: &workers.RunWorkflowErrorHandler{Pool: pool, Logger: logger},
			// rivertrace (outer) restores the enqueuing request's trace context
			// from job metadata; otelriver then opens river.work as a child of
			// it — so api insert and worker execution share one distributed trace.
			// The ordering is load-bearing, so the slice is built in one place.
			Middleware: rivertrace.Middlewares(),
		},
	})
	if err != nil {
		return fmt.Errorf("river client: %w", err)
	}

	if err := client.Start(ctx); err != nil {
		return fmt.Errorf("river start: %w", err)
	}
	logger.Info("pivox-worker running", "queues", []string{river.QueueDefault}, "periodic_jobs", len(periodic))

	// Keycloak → Pivox identity-sync consumer. Provisions / tombstones
	// `identities` rows from the keycloak-events Kafka topic (the
	// KC event-sync that replaced the old sign-in identity hook).
	// Runs on the pivox pool; shuts down when ctx cancels (it polls with
	// ctx and closes its kgo client on return).
	brokers := strings.Split(kafkaBrokers, ",")
	identityConsumer, err := identitysync.NewConsumer(identitysync.ConsumerConfig{
		Brokers: brokers,
		Handler: identitysync.NewHandler(identitysync.HandlerConfig{
			Queries: queries,
			Realm:   kcRealm,
			Logger:  logger,
		}),
		Logger: logger,
	})
	if err != nil {
		return fmt.Errorf("identity-sync consumer: %w", err)
	}
	identitySyncDone := make(chan struct{})
	go func() {
		defer close(identitySyncDone)
		if err := identityConsumer.Run(ctx); err != nil {
			logger.Error("identity-sync consumer exited with error", "error", err)
		}
	}()
	logger.Info("identity-sync consumer running", "brokers", brokers, "realm", kcRealm, "topic", "keycloak-events")

	// Block until signal. River's Start goroutine runs until the
	// context cancels or Stop is called.
	<-ctx.Done()
	logger.Info("shutting down...")

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer stopCancel()
	if err := client.Stop(stopCtx); err != nil {
		logger.Warn("river stop returned error", "error", err)
	}

	// Wait for the identity-sync consumer to drain its current poll and
	// close its kgo client (ctx is already cancelled, so Run returns).
	select {
	case <-identitySyncDone:
	case <-stopCtx.Done():
		logger.Warn("identity-sync consumer did not stop within shutdown deadline")
	}

	logger.Info("pivox-worker stopped")
	return nil
}
