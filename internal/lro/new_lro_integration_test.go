//go:build dev

package lro

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/dashkan/pivox/internal/testutil"
)

// testJobArgs is a minimal river.JobArgs for integration tests. We
// don't register a worker for this kind — the test only enqueues
// (the river_job row gets created); execution would require
// pivox-worker, which is out of scope for these unit-of-work tests.
type testJobArgs struct {
	Resource string `json:"resource"`
}

func (testJobArgs) Kind() string { return "test_lro_enqueue" }

// TestNewLro_AtomicallyInsertsOperationAndJob covers the core
// contract: a successful NewLro produces both an operations row and
// a river_job row, in the same transaction. Visible to both queries
// after the call returns.
func TestNewLro_AtomicallyInsertsOperationAndJob(t *testing.T) {
	pool, queries, cleanup := testutil.SetupTestDB(t)
	defer cleanup()
	ctx := context.Background()

	// River's tables live in the `river` schema. SetupTestDB only
	// runs our migrations; ensure the river schema + tables exist
	// for this test by running rivermigrate the same way pivox-worker
	// would on boot.
	driver := riverpgxv5.New(pool)
	_, err := pool.Exec(ctx, "CREATE SCHEMA IF NOT EXISTS river")
	require.NoError(t, err)
	migrator, err := rivermigrate.New(driver, &rivermigrate.Config{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Schema: "river",
	})
	require.NoError(t, err)
	_, err = migrator.Migrate(ctx, rivermigrate.DirectionUp, nil)
	require.NoError(t, err)

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	riverClient, err := river.NewClient(driver, &river.Config{
		Logger: logger,
		Schema: "river",
	})
	require.NoError(t, err)

	m := NewManager(ManagerConfig{
		Queries: queries,
		Logger:  logger,
		Pool:    pool,
		River:   riverClient,
	})

	parent := "organizations/acme/spaces/dev"
	meta, err := structpb.NewStruct(map[string]any{"phase": "VALIDATING"})
	require.NoError(t, err)

	op, err := m.NewLro(ctx, parent, NewLroOpts{
		JobArgs:  testJobArgs{Resource: parent},
		Metadata: meta,
	})
	require.NoError(t, err)
	require.NotNil(t, op)
	assert.Contains(t, op.Name, parent+"/operations/")
	assert.False(t, op.Done)

	// Operations row must exist after commit.
	opID, err := parseOperationName(op.Name)
	require.NoError(t, err)
	dbOp, err := queries.GetOperation(ctx, opID)
	require.NoError(t, err, "operations row missing — tx didn't commit?")
	assert.Equal(t, parent, dbOp.Parent)
	assert.False(t, dbOp.Done)
	assert.NotNil(t, dbOp.Metadata)

	// River job row must also exist after commit, with kind matching
	// JobArgs.Kind().
	var riverJobCount int
	err = pool.QueryRow(ctx,
		`SELECT count(*) FROM river.river_job WHERE kind = $1`,
		testJobArgs{}.Kind(),
	).Scan(&riverJobCount)
	require.NoError(t, err)
	assert.Equal(t, 1, riverJobCount, "river_job row missing — InsertTx didn't commit?")
}

// TestNewLro_RollsBackOnRiverFailure verifies atomicity from the
// failure side: if River.InsertTx returns an error, the operations
// row must NOT be visible after the call. (We can't easily induce a
// real River failure mid-tx without monkey-patching, so this test
// instead asserts the rollback path by injecting a context that's
// cancelled before InsertTx — pgx returns ctx.Err() and the deferred
// Rollback fires.)
func TestNewLro_RollsBackOnTxFailure(t *testing.T) {
	pool, queries, cleanup := testutil.SetupTestDB(t)
	defer cleanup()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	driver := riverpgxv5.New(pool)
	_, err := pool.Exec(context.Background(), "CREATE SCHEMA IF NOT EXISTS river")
	require.NoError(t, err)
	migrator, err := rivermigrate.New(driver, &rivermigrate.Config{
		Logger: logger,
		Schema: "river",
	})
	require.NoError(t, err)
	_, err = migrator.Migrate(context.Background(), rivermigrate.DirectionUp, nil)
	require.NoError(t, err)

	riverClient, err := river.NewClient(driver, &river.Config{
		Logger: logger,
		Schema: "river",
	})
	require.NoError(t, err)

	m := NewManager(ManagerConfig{
		Queries: queries,
		Logger:  logger,
		Pool:    pool,
		River:   riverClient,
	})

	cancelledCtx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = m.NewLro(cancelledCtx, "organizations/acme", NewLroOpts{
		JobArgs: testJobArgs{Resource: "organizations/acme"},
	})
	require.Error(t, err)

	// No operations row should have leaked through.
	var opCount int
	err = pool.QueryRow(context.Background(),
		`SELECT count(*) FROM operations WHERE parent = $1`,
		"organizations/acme",
	).Scan(&opCount)
	require.NoError(t, err)
	assert.Equal(t, 0, opCount, "operations row leaked through a failed tx — atomicity broken")

	// And no river_job row either.
	var jobCount int
	err = pool.QueryRow(context.Background(),
		`SELECT count(*) FROM river.river_job WHERE kind = $1`,
		testJobArgs{}.Kind(),
	).Scan(&jobCount)
	require.NoError(t, err)
	assert.Equal(t, 0, jobCount, "river_job row leaked through a failed tx — atomicity broken")

}

// TestNewLro_RequiresPoolAndRiver pins the call-time validation: a
// Manager constructed without Pool or River errors at NewLro call
// time (rather than panicking at construct). Allows the legacy
// CreateAndRun path to keep working in test wiring that hasn't
// migrated.
func TestNewLro_RequiresPoolAndRiver(t *testing.T) {
	pool, queries, cleanup := testutil.SetupTestDB(t)
	defer cleanup()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	// No Pool, no River.
	m := NewManager(ManagerConfig{Queries: queries, Logger: logger})
	_, err := m.NewLro(context.Background(), "organizations/acme", NewLroOpts{
		JobArgs: testJobArgs{},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Pool")

	// Pool set, River nil.
	m = NewManager(ManagerConfig{Queries: queries, Logger: logger, Pool: pool})
	_, err = m.NewLro(context.Background(), "organizations/acme", NewLroOpts{
		JobArgs: testJobArgs{},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "River")
}
