package health

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFlag(t *testing.T) {
	t.Parallel()

	t.Run("starts not-ready and reports why", func(t *testing.T) {
		t.Parallel()
		// Fail closed: a link that has never come up must not read as ready.
		f := NewFlag("cloud-stream", "not connected")

		err := f.Check().Func(context.Background())

		require.Error(t, err)
		assert.ErrorContains(t, err, "not connected")
	})

	t.Run("passes once set", func(t *testing.T) {
		t.Parallel()
		f := NewFlag("cloud-stream", "not connected")

		f.Set(true)

		assert.NoError(t, f.Check().Func(context.Background()))
	})

	t.Run("goes not-ready again when cleared", func(t *testing.T) {
		t.Parallel()
		// A dropped stream must take the process out of the ready set — it can no
		// longer receive new sessions.
		f := NewFlag("cloud-stream", "not connected")
		f.Set(true)
		require.NoError(t, f.Check().Func(context.Background()))

		f.Set(false)

		assert.Error(t, f.Check().Func(context.Background()))
	})

	t.Run("drives readiness but never liveness", func(t *testing.T) {
		t.Parallel()
		// THE GUARD THAT MATTERS. If the cloud link fed liveness, a cloud outage
		// would fail liveness on every agent at once and the orchestrator would
		// restart the entire fleet — amplifying someone else's outage into ours.
		// Readiness pulls a disconnected agent out of rotation; it never restarts it.
		f := NewFlag("cloud-stream", "not connected")
		state := NewState()
		state.SetChecks(f.Check())
		base := startServer(t, state)

		code, _ := get(t, base+"/readyz")
		assert.Equal(t, http.StatusServiceUnavailable, code)

		liveCode, _ := get(t, base+"/healthz")
		assert.Equal(t, http.StatusOK, liveCode, "liveness must ignore the cloud link")
	})
}
