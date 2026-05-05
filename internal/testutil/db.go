package testutil

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	pgxvector "github.com/pgvector/pgvector-go/pgx"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	db "github.com/dashkan/pivox/internal/db/generated"
)

// SetupTestDB starts a Postgres container, runs migrations, and returns a pool.
// Call cleanup() when done (typically via t.Cleanup).
func SetupTestDB(t *testing.T) (pool *pgxpool.Pool, queries *db.Queries, cleanup func()) {
	t.Helper()
	ctx := context.Background()

	container, err := postgres.Run(ctx,
		"pgvector/pgvector:pg18",
		postgres.WithDatabase("pivox_test"),
		postgres.WithUsername("test"),
		postgres.WithPassword("test"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(30*time.Second),
		),
	)
	if err != nil {
		t.Fatalf("failed to start postgres container: %v", err)
	}

	connStr, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		_ = container.Terminate(ctx)
		t.Fatalf("failed to get connection string: %v", err)
	}

	// First pool: run migrations (pgvector extension not yet available).
	migrationPool, err := pgxpool.New(ctx, connStr)
	if err != nil {
		_ = container.Terminate(ctx)
		t.Fatalf("failed to create migration pool: %v", err)
	}

	if err := runMigrations(ctx, migrationPool); err != nil {
		migrationPool.Close()
		_ = container.Terminate(ctx)
		t.Fatalf("failed to run migrations: %v", err)
	}
	// Set default for embedding column to avoid pgvector-go NULL scan panic.
	// pgvector.Vector (non-pointer) panics on NULL; a zero-vector default sidesteps this.
	// The column is vector(768), so we use a 768-dimensional zero vector.
	if _, err := migrationPool.Exec(ctx, "ALTER TABLE assets ALTER COLUMN embedding SET DEFAULT (array_fill(0, ARRAY[768])::vector)"); err != nil {
		migrationPool.Close()
		_ = container.Terminate(ctx)
		t.Fatalf("failed to set embedding default: %v", err)
	}
	migrationPool.Close()

	// Second pool: register pgvector types now that the extension exists.
	// Use DefaultQueryExecMode = QueryExecModeDescribeExec to let pgx
	// handle NULL vector columns gracefully via the describe protocol.
	poolCfg, err := pgxpool.ParseConfig(connStr)
	if err != nil {
		_ = container.Terminate(ctx)
		t.Fatalf("failed to parse pool config: %v", err)
	}
	// search_path includes river so test queries against
	// `river.river_job` resolve via short name when needed (rivertest
	// helpers reach into the driver's default executor without
	// applying river.Config.Schema templating). Test-only — production
	// uses a dedicated river-schema'd pool for Client construction
	// (see cmd/pivox-cloud/main.go) and keeps the app pool clean.
	poolCfg.ConnConfig.RuntimeParams["search_path"] = "public, river"
	poolCfg.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		return pgxvector.RegisterTypes(ctx, conn)
	}
	pool, err = pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		_ = container.Terminate(ctx)
		t.Fatalf("failed to create pool: %v", err)
	}

	// Run River's own migrations into the `river` schema so any test
	// that exercises River-backed paths (LRO enqueue, periodic jobs,
	// integration tests via grpcharness) has the river_job /
	// river_queue / river_leader tables ready. Idempotent on
	// already-migrated DBs; cheap (~10ms) on fresh ones. The init
	// migration above creates the `river` schema; this populates it.
	if err := runRiverMigrations(ctx, pool); err != nil {
		pool.Close()
		_ = container.Terminate(ctx)
		t.Fatalf("failed to run river migrations: %v", err)
	}

	queries = db.New(pool)
	cleanup = func() {
		pool.Close()
		_ = container.Terminate(context.Background())
	}
	return pool, queries, cleanup
}

// migrationsDir returns the absolute path to the migrations directory.
func migrationsDir() string {
	_, filename, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(filename), "..", "db", "migrations")
}

// runMigrations reads all *.up.sql files from the migrations directory and
// executes them in order against the given pool.
func runMigrations(ctx context.Context, pool *pgxpool.Pool) error {
	dir := migrationsDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("read migrations dir %s: %w", dir, err)
	}

	var upFiles []string
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".sql" && len(e.Name()) > 7 && e.Name()[len(e.Name())-7:] == ".up.sql" {
			upFiles = append(upFiles, e.Name())
		}
	}
	sort.Strings(upFiles)

	for _, name := range upFiles {
		sql, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return fmt.Errorf("read migration %s: %w", name, err)
		}
		if _, err := pool.Exec(ctx, string(sql)); err != nil {
			return fmt.Errorf("execute migration %s: %w", name, err)
		}
	}
	return nil
}

// runRiverMigrations runs River's internal migrations into the
// `river` schema. Used by SetupTestDB so every integration test has
// the queue tables available without each test re-running migrations.
func runRiverMigrations(ctx context.Context, pool *pgxpool.Pool) error {
	driver := riverpgxv5.New(pool)
	migrator, err := rivermigrate.New(driver, &rivermigrate.Config{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Schema: "river",
	})
	if err != nil {
		return fmt.Errorf("river migrator: %w", err)
	}
	if _, err := migrator.Migrate(ctx, rivermigrate.DirectionUp, nil); err != nil {
		return fmt.Errorf("river migrate up: %w", err)
	}
	return nil
}
