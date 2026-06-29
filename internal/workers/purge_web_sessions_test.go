package workers

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/riverqueue/river"
	"github.com/stretchr/testify/require"

	"github.com/dashkan/pivox/internal/testutil"
)

// TestPurgeWebSessionsWorker exercises the real DELETE against a real Postgres:
// a row past its horizon is reclaimed, a still-live row is left untouched.
//
// In production `web_sessions` lives in the BFF-owned `sessions` database, which
// has NO Go migrations (the BFF creates the schema itself). The test pool points
// at the app test DB, where that table therefore does not exist — so the test
// creates it here to mirror the BFF-owned schema before running the worker.
func TestPurgeWebSessionsWorker(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pool, _ := testutil.SetupTestDB(t)

	w := &PurgeWebSessionsWorker{
		Pool:   pool,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	// Cold-start tolerance: before the BFF has created web_sessions, the purge
	// tick must no-op (treat the missing table as "nothing to GC"), not error.
	require.NoError(t, w.Work(ctx, &river.Job[PurgeWebSessionsArgs]{}),
		"purge must no-op when web_sessions does not exist yet")

	_, err := pool.Exec(ctx, `
		CREATE TABLE web_sessions (
			id          TEXT PRIMARY KEY,
			tokens      JSONB NOT NULL,
			sub         TEXT NOT NULL,
			created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
			expires_at  TIMESTAMPTZ NOT NULL
		)
	`)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `
		INSERT INTO web_sessions (id, tokens, sub, expires_at) VALUES
		  ('expired', '{}'::jsonb, 'sub-a', now() - interval '1 hour'),
		  ('live',    '{}'::jsonb, 'sub-b', now() + interval '1 hour')
	`)
	require.NoError(t, err)

	require.NoError(t, w.Work(ctx, &river.Job[PurgeWebSessionsArgs]{}))

	rows, err := pool.Query(ctx, `SELECT id FROM web_sessions ORDER BY id`)
	require.NoError(t, err)
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		require.NoError(t, rows.Scan(&id))
		ids = append(ids, id)
	}
	require.NoError(t, rows.Err())

	require.Equal(t, []string{"live"}, ids, "only the live session should remain")
}
