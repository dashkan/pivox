---
title: gRPC Health Service
impact: MEDIUM
impactDescription: primary health mechanism for K8s probes and load balancers
tags: health, gRPC, Kubernetes, probes, liveness, readiness
---

## gRPC Health Service

> Read this when: wiring up health checks, configuring K8s probes, or adding dependency health monitoring.

### Primary Health Mechanism

Use the standard `grpc.health.v1.Health` service as the **primary** health mechanism. Kubernetes gRPC liveness/readiness probes and Envoy load balancers consume this natively.

```go
import (
    "google.golang.org/grpc/health"
    healthgrpc "google.golang.org/grpc/health/grpc_health_v1"
    healthpb "google.golang.org/grpc/health/grpc_health_v1"
)

healthcheck := health.NewServer()
healthgrpc.RegisterHealthServer(grpcServer, healthcheck)

// Background goroutine checks dependencies and sets status
go func() {
    for {
        if err := db.Ping(ctx); err != nil {
            healthcheck.SetServingStatus("", healthpb.HealthCheckResponse_NOT_SERVING)
        } else {
            healthcheck.SetServingStatus("", healthpb.HealthCheckResponse_SERVING)
        }
        time.Sleep(10 * time.Second)
    }
}()
```

### What to Check

The background goroutine should verify critical dependencies:

- **PostgreSQL** — `db.Ping(ctx)` on the connection pool
- **External services** — any upstream gRPC/HTTP services this service depends on (if applicable)

Do NOT check non-critical dependencies (caches, optional feature flags). A health check failure should mean "this instance cannot serve requests."

### HTTP Health Wrapper (Debug Port)

On the `:9090` debug port, serve a minimal `GET /healthz` that queries the gRPC health server internally. This is for basic infra that doesn't speak gRPC:

```go
func healthHandler(w http.ResponseWriter, r *http.Request) {
    resp, err := healthClient.Check(r.Context(), &healthpb.HealthCheckRequest{})
    if err != nil || resp.Status != healthpb.HealthCheckResponse_SERVING {
        w.WriteHeader(http.StatusServiceUnavailable)
        fmt.Fprintln(w, "not serving")
        return
    }
    w.WriteHeader(http.StatusOK)
    fmt.Fprintln(w, "ok")
}
```

### Graceful Shutdown

During shutdown, set status to `NOT_SERVING` before draining:

```go
healthcheck.SetServingStatus("", healthpb.HealthCheckResponse_NOT_SERVING)
// Then gracefully stop the gRPC server
grpcServer.GracefulStop()
```

This lets K8s probes detect the instance is draining and stop routing traffic before the server actually stops.
