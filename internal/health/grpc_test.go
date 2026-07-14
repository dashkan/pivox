package health

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
)

func status(t *testing.T, g *GRPCService) healthpb.HealthCheckResponse_ServingStatus {
	t.Helper()
	resp, err := g.Server().Check(context.Background(), &healthpb.HealthCheckRequest{})
	require.NoError(t, err)
	return resp.GetStatus()
}

func TestGRPCService(t *testing.T) {
	t.Parallel()

	t.Run("is NOT_SERVING while starting", func(t *testing.T) {
		t.Parallel()
		// Same fail-closed rule as /readyz: before checks are installed, we must
		// not tell a load balancer we can take traffic.
		g := NewGRPCService(NewState())

		assert.Equal(t, healthpb.HealthCheckResponse_NOT_SERVING, status(t, g))
	})

	t.Run("becomes SERVING once readiness passes", func(t *testing.T) {
		t.Parallel()
		state := NewState()
		g := NewGRPCService(state)

		state.SetChecks(Check{Name: "pivox-db", Func: func(context.Context) error { return nil }})
		g.Refresh(context.Background())

		assert.Equal(t, healthpb.HealthCheckResponse_SERVING, status(t, g))
	})

	t.Run("tracks the SAME State as /readyz, so the two cannot disagree", func(t *testing.T) {
		t.Parallel()
		var down atomic.Bool
		down.Store(true)

		state := NewState()
		state.SetChecks(Check{Name: "pivox-db", Func: func(context.Context) error {
			if down.Load() {
				return errors.New("refused")
			}
			return nil
		}})
		g := NewGRPCService(state)

		g.Refresh(context.Background())
		require.Equal(t, healthpb.HealthCheckResponse_NOT_SERVING, status(t, g))
		require.Error(t, state.Ready(context.Background()), "HTTP readiness agrees")

		down.Store(false)
		g.Refresh(context.Background())

		assert.Equal(t, healthpb.HealthCheckResponse_SERVING, status(t, g))
		assert.NoError(t, state.Ready(context.Background()), "HTTP readiness agrees")
	})

	t.Run("goes NOT_SERVING on shutdown so load balancers drain", func(t *testing.T) {
		t.Parallel()
		state := NewState()
		state.SetChecks()
		g := NewGRPCService(state)
		g.Refresh(context.Background())
		require.Equal(t, healthpb.HealthCheckResponse_SERVING, status(t, g))

		// Flip to NOT_SERVING BEFORE the gRPC server stops accepting, so upstreams
		// stop routing to us while we can still finish in-flight work. Without
		// this, the LB keeps sending requests into a closing server.
		g.Shutdown()

		resp, err := g.Server().Check(context.Background(), &healthpb.HealthCheckRequest{})
		require.NoError(t, err)
		assert.Equal(t, healthpb.HealthCheckResponse_NOT_SERVING, resp.GetStatus())
	})
}
