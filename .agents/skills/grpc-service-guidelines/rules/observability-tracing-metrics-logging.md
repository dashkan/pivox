---
title: Observability — Tracing, Metrics, Logging
impact: MEDIUM
impactDescription: enables production monitoring and debugging
tags: observability, OTel, tracing, Prometheus, metrics, slog, logging
---

## Observability — Tracing, Metrics, Logging

> Read this when: setting up telemetry, adding application metrics, or implementing the slog trace handler.

### Three-Signal Architecture

| Signal | Pipeline | Rationale |
|---|---|---|
| **Tracing** | OTel SDK -> OTLP exporter -> collector/backend | Distributed traces require OTel — no stdlib alternative |
| **Metrics** | OTel SDK -> Prometheus exporter -> `/metrics` scrape | Prometheus pull model is infra-standard; OTel bridges cleanly |
| **Logging** | `log/slog` -> JSON to stdout -> infra scrapes | 12-factor, container-native; no OTel log exporter needed |

Logs are **correlated** with traces by injecting `trace_id` and `span_id` from OTel span context into every slog record.

### `internal/o11y/` Package Structure

- `tracing.go` — OTel TracerProvider setup (OTLP exporter)
- `metrics.go` — OTel MeterProvider setup (Prometheus exporter)
- `slog.go` — Trace-correlating slog handler wrapper
- `appmetrics.go` — Application metric definitions
- `o11y.go` — Top-level `Init()` / `Shutdown()` for all telemetry

### Tracer Provider (`tracing.go`)

```go
func initTracer(ctx context.Context, serviceName, version string) (*sdktrace.TracerProvider, error) {
    exporter, err := otlptracegrpc.New(ctx) // reads OTEL_EXPORTER_OTLP_ENDPOINT env
    if err != nil {
        return nil, fmt.Errorf("create trace exporter: %w", err)
    }
    tp := sdktrace.NewTracerProvider(
        sdktrace.WithBatcher(exporter),
        sdktrace.WithResource(resource.NewWithAttributes(
            semconv.SchemaURL,
            semconv.ServiceNameKey.String(serviceName),
            semconv.ServiceVersionKey.String(version),
        )),
    )
    otel.SetTracerProvider(tp)
    return tp, nil
}
```

### Prometheus Metrics + Exemplars (`metrics.go`)

```go
func initMeter(serviceName string) (*sdkmetric.MeterProvider, http.Handler, error) {
    exporter, err := prometheus.New(prometheus.WithoutScopeInfo())
    if err != nil {
        return nil, nil, fmt.Errorf("create prometheus exporter: %w", err)
    }
    mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(exporter))
    otel.SetMeterProvider(mp)
    return mp, exporter, nil // exporter is also the /metrics http.Handler
}
```

**Exemplars:** Pass `ctx` when recording metrics so the Prometheus exporter auto-attaches `trace_id`/`span_id`. This enables clicking from a Grafana histogram spike directly to the trace.

```go
requestDuration.Record(ctx, elapsed.Seconds())
// OTel Prometheus exporter auto-extracts trace_id/span_id from ctx
```

### Trace-Correlating slog Handler (`slog.go`)

```go
type traceHandler struct {
    inner slog.Handler
}

func NewTraceHandler(inner slog.Handler) slog.Handler {
    return &traceHandler{inner: inner}
}

func (h *traceHandler) Handle(ctx context.Context, r slog.Record) error {
    sc := trace.SpanContextFromContext(ctx)
    if sc.HasTraceID() {
        r.AddAttrs(
            slog.String("trace_id", sc.TraceID().String()),
            slog.String("span_id", sc.SpanID().String()),
        )
    }
    return h.inner.Handle(ctx, r)
}

func (h *traceHandler) Enabled(ctx context.Context, level slog.Level) bool {
    return h.inner.Enabled(ctx, level)
}

func (h *traceHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
    return &traceHandler{inner: h.inner.WithAttrs(attrs)}
}

func (h *traceHandler) WithGroup(name string) slog.Handler {
    return &traceHandler{inner: h.inner.WithGroup(name)}
}
```

### gRPC Auto-Instrumentation

```go
import "go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"

s := grpc.NewServer(
    grpc.StatsHandler(otelgrpc.NewServerHandler()),
)
```

Provides automatic per-RPC spans + metrics (`rpc.server.duration`, `rpc.server.request.size`).

### slog Conventions

- **Always pass `ctx`** to `slog.InfoContext` / `slog.ErrorContext` for trace correlation
- Log at `Error` for 5xx, `Warn` for 4xx that may indicate bugs, `Info` for success
- gRPC unary interceptor logs: method, caller identity, duration, gRPC status code

```go
// Example output:
// {"time":"...","level":"INFO","msg":"resource created",
//   "method":"CreateThing","resource_name":"things/abc-123",
//   "latency":"12.3ms","trace_id":"a1b2c3...","span_id":"d4e5f6..."}
```

### Application Metrics (`appmetrics.go`)

Infrastructure metrics are auto-handled by OTel gRPC interceptors. These are **your** metrics:

**Resource Lifecycle:**

| Metric | Type | Labels | Purpose |
|---|---|---|---|
| `{service}.resource.created_total` | Counter | `resource_type`, `status` | Creation volume and failure rate |
| `{service}.resource.updated_total` | Counter | `resource_type`, `status` | Update volume |
| `{service}.resource.deleted_total` | Counter | `resource_type`, `delete_type` | Deletion patterns (soft vs hard) |
| `{service}.resource.undeleted_total` | Counter | `resource_type` | High undelete rate = UX problem |

**Long-Running Operations:**

| Metric | Type | Labels | Purpose |
|---|---|---|---|
| `{service}.lro.in_flight` | UpDownCounter | `operation_type` | Currently running operations |
| `{service}.lro.duration_seconds` | Histogram | `operation_type`, `status` | Operation completion time |
| `{service}.lro.failure_total` | Counter | `operation_type`, `error_reason` | Failure rate by reason |

**Authentication:**

| Metric | Type | Labels | Purpose |
|---|---|---|---|
| `{service}.auth.attempts_total` | Counter | `method`, `status` | Auth method distribution |
| `{service}.auth.failures_total` | Counter | `method`, `reason` | Spike = broken integration |

**Database:**

| Metric | Type | Labels | Purpose |
|---|---|---|---|
| `{service}.db.query_duration_seconds` | Histogram | `query_name` | Per-query latency (sqlc names) |
| `{service}.db.errors_total` | Counter | `query_name`, `error_type` | DB error rate by query |
| `{service}.db.pool.active_connections` | UpDownCounter | — | Connection pool pressure |

**Pagination Depth:**

| Metric | Type | Labels | Purpose |
|---|---|---|---|
| `{service}.list.page_depth` | Histogram | `resource_type` | Deep pagination = bad filters or small page size |
| `{service}.list.result_count` | Histogram | `resource_type` | Results per page actually returned |

**Implementation Pattern:**

```go
type AppMetrics struct {
    ResourceCreated metric.Int64Counter
    LROInFlight     metric.Int64UpDownCounter
    LRODuration     metric.Float64Histogram
    DBQueryDuration metric.Float64Histogram
    // ...
}

func NewAppMetrics(serviceName string) (*AppMetrics, error) {
    meter := otel.Meter(serviceName)
    m := &AppMetrics{}
    var err error
    m.ResourceCreated, err = meter.Int64Counter(serviceName+".resource.created_total",
        metric.WithDescription("Total resources created"))
    // ... initialize all metrics ...
    return m, nil
}
```

Instrument at the **service layer**, always passing `ctx` for exemplar correlation:

```go
s.metrics.ResourceCreated.Add(ctx, 1,
    metric.WithAttributes(
        attribute.String("resource_type", "Thing"),
        attribute.String("status", "ok"),
    ),
)
```

**Principle:** When implementing any method, ask: "What metric would an SRE want to alert on or dashboard for this?" Always add at least one.
