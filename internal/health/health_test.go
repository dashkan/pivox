package health

import (
	"context"
	"errors"
	"io"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func get(t *testing.T, url string) (int, string) {
	t.Helper()
	resp, err := http.Get(url) //nolint:noctx // test-local request against our own listener
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, string(body)
}

// startServer binds a health server on an ephemeral port and returns its base URL.
func startServer(t *testing.T, state *State) string {
	t.Helper()
	srv := NewServer(Config{Addr: "127.0.0.1:0", State: state})
	require.NoError(t, srv.Start())
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	})
	return "http://" + srv.Addr()
}

func TestLiveness(t *testing.T) {
	t.Parallel()

	t.Run("is 200 before the process is ready", func(t *testing.T) {
		t.Parallel()
		// The whole point of binding the debug server EARLY: liveness answers
		// while dependencies are still coming up.
		base := startServer(t, NewState())

		code, body := get(t, base+"/healthz")

		assert.Equal(t, http.StatusOK, code)
		assert.Contains(t, body, "ok")
	})

	t.Run("stays 200 when a readiness check is failing", func(t *testing.T) {
		t.Parallel()
		// LOAD-BEARING. If liveness depended on Postgres, one DB blip would fail
		// liveness on every replica at once and the orchestrator would restart the
		// entire fleet — amplifying a brief dependency hiccup into a full outage.
		// Liveness answers "am I alive", never "can my dependencies be reached".
		state := NewState()
		state.SetChecks(Check{
			Name: "pivox-db",
			Func: func(context.Context) error { return errors.New("connection refused") },
		})
		base := startServer(t, state)

		code, _ := get(t, base+"/healthz")
		assert.Equal(t, http.StatusOK, code, "liveness must not depend on readiness")

		readyCode, _ := get(t, base+"/readyz")
		assert.Equal(t, http.StatusServiceUnavailable, readyCode, "but readiness must fail")
	})
}

func TestReadiness(t *testing.T) {
	t.Parallel()

	t.Run("is not ready until checks are installed", func(t *testing.T) {
		t.Parallel()
		// Fail closed while starting: a fresh State has not been told what "ready"
		// means yet, so it must not claim to be ready.
		base := startServer(t, NewState())

		code, body := get(t, base+"/readyz")

		assert.Equal(t, http.StatusServiceUnavailable, code)
		assert.Contains(t, body, "starting", "should say WHY it isn't ready")
	})

	t.Run("is ready when every check passes", func(t *testing.T) {
		t.Parallel()
		state := NewState()
		state.SetChecks(
			Check{Name: "pivox-db", Func: func(context.Context) error { return nil }},
			Check{Name: "sessions-db", Func: func(context.Context) error { return nil }},
		)
		base := startServer(t, state)

		code, body := get(t, base+"/readyz")

		assert.Equal(t, http.StatusOK, code)
		assert.Contains(t, body, "ready")
	})

	t.Run("names the failing dependency but never leaks its error text", func(t *testing.T) {
		t.Parallel()
		// "not ready" with no reason is what made the original incident opaque, so
		// the body must say WHICH dependency is down.
		//
		// But it must say ONLY that. /readyz is routed through the PUBLIC ingress
		// (configs/agentgateway.yaml), so this body is internet-reachable. A
		// dependency's raw error is not safe to echo: pgconn's connect error
		// embeds the DSN — database user, database name, internal host, port. That
		// would be disclosed to unauthenticated callers at exactly the moment an
		// incident is underway. Names in, payloads out.
		state := NewState()
		state.SetChecks(
			Check{Name: "pivox-db", Func: func(context.Context) error {
				return errors.New(`failed to connect to "user=pivox database=pivox": 10.1.2.3:5432 (db.internal): connection refused`)
			}},
			Check{Name: "kafka", Func: func(context.Context) error { return nil }},
		)
		base := startServer(t, state)

		code, body := get(t, base+"/readyz")

		assert.Equal(t, http.StatusServiceUnavailable, code)
		assert.Contains(t, body, "pivox-db", "must identify WHICH dependency is down")
		assert.NotContains(t, body, "kafka", "should not blame a healthy dependency")

		for _, secret := range []string{"user=pivox", "10.1.2.3", "db.internal", "connection refused"} {
			assert.NotContains(t, body, secret,
				"the dependency's error text must not reach a public response body")
		}
	})

	t.Run("is ready with no checks once explicitly marked", func(t *testing.T) {
		t.Parallel()
		// The agent has no Postgres: "ready" for it means "serving", full stop.
		// SetChecks() with none is an explicit statement, distinct from a fresh
		// (never-configured) State.
		state := NewState()
		state.SetChecks()
		base := startServer(t, state)

		code, _ := get(t, base+"/readyz")

		assert.Equal(t, http.StatusOK, code)
	})

	t.Run("reflects a dependency that recovers", func(t *testing.T) {
		t.Parallel()
		var down bool
		var mu sync.Mutex
		mu.Lock()
		down = true
		mu.Unlock()

		state := NewState()
		state.SetChecks(Check{Name: "pivox-db", Func: func(context.Context) error {
			mu.Lock()
			defer mu.Unlock()
			if down {
				return errors.New("refused")
			}
			return nil
		}})
		base := startServer(t, state)

		code, _ := get(t, base+"/readyz")
		require.Equal(t, http.StatusServiceUnavailable, code)

		mu.Lock()
		down = false
		mu.Unlock()

		code, _ = get(t, base+"/readyz")
		assert.Equal(t, http.StatusOK, code, "readiness is re-evaluated per request, not cached")
	})
}

func TestState_Ready(t *testing.T) {
	t.Parallel()

	t.Run("reports not-ready before configuration", func(t *testing.T) {
		t.Parallel()
		err := NewState().Ready(context.Background())
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrStarting)
	})

	t.Run("aggregates every failing check, not just the first", func(t *testing.T) {
		t.Parallel()
		state := NewState()
		state.SetChecks(
			Check{Name: "a", Func: func(context.Context) error { return errors.New("boom-a") }},
			Check{Name: "b", Func: func(context.Context) error { return nil }},
			Check{Name: "c", Func: func(context.Context) error { return errors.New("boom-c") }},
		)

		err := state.Ready(context.Background())

		require.Error(t, err)
		assert.ErrorContains(t, err, "a")
		assert.ErrorContains(t, err, "c")
	})
}

func TestNewServer_RequiresConfig(t *testing.T) {
	t.Parallel()
	assert.Panics(t, func() { NewServer(Config{State: NewState()}) }, "Addr is required")
	assert.Panics(t, func() { NewServer(Config{Addr: "127.0.0.1:0"}) }, "State is required")
}

func TestStart_FailsOnPortConflict(t *testing.T) {
	t.Parallel()
	// Bind must be synchronous so a port clash is a loud startup error, not a
	// silently-absent health endpoint.
	first := NewServer(Config{Addr: "127.0.0.1:0", State: NewState()})
	require.NoError(t, first.Start())
	t.Cleanup(func() { _ = first.Shutdown(context.Background()) })

	second := NewServer(Config{Addr: first.Addr(), State: NewState()})
	assert.Error(t, second.Start())
}
