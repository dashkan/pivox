package health

import (
	"context"
	"log/slog"
	"time"

	"google.golang.org/grpc"
	grpchealth "google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
)

// defaultRefreshInterval is how often the serving status is recomputed. The gRPC
// health protocol is push-based (Watch), unlike /readyz, so it needs a timer.
const defaultRefreshInterval = 5 * time.Second

// GRPCService adapts a State to grpc.health.v1, for k8s gRPC probes and L7
// proxies. Driven by the SAME State as /readyz so the two cannot disagree.
//
// The protocol has no liveness/readiness split — one status per service name — so
// the empty name ("" = overall) maps to READINESS. A server that answers Check at
// all is alive.
//
// Registered on the API only: the Worker runs no gRPC server and the Agent is a
// gRPC *client*, so standing one up purely for health would add surface for no
// consumer.
type GRPCService struct {
	state  *State
	srv    *grpchealth.Server
	logger *slog.Logger
}

// NewGRPCService returns a health service backed by state. Starts NOT_SERVING —
// fail closed until readiness is established.
func NewGRPCService(state *State) *GRPCService {
	if state == nil {
		panic("health: NewGRPCService requires a State")
	}
	srv := grpchealth.NewServer()
	srv.SetServingStatus("", healthpb.HealthCheckResponse_NOT_SERVING)
	return &GRPCService{state: state, srv: srv, logger: slog.Default()}
}

// Server exposes the underlying grpc-go health server (Check + Watch).
func (g *GRPCService) Server() *grpchealth.Server {
	return g.srv
}

// Register wires the health service onto a gRPC server.
//
// grpc.health.v1 is intentionally UNAUTHENTICATED (GatedUnaryInterceptor gates
// auth to pivox.* + LRO): probers hold no credentials, and it discloses only
// SERVING/NOT_SERVING. Unlike reflection, which exposes the API surface.
func (g *GRPCService) Register(s *grpc.Server) {
	healthpb.RegisterHealthServer(s, g.srv)
}

// Refresh recomputes the serving status once. Exported so tests can drive it
// deterministically rather than wait on the ticker.
func (g *GRPCService) Refresh(ctx context.Context) {
	st := healthpb.HealthCheckResponse_SERVING
	if err := g.state.Ready(ctx); err != nil {
		st = healthpb.HealthCheckResponse_NOT_SERVING
	}
	g.srv.SetServingStatus("", st)
}

// Run refreshes the serving status until ctx is cancelled. Blocks; run it in a
// goroutine.
func (g *GRPCService) Run(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = defaultRefreshInterval
	}
	t := time.NewTicker(interval)
	defer t.Stop()

	g.Refresh(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			g.Refresh(ctx)
		}
	}
}

// Shutdown flips every service to NOT_SERVING. Call at the START of graceful
// shutdown, before the server stops accepting, so load balancers drain first.
func (g *GRPCService) Shutdown() {
	g.srv.Shutdown()
}
