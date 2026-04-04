# Telemetry — Logging, Tracing, and Metrics

## Overview

Pivox uses **OpenTelemetry (OTel)** as the unified telemetry framework across all components. Traces, logs, and metrics flow through one protocol (OTLP) to one backend — regardless of which language or component produced them.

The goal: an engineer sees a dropped frame, clicks into it, and traces the full path from operator button press through the Playout Agent to the engine's compositor — with timing for every stage. One dashboard, one query language, correlated across all components.

## Architecture

```
┌─────────────┐  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐
│ Engine      │  │ Playout     │  │ Cloud       │  │ Native App  │
│ (Rust)      │  │ Agent (Go)  │  │ Controller  │  │ (C++/Swift) │
│             │  │             │  │ (Go)        │  │             │
│ OTel SDK    │  │ OTel SDK    │  │ OTel SDK    │  │ OTel SDK    │
└──────┬──────┘  └──────┬──────┘  └──────┬──────┘  └──────┬──────┘
       │                │                │                │
       │           OTLP │           OTLP │           OTLP │
       │                │                │                │
       └────────────────┴────────┬───────┴────────────────┘
                                 │
                                 ▼
                        ┌────────────────┐
                        │ OTel Collector │  (optional — can export direct)
                        └────────┬───────┘
                                 │
                    ┌────────────┼────────────┐
                    ▼            ▼            ▼
              ┌──────────┐ ┌──────────┐ ┌──────────┐
              │ Traces   │ │ Logs     │ │ Metrics  │
              │ (Jaeger/ │ │ (Loki/   │ │ (Prom/   │
              │  Tempo)  │ │  ELK)    │ │  Mimir)  │
              └──────────┘ └──────────┘ └──────────┘
```

## Per-Language SDKs

Each language uses its idiomatic OTel SDK. All export via OTLP.

| Component | Language | OTel SDK | Logging Framework |
|---|---|---|---|
| Engine | Rust | `opentelemetry-rust` + `tracing` | `tracing` crate with OTel layer |
| Playout Agent | Go | `go.opentelemetry.io/otel` | `slog` with OTel bridge |
| Cloud Controller | Go | `go.opentelemetry.io/otel` | `slog` with OTel bridge |
| Shared C++ Core | C++ | `opentelemetry-cpp` | `absl::log` with custom OTel `LogSink` |
| Native App (macOS) | Swift | `opentelemetry-swift` | `os.Logger` or OTel bridge |
| Native App (Windows) | C++/WinRT | `opentelemetry-cpp` | `absl::log` (via shared core) |

### Why `absl::log` for C++

gRPC uses `absl::log` internally. It's already linked as a transitive dependency. The shared C++ core uses `absl::log` directly — `LOG(INFO)`, `LOG(ERROR)`, `VLOG(2)`. A custom `absl::LogSink` bridges to OTel's log exporter:

```cpp
class OTelLogSink : public absl::LogSink {
public:
    void Send(const absl::LogEntry& entry) override {
        // Convert absl::LogEntry → OTel LogRecord
        // Export via OTLP
    }
};
```

This sink is registered once at startup. All `LOG()` calls — both application code and gRPC internals — flow through it. gRPC's verbose logging is suppressed via:

```cpp
absl::SetVLogLevel("*grpc*/*", -1);  // suppress gRPC VLOG noise
```

### Why `tracing` for Rust

The `tracing` crate is the Rust ecosystem standard for structured, span-based instrumentation. It maps naturally to OTel's trace/span model. The `tracing-opentelemetry` layer exports spans and events as OTel data.

```rust
#[tracing::instrument(fields(channel = channel_id))]
fn render_frame(&self, channel_id: u32) -> Frame {
    let _cef_span = tracing::info_span!("cef_render").entered();
    let cef_buffer = self.cef.render();
    drop(_cef_span);

    let _comp_span = tracing::info_span!("composite").entered();
    let frame = self.compositor.blend(&layers);
    drop(_comp_span);

    frame
}
```

This produces a trace with parent span `render_frame` and child spans `cef_render` and `composite` — with timing for each.

## What Gets Instrumented

### Traces

Traces follow requests across component boundaries. Every gRPC call between the native app, Playout Agent, and engine is automatically instrumented by gRPC's OTel integration.

**Engine traces (per frame):**

| Span | Parent | What It Measures |
|---|---|---|
| `frame` | (root) | Total frame time (must be < 16.7ms at 59.94fps) |
| `clock.wait` | `frame` | Time waiting for clock edge (genlock or software) |
| `cef.tick` | `frame` | CEF `DoMessageLoopWork` duration |
| `cef.on_paint` | `frame` | CEF `OnPaint` callback + buffer copy |
| `ffmpeg.advance` | `frame` | FFmpeg frame decode (if video layer active) |
| `rive.advance` | `frame` | Rive state machine advance + render |
| `compositor.render` | `frame` | Layer compositing (alpha blend + transforms) |
| `pipeline.colorspace` | `frame` | sRGB → Rec.709 conversion |
| `pipeline.fill_key` | `frame` | Fill + key buffer split |
| `output.aja` | `frame` | AJA AutoCirculate frame delivery |
| `output.ndi` | `frame` | NDI frame send |

**Playout Agent traces:**

| Span | What It Measures |
|---|---|
| `command.take` | Full take command lifecycle: resolve element → generate engine commands → send → confirm |
| `command.cue` | Cue/load command lifecycle |
| `data_plane.update` | Data feed update: receive → gate → throttle → deliver to engine |
| `rundown.advance` | Rundown auto-advance timer trigger |
| `license.refresh` | Daily license entitlement refresh from Cloud Controller |

**Cross-component trace example:**

```
Native App: operator.take (element="lower-third")
  └── Playout Agent: command.take (channel=0, layer=2)
        ├── engine.load (template="lower-third.html", slot=background)
        └── engine.play (transition=dissolve, duration=20 frames)
              └── Engine: frame (channel=0)
                    ├── cef.tick
                    ├── compositor.render (transition progress=0.5)
                    └── output.aja
```

One trace ID connects the entire chain. An engineer clicks the take command and sees exactly where time was spent.

### Logs

Logs are correlated with trace IDs when available. Every log line includes the active trace and span ID, so you can jump from a log entry to the trace that produced it.

**Structured log schema (all components):**

| Field | Type | Description |
|---|---|---|
| `timestamp` | ISO 8601 | When |
| `severity` | enum | `trace`, `debug`, `info`, `warn`, `error`, `fatal` |
| `body` | string | Human-readable message |
| `component` | string | `engine`, `playout-agent`, `cloud-controller`, `native-app` |
| `host` | string | Machine hostname |
| `trace_id` | hex string | OTel trace ID (if in a trace context) |
| `span_id` | hex string | OTel span ID (if in a span) |
| `channel` | int | Channel number (engine/agent logs) |
| `layer` | int | Layer number (engine logs) |

### Metrics

Continuously exported metrics for dashboards and alerting.

**Engine metrics:**

| Metric | Type | Description |
|---|---|---|
| `pivox.engine.frame_time_ms` | histogram | Per-frame render time |
| `pivox.engine.frame_drops` | counter | Frames that missed the genlock deadline |
| `pivox.engine.cef_render_ms` | histogram | CEF render time per frame |
| `pivox.engine.compositor_ms` | histogram | Compositor blend time per frame |
| `pivox.engine.gpu_utilization` | gauge | GPU usage percentage |
| `pivox.engine.vram_used_mb` | gauge | VRAM consumption |
| `pivox.engine.channels_active` | gauge | Number of active channels |
| `pivox.engine.ndi_bandwidth_mbps` | gauge | NDI output bandwidth |

**Playout Agent metrics:**

| Metric | Type | Description |
|---|---|---|
| `pivox.agent.commands_per_sec` | counter | Playout commands sent to engine |
| `pivox.agent.command_latency_ms` | histogram | Command round-trip time |
| `pivox.agent.data_feed_updates_sec` | counter | Data plane updates per second |
| `pivox.agent.engines_connected` | gauge | Number of connected engines |
| `pivox.agent.license_days_remaining` | gauge | Days until cached entitlement expires |
| `pivox.agent.cloud_connected` | gauge | 1 if connected to Cloud Controller, 0 if offline |

## Sampling

### The Problem

Full trace-level instrumentation on the engine produces 600-1200 spans/second per channel (10-20 spans/frame × 60fps). Four channels = ~5000 spans/second. That's ~430 million spans/day — expensive to store and query.

### Default: Smart Sampling

The engine always instruments everything. The sampling decision happens at export time, not instrumentation time. No code changes between sampling levels.

| Mode | Sample Rate | When |
|---|---|---|
| **Steady state** | 1% head-based | Normal operation. ~50 spans/sec for 4 channels. Enough for performance profiling. |
| **Error-triggered** | 100% on anomalies | Frame drops, budget overruns, plugin crashes. Always captured regardless of sample rate. |
| **On-demand** | Up to 100% | Engineer requests full trace for a specific channel. Auto-reverts. |

### Runtime Sampling Control

Engineers can temporarily increase sampling for debugging via the monitoring dashboard. The system enforces safety constraints:

```
Engineer → "Enable full trace on CH0" (monitoring dashboard)
  │
  ▼
Cloud Controller → sends sampling override to Playout Agent
  │
  ▼
Playout Agent → forwards to Engine via gRPC:
  {channel: 0, sample_rate: 1.0, ttl_seconds: 60}
  │
  ▼
Engine applies override for 60 seconds
  │
  ▼
Engine auto-reverts to default sampling (1%)
```

**Constraints:**
- Maximum TTL is capped (e.g., 300 seconds). Cannot be set to permanent.
- The TTL is enforced by the engine, not the UI. Even if the Cloud Controller goes down mid-override, the engine reverts on its own timer.
- The monitoring dashboard shows active overrides with countdown timers.
- No way to set permanent high sampling through any interface.

### Sampling Configuration

```toml
# Engine config
[telemetry.sampling]
default_rate = 0.01              # 1% steady state
error_rate = 1.0                 # 100% on anomalies
max_override_ttl_seconds = 300   # 5 minute cap on on-demand traces
```

## Export Configuration

### Development / Designer Mode

No collector needed. Export to stdout as structured JSON for local debugging:

```toml
[telemetry]
exporter = "stdout"    # structured JSON to stdout
sampling = 1.0         # 100% — low volume in dev, capture everything
```

### Production

Each agent exports directly to the cloud backend. No dedicated collector process. The engine exports to its local Playout Agent via localhost; the Playout Agent forwards to the cloud alongside its own telemetry.

```
Engine Machine:
  Engine ──OTLP(localhost)──→ Playout Agent ──OTLP──→ Cloud Backend
                                    │
                              Local disk buffer
                              (bounded, survives outages)

Storage Machine:
  Storage Agent ──OTLP──→ Cloud Backend
                    │
              Local disk buffer

Native App ──OTLP──→ Cloud Backend
```

```toml
# Engine config — exports to local Playout Agent
[telemetry]
exporter = "otlp"
otlp_endpoint = "http://localhost:4317"

[telemetry.sampling]
default_rate = 0.01
```

```toml
# Playout Agent config — receives from engine, exports to cloud
[telemetry]
exporter = "otlp"
otlp_endpoint = "https://telemetry.pivox.app:4317"
local_receiver_port = 4317          # accepts OTLP from local engine

[telemetry.buffer]
max_disk_mb = 1024                  # 1GB disk budget for offline buffering
retention_hours = 72                # drop entries older than 72h
```

### Local Buffering and Offline Resilience

Each agent maintains a **bounded disk-backed buffer** for telemetry data. When the cloud backend is unreachable, telemetry accumulates locally. When connectivity restores, the agent flushes the buffer to the cloud.

The buffer has a fixed disk budget (configurable, default 1GB). When the buffer fills, the oldest entries are dropped. No runaway disk consumption. The OTel SDK's `BatchSpanProcessor` and `BatchLogRecordProcessor` handle this natively — bounded queue size, configurable max export batch size, and drop-on-full semantics.

### Offline Diagnosis

When the cloud is unreachable, engineers diagnose locally via the Playout Agent. The native app (Engineering workspace) connects to the Playout Agent on the LAN and reads telemetry directly:

```
Native App (Engineering mode) ──gRPC──→ Playout Agent
                                         ├── Recent traces (in-memory ring buffer)
                                         ├── Recent logs (disk buffer, queryable)
                                         └── Current metrics (live)
```

No cloud needed for local diagnosis. The engineer is already on the LAN. The Playout Agent exposes a local telemetry query API — recent traces, logs, and live metrics — accessible from the native app's Engineering workspace.

## gRPC Auto-Instrumentation

gRPC's OTel integration (`grpc-observability`) automatically instruments all RPC calls with:

- **Traces:** Every RPC gets a span with method name, status code, duration. Context propagation across services is automatic.
- **Metrics:** Request count, latency histograms, error rates per method.
- **Logs:** RPC request/response metadata (configurable — can be disabled for performance).

This means every command from the native app through the Playout Agent to the engine is traced automatically. No manual instrumentation needed for the gRPC layer.

```go
// Go — enable gRPC OTel instrumentation
import "go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"

server := grpc.NewServer(
    grpc.StatsHandler(otelgrpc.NewServerHandler()),
)
```

```rust
// Rust — enable gRPC OTel instrumentation (tonic)
use tonic_opentelemetry::OpenTelemetryLayer;

Server::builder()
    .layer(OpenTelemetryLayer::default())
    .add_service(playout_service)
    .serve(addr)
    .await?;
```

## Backend Recommendations

Pivox does not mandate a specific observability backend. The OTLP protocol is vendor-neutral. Recommended options:

| Backend | Traces | Logs | Metrics | Notes |
|---|---|---|---|---|
| **Grafana Stack** (Tempo + Loki + Mimir) | Tempo | Loki | Mimir/Prometheus | Open source, self-hostable. Good for on-prem. |
| **Grafana Cloud** | Tempo | Loki | Mimir | Hosted. Pay per usage. |
| **Datadog** | Yes | Yes | Yes | Hosted. Full-featured but expensive at scale. |
| **Jaeger + ELK + Prometheus** | Jaeger | Elasticsearch | Prometheus | Open source, proven. More operational overhead. |

For Pivox Cloud (SaaS), Grafana Cloud is the likely default — managed, OTLP-native, reasonable pricing. On-prem customers can point their Playout Agent's collector at their own backend or let telemetry flow to Pivox Cloud for managed monitoring.

## Telemetry vs Customer Data Privacy

Telemetry data (traces, logs, metrics) may contain:
- Template names and data field names (in command traces)
- Asset filenames (in load commands)
- Operator actions and timing

For cloud-hosted telemetry, this data flows to Pivox's backend. Customers who cannot send operational data to the cloud (air-gapped facilities, strict data policies) run their own backend on-prem. The Playout Agent's collector routes to whichever endpoint the customer configures.

Telemetry never contains template content, asset file contents, or data feed values. It contains metadata about operations, not the operations' data.
