---
title: Network Architecture — Three Ports
impact: MEDIUM
impactDescription: defines port layout and debug HTTP mux for the service
tags: ports, network, debug, swagger, mux, HTTP
---

## Network Architecture — Three Ports

> Read this when: wiring up `cmd/server/main.go`, configuring K8s manifests, or adding debug endpoints.

### Port Layout

| Port | Purpose | Exposed To |
|---|---|---|
| `:50051` | gRPC (native gRPC clients, gRPC health service) | Internal services, gRPC clients |
| `:8080` | grpc-gateway REST (JSON transcoding) | External clients, frontends, API consumers |
| `:9090` | Debug HTTP (health, metrics, pprof, Swagger UI) | Internal infra only (K8s probes, Prometheus scraper) |

### Rationale

Separate ports give:

- **Independent lifecycle** — drain gRPC while health endpoint still responds
- **Clean observability** — connection metrics per protocol
- **Security boundaries** — debug port never exposed externally
- **K8s compatibility** — correct `appProtocol` hints per port

### Debug Mux (`:9090`)

Use Go 1.22+ `net/http` enhanced routing for the debug HTTP server:

```go
mux := http.NewServeMux()

// Health check (thin wrapper over gRPC health service)
mux.HandleFunc("GET /healthz", healthHandler)

// Prometheus metrics scrape endpoint
mux.Handle("GET /metrics", metricsHandler) // from OTel Prometheus exporter

// pprof (default mux registers automatically, or mount explicitly)
mux.HandleFunc("GET /debug/pprof/", pprof.Index)
mux.HandleFunc("GET /debug/pprof/cmdline", pprof.Cmdline)
mux.HandleFunc("GET /debug/pprof/profile", pprof.Profile)
mux.HandleFunc("GET /debug/pprof/symbol", pprof.Symbol)
mux.HandleFunc("GET /debug/pprof/trace", pprof.Trace)

// Swagger UI — serve generated OpenAPI spec
mux.Handle("GET /swagger/", http.StripPrefix("/swagger/",
    http.FileServer(http.Dir("gen/openapiv2"))))

debugServer := &http.Server{
    Addr:    ":9090",
    Handler: mux,
}
```

### K8s Probe Configuration

```yaml
livenessProbe:
  grpc:
    port: 50051
  initialDelaySeconds: 5
  periodSeconds: 10

readinessProbe:
  grpc:
    port: 50051
  initialDelaySeconds: 5
  periodSeconds: 10
```

For infra that doesn't speak gRPC, use the HTTP health endpoint:

```yaml
livenessProbe:
  httpGet:
    path: /healthz
    port: 9090
```
