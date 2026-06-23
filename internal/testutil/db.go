package testutil

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"

	db "github.com/dashkan/pivox/internal/db/generated"
)

// Test Postgres lives in docker-compose.test.yml. `make test-up`
// starts it; tests connect to a fixed port. Per-test isolation is
// via the Postgres `CREATE DATABASE ... TEMPLATE` pattern: a
// template database with all migrations applied is built once per
// test process; each SetupTestDB call clones the template into a
// fresh per-test database (~50ms per clone vs ~1.5s per
// container).
//
// Why template-clone instead of per-test schema or per-test tx
// rollback:
//   - Schema-per-test would require migrations to target a schema
//     other than `public`, which they don't (they reference
//     unqualified table names). Rewriting every migration is out of
//     scope.
//   - Tx-rollback breaks the moment the system-under-test calls
//     RunInTx itself; the inner tx commits relative to the outer,
//     and rolling back the outer aborts the inner mid-statement.
//     Production handlers nest tx liberally, so rollback isolation
//     would be a constant footgun.
//   - Template-clone gives each test a real, independent database;
//     handlers commit normally, no surprises.

const (
	// composeAdminDSN connects to the docker-compose Postgres's
	// default `pivox` database (created by POSTGRES_DB env). Used
	// for CREATE/DROP DATABASE administration.
	composeAdminDSN = "postgres://pivox:pivox@localhost:55432/pivox?sslmode=disable"
	// templateDBName is the name of the per-process template
	// database. Built once with all migrations applied; cloned
	// per test. The "_template" suffix is a Postgres convention.
	templateDBName = "pivox_test_template"
)

var (
	// templateOnce guards the one-time template build.
	templateOnce    sync.Once
	templateInitErr error
)

// SetupTestDB returns a pool + queries pointing at a fresh per-test
// database cloned from the package-shared template. The database is
// dropped via t.Cleanup, so callers don't manage cleanup. Each call
// gets a unique DB so concurrent subtests don't see each other's
// writes.
func SetupTestDB(t *testing.T) (pool *pgxpool.Pool, queries *db.Queries) {
	t.Helper()
	ctx := context.Background()

	templateOnce.Do(initTemplateDB)
	if templateInitErr != nil {
		t.Fatalf("test database template init failed: %v", templateInitErr)
	}

	dbName := perTestDBName(t)
	if err := cloneTemplateInto(ctx, dbName); err != nil {
		t.Fatalf("clone template -> %s: %v", dbName, err)
	}

	pool, err := openPerTestPool(ctx, dbName)
	if err != nil {
		_ = dropDatabase(context.Background(), dbName)
		t.Fatalf("open per-test pool: %v", err)
	}

	t.Cleanup(func() {
		pool.Close()
		_ = dropDatabase(context.Background(), dbName)
	})
	return pool, db.New(pool)
}

// initTemplateDB runs once per test process. Cross-process
// coordination is via a Postgres advisory lock: the first process
// to acquire it drops + recreates the template and applies
// migrations; concurrent processes wait on the lock and then see
// the populated template, skipping re-init.
//
// The lock-and-marker pattern avoids the race that was filed as
// the original cross-package failure: multiple `go test ./...`
// packages all running their own initTemplateDB simultaneously
// hit `pg_database_datname_index` constraint violations on
// CREATE DATABASE, plus apply migrations into a partially-built
// template. The marker table tells late arrivals "template is
// ready, don't re-build it."
func initTemplateDB() {
	ctx := context.Background()

	// Acquire an exclusive advisory lock keyed to a hash of the
	// template name. Held until releaseTemplateLock; serializes
	// the drop/create/migrate window across every test process
	// running against this Postgres.
	lockConn, err := pgx.Connect(ctx, composeAdminDSN)
	if err != nil {
		templateInitErr = fmt.Errorf("admin connect for lock: %w", err)
		return
	}
	defer func() { _ = lockConn.Close(ctx) }()
	if _, err := lockConn.Exec(ctx, `SELECT pg_advisory_lock(hashtext($1))`, templateDBName); err != nil {
		templateInitErr = fmt.Errorf("acquire template lock: %w", err)
		return
	}
	defer func() {
		_, _ = lockConn.Exec(ctx, `SELECT pg_advisory_unlock(hashtext($1))`, templateDBName)
	}()

	// If another process already populated the template, skip.
	// Existence-with-marker check: the template DB exists AND has
	// our `__pivox_test_template_ready` marker table inside it.
	// Bare existence isn't enough — a half-built template (process
	// crashed mid-migration) would also "exist."
	if ready, err := templateIsReady(ctx); err != nil {
		templateInitErr = fmt.Errorf("check template ready: %w", err)
		return
	} else if ready {
		return
	}

	// Drop stale template (if any). Idempotent.
	if err := dropDatabase(ctx, templateDBName); err != nil {
		templateInitErr = fmt.Errorf("drop stale template: %w", err)
		return
	}
	if err := createDatabase(ctx, templateDBName); err != nil {
		templateInitErr = fmt.Errorf("create template: %w", err)
		return
	}

	// Apply migrations to the template using a one-shot pool. No
	// pgvector type registration here — extensions are created by
	// the migrations themselves; the per-test pool registers types
	// after.
	migrationDSN := dsnFor(templateDBName)
	migPool, err := pgxpool.New(ctx, migrationDSN)
	if err != nil {
		templateInitErr = fmt.Errorf("connect to template for migration: %w", err)
		return
	}
	defer migPool.Close()

	if err := runMigrations(ctx, migPool); err != nil {
		templateInitErr = fmt.Errorf("apply pivox migrations: %w", err)
		return
	}

	if err := runRiverMigrations(ctx, migPool); err != nil {
		templateInitErr = fmt.Errorf("apply river migrations: %w", err)
		return
	}

	// Mark the template as ready. Other processes use the marker
	// table to distinguish a fully-populated template from a
	// partially-built one.
	if _, err := migPool.Exec(ctx, `CREATE TABLE __pivox_test_template_ready (created_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		templateInitErr = fmt.Errorf("create ready marker: %w", err)
		return
	}
}

// templateIsReady returns true when the template database exists
// AND contains the `__pivox_test_template_ready` marker table —
// i.e., a previous initTemplateDB completed all migrations
// successfully. A bare-existence check would falsely return true
// for a half-built template left behind by a crashed process.
func templateIsReady(ctx context.Context) (bool, error) {
	conn, err := pgx.Connect(ctx, composeAdminDSN)
	if err != nil {
		return false, fmt.Errorf("admin connect: %w", err)
	}
	defer func() { _ = conn.Close(ctx) }()

	var exists bool
	if err := conn.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_database WHERE datname = $1)`, templateDBName).Scan(&exists); err != nil {
		return false, fmt.Errorf("check db existence: %w", err)
	}
	if !exists {
		return false, nil
	}

	// Connect to the template itself to check the marker.
	tmplConn, err := pgx.Connect(ctx, dsnFor(templateDBName))
	if err != nil {
		return false, fmt.Errorf("template connect: %w", err)
	}
	defer func() { _ = tmplConn.Close(ctx) }()

	var hasMarker bool
	if err := tmplConn.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_tables WHERE schemaname = 'public' AND tablename = '__pivox_test_template_ready')`).Scan(&hasMarker); err != nil {
		return false, fmt.Errorf("check marker: %w", err)
	}
	return hasMarker, nil
}

// cloneTemplateInto runs CREATE DATABASE <name> WITH TEMPLATE =
// pivox_test_template. Postgres requires no other connections to
// the template, which is true here because initTemplateDB closed
// its pool before this runs.
func cloneTemplateInto(ctx context.Context, dbName string) error {
	conn, err := pgx.Connect(ctx, composeAdminDSN)
	if err != nil {
		return fmt.Errorf("admin connect: %w", err)
	}
	defer func() { _ = conn.Close(ctx) }()

	// Database identifiers can't be parameterized; the names are
	// generated from a fixed alphabet (letters/digits) so they're
	// safe to interpolate. Sanity-check anyway.
	if !validIdent(dbName) {
		return fmt.Errorf("unsafe db name %q", dbName)
	}
	if _, err := conn.Exec(ctx, fmt.Sprintf(`CREATE DATABASE %q WITH TEMPLATE = %q`, dbName, templateDBName)); err != nil {
		return fmt.Errorf("clone template: %w", err)
	}
	return nil
}

// openPerTestPool builds a pool against the per-test database with
// pgvector type registration. search_path includes the `river`
// schema so rivertest helpers that read river.river_job by short
// name resolve correctly.
func openPerTestPool(ctx context.Context, dbName string) (*pgxpool.Pool, error) {
	// Share the production pool builder (otelpgx tracer + pgvector type
	// registration) so test pools can't drift from prod; add the test-only
	// search_path (river schema for rivertest short-name lookups) via the hook.
	return db.NewPool(ctx, dsnFor(dbName), func(cfg *pgxpool.Config) {
		cfg.ConnConfig.RuntimeParams["search_path"] = "public, river"
	})
}

// dsnFor returns a DSN connecting to a specific database on the
// docker-compose Postgres.
func dsnFor(dbName string) string {
	return fmt.Sprintf("postgres://pivox:pivox@localhost:55432/%s?sslmode=disable", dbName)
}

// createDatabase runs CREATE DATABASE via the admin connection.
func createDatabase(ctx context.Context, name string) error {
	if !validIdent(name) {
		return fmt.Errorf("unsafe db name %q", name)
	}
	conn, err := pgx.Connect(ctx, composeAdminDSN)
	if err != nil {
		return fmt.Errorf("admin connect: %w", err)
	}
	defer func() { _ = conn.Close(ctx) }()
	if _, err := conn.Exec(ctx, fmt.Sprintf(`CREATE DATABASE %q`, name)); err != nil {
		return fmt.Errorf("create database %s: %w", name, err)
	}
	return nil
}

// dropDatabase runs DROP DATABASE IF EXISTS via the admin
// connection. Forces termination of any leftover connections so
// the drop succeeds even if a prior test leaked.
func dropDatabase(ctx context.Context, name string) error {
	if !validIdent(name) {
		return fmt.Errorf("unsafe db name %q", name)
	}
	conn, err := pgx.Connect(ctx, composeAdminDSN)
	if err != nil {
		return fmt.Errorf("admin connect: %w", err)
	}
	defer func() { _ = conn.Close(ctx) }()
	// Terminate any straggler backends so DROP doesn't fail on
	// "database is being accessed by other users."
	_, _ = conn.Exec(ctx, `SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = $1 AND pid <> pg_backend_pid()`, name)
	if _, err := conn.Exec(ctx, fmt.Sprintf(`DROP DATABASE IF EXISTS %q`, name)); err != nil {
		return fmt.Errorf("drop database %s: %w", name, err)
	}
	return nil
}

// perTestDBName produces a unique database name for a test based
// on a hash-equivalent of t.Name() plus random bytes. Postgres
// caps identifiers at 63 chars; we stay well under.
func perTestDBName(t *testing.T) string {
	var rnd [4]byte
	_, _ = rand.Read(rnd[:])
	suffix := hex.EncodeToString(rnd[:])
	// Sanitize t.Name() — slashes become underscores, lowercased,
	// truncated. Combined with the random suffix, collisions are
	// effectively impossible.
	base := strings.ToLower(strings.ReplaceAll(t.Name(), "/", "_"))
	base = strings.NewReplacer(" ", "_", "-", "_", ".", "_").Replace(base)
	if len(base) > 40 {
		base = base[:40]
	}
	// Strip any character that isn't lowercase ASCII / digit / underscore.
	var clean strings.Builder
	for _, r := range base {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' {
			clean.WriteRune(r)
		}
	}
	return "pivox_t_" + clean.String() + "_" + suffix
}

// validIdent guards against accidental SQL injection in the few
// places where a database name has to be interpolated (Postgres
// can't parameterize identifiers). The names produced by
// perTestDBName are already constrained to [a-z0-9_]; this guards
// against future callers slipping in something else.
func validIdent(s string) bool {
	if s == "" || len(s) > 63 {
		return false
	}
	for i, r := range s {
		ok := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_'
		if i == 0 {
			ok = ok && r != '0' && r != '1' && r != '2' && r != '3' && r != '4' && r != '5' && r != '6' && r != '7' && r != '8' && r != '9'
		}
		if !ok {
			return false
		}
	}
	return true
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
// `river` schema so River-backed paths (LRO enqueue, periodic
// jobs) have river_job/river_queue/river_leader tables ready in
// every cloned database.
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
