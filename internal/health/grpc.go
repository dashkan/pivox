package health

import (
	"context"
	"log/slog"
	"time"

	"google.golang.org/grpc"
	grpchealth "google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
)

// defaultRefreshInterval is how often the gRPC serving status is recomputed from
// State.
//
// The gRPC health protocol is PUSH-based (Watch streams a status), unlike
// /readyz which is PULL-based (recomputed per request), so the status has to be
// refreshed on a timer. 5s is fast enough for a load balancer to stop routing to
// a broken instance well inside a typical outlier-detection window, and slow
// enough that the readiness checks (a DB ping each) are not a load source of
// their own.
const defaultRefreshInterval = 5 * time.Second

// GRPCService adapts a State to the standard gRPC health protocol
// (grpc.health.v1), so Kubernetes gRPC probes and L7 proxies (agentgateway,
// Envoy) can see the same readiness the HTTP /readyz endpoint reports.
//
// It is deliberately driven by the SAME State as the HTTP handlers: two
// independent readiness computations would drift, and "HTTP says ready, gRPC
// says NOT_SERVING" is a genuinely confusing failure to debug.
//
// The gRPC health protocol has no liveness/readiness split — it is one status
// per service name. We map the empty service name ("" — the conventional
// "overall server status") to READINESS. Liveness needs no mapping: a server
// that answers Check at all is alive.
//
// Registered on the API's gRPC server only. The Worker and Storage Agent run no
// gRPC server (the agent is a gRPC *client* — it dials the cloud), so standing
// one up purely to serve health would add a listener and attack surface for no
// consumer. They expose HTTP /healthz + /readyz, which every prober understands.
type GRPCService struct {
	state  *State
	srv    *grpchealth.Server
	logger *slog.Logger
}

// NewGRPCService returns a health service backed by state. It starts
// NOT_SERVING: fail closed until readiness has actually been established, the
// same rule /readyz follows.
func NewGRPCService(state *State) *GRPCService {
	if state == nil {
		panic("health: NewGRPCService requires a State")
	}
	srv := grpchealth.NewServer()
	srv.SetServingStatus("", healthpb.HealthCheckResponse_NOT_SERVING)
	return &GRPCService{state: state, srv: srv, logger: slog.Default()}
}

// Server exposes the underlying grpc-go health server (it implements Check and
// the Watch stream).
func (g *GRPCService) Server() *grpchealth.Server {
	return g.srv
}

// Register wires the health service onto a gRPC server.
//
// NOTE: grpc.health.v1 is intentionally UNAUTHENTICATED — the auth interceptor
// is gated to pivox.* + LRO methods (see server.GatedUnaryInterceptor), so
// health bypasses it. That is the standard posture for a health protocol whose
// whole job is to answer probers that hold no credentials, and it discloses only
// SERVING/NOT_SERVING. (Contrast reflection, which is off by default in prod
// because it exposes the entire API surface.)
func (g *GRPCService) Register(s *grpc.Server) {
	healthpb.RegisterHealthServer(s, g.srv)
}

// Refresh recomputes the serving status from State once. Exported so callers
// (and tests) can drive it deterministically instead of waiting on the ticker.
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

// Shutdown flips every service to NOT_SERVING.
//
// Call this at the START of graceful shutdown, BEFORE the gRPC server stops
// accepting: it gives load balancers and Watch subscribers a chance to stop
// routing to this instance while it can still finish in-flight requests.
// Skipping it means the LB keeps sending traffic into a server that is already
// closing, and those requests fail for no reason.
func (g *GRPCService) Shutdown() {
	g.srv.Shutdown()
}
