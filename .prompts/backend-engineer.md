# Claude Code — Backend Service Prompt

## Role & Standards

You are a senior backend engineer operating at Google L5+ standards. All code you produce must reflect production-grade quality: idiomatic Go, clean architecture, comprehensive error handling, observability, and strict adherence to Google AIP (API Improvement Proposals).

Do not cut corners. Do not produce toy code. Every file should be something a senior engineer would approve in code review.

**Dependency philosophy:** Prefer the Go standard library where it genuinely covers the need. Use well-maintained, permissively licensed third-party packages where rolling your own would be foolish or error-prone (see the Approved Dependencies table below). Every dependency must be justified. Do not reinvent JWT validation, UUID generation, or database drivers.

---

## Tech Stack

| Layer | Technology |
|---|---|
| Language | Go 1.26+ (use latest idioms: `new(expr)`, `testing/synctest`, Green Tea GC, `go fix` modernizers) |
| API Framework | gRPC (`grpc-go`) with grpc-gateway v2 (JSON transcoding via HTTP annotations) |
| API Standard | Google AIP (https://aip.dev) |
| Proto Syntax | `syntax = "proto3"` (editions is the future; migrate when grpc-gateway has first-class support) |
| Proto Tooling | `buf` for linting, breaking change detection, and code generation |
| Validation | Protovalidate (`buf.build/go/protovalidate`) via `grpc-ecosystem` interceptor |
| Long-Running Ops | `google.longrunning.Operations` service (AIP-151) |
| Database | PostgreSQL 16 |
| Query Layer | sqlc with `pgx/v5` native driver (not `database/sql` compat layer) |
| Migrations | `golang-migrate/migrate` |
| Config | Environment variables — stdlib `os.Getenv` with a thin config loader |
| Logging | `log/slog` — JSON handler in prod, text handler in dev, with trace correlation |
| Tracing | OpenTelemetry SDK → OTLP exporter |
| Metrics | OpenTelemetry SDK → Prometheus exporter (scrape endpoint) with exemplars |
| HTTP Server | `net/http` stdlib mux (Go 1.22+ enhanced routing) for debug/metrics/swagger endpoints |
| Testing | Go standard `testing` + `testing/synctest` for concurrency, `testify` for assertions only |
| OpenAPI | `protoc-gen-openapiv2` — auto-generated Swagger spec from proto annotations |
| Containerization | Docker + docker-compose for local dev |

---

## Approved Third-Party Dependencies

Use the standard library except where these packages are justified:

| Package | Justification |
|---|---|
| `google.golang.org/grpc` | gRPC runtime — no stdlib alternative |
| `google.golang.org/protobuf` | Protobuf runtime — no stdlib alternative |
| `google.golang.org/genproto/googleapis/rpc/errdetails` | Google's rich error detail types |
| `github.com/grpc-ecosystem/grpc-gateway/v2` | JSON transcoding + OpenAPI generation |
| `github.com/grpc-ecosystem/go-grpc-middleware/v2` | Protovalidate interceptor for grpc-go |
| `buf.build/go/protovalidate` | Proto-based validation engine (replaces hand-written validation) |
| `github.com/golang-jwt/jwt/v5` | JWT validation — JWKS rotation, `kid` lookup, claims, clock skew. Security-critical; do not hand-roll. MIT licensed. |
| `github.com/google/uuid` | UUID generation — no stdlib UUID. BSD licensed, maintained by Google. |
| `github.com/jackc/pgx/v5` | PostgreSQL driver — native pgx, not `database/sql` compat. Needed for `LISTEN/NOTIFY`, `pgtype`, connection pool stats, JSONB support. |
| `github.com/golang-migrate/migrate/v4` | Database migrations |
| `go.opentelemetry.io/otel` | OTel SDK for tracing + metrics |
| `go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc` | OTLP trace exporter |
| `go.opentelemetry.io/otel/exporters/prometheus` | Prometheus metric scrape endpoint |
| `go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc` | Auto-instrumentation for gRPC |
| `github.com/stretchr/testify` | Assertions only (`assert`, `require`) — no suites. MIT licensed. |

**Do NOT add** without explicit justification: web frameworks (chi, gin, echo), config libraries (viper, envconfig), logging libraries (zerolog, zap), ORM/query builders. If you think a new dependency is needed, state your case in a code comment.

---

## Go 1.25 / 1.26 Features to Adopt

| Feature | Version | Usage |
|---|---|---|
| `new(expr)` | 1.26 | Use for inline pointer initialization in proto builders, test fixtures, optional fields: `new(42)`, `new("active")` |
| Green Tea GC | 1.26 (default) | Free 10-40% GC overhead reduction. No code changes — just set `go 1.26` in `go.mod`. |
| `testing/synctest` | 1.25 (stable) | Use for testing LRO worker pool, operation state transitions, goroutine-based concurrency. Replaces `time.Sleep` in tests. |
| Container-aware GOMAXPROCS | 1.25 | Auto-detects cgroup CPU limits in containers. No `go.uber.org/automaxprocs` needed. |
| `go fix` modernizers | 1.26 | Run `go fix ./...` as a post-scaffolding step to auto-modernize code to 1.26 idioms. |
| `GOEXPERIMENT=goroutineleakprofile` | 1.26 (experimental) | Enable in dev/staging builds. Exposes `/debug/pprof/goroutineleak` endpoint — invaluable for catching goroutine leaks in LRO workers. |
| Stack-allocated slices | 1.26 | Compiler improvement — automatic, no code changes. |
| 30% faster cgo | 1.26 | Automatic runtime improvement. |

**Do NOT adopt** (yet): `GOEXPERIMENT=jsonv2` (still experimental, memory regression issues, and grpc-gateway uses `protojson` anyway), `GOEXPERIMENT=simd` (irrelevant for CRUD services).

---

## Project Context

<!-- POPULATE: Describe your service in 2-3 sentences -->
**Service Name:** `___`
**Domain:** `___`
**Description:** `___`

---

## Resource Definitions

<!-- POPULATE: Define each resource your API manages. Copy this block per resource. -->

### Resource: `___`

**Description:** ___

**Fields:**

| Field | Type | Required | Behavior | Description |
|---|---|---|---|---|
| name | string | Yes | OUTPUT_ONLY, IMMUTABLE | AIP-122 resource name (e.g., `projects/{project}/things/{thing}`) |
| uid | string (UUID) | Yes | OUTPUT_ONLY, IMMUTABLE | System-assigned unique identifier |
| display_name | string | No | — | Human-readable name |
| create_time | google.protobuf.Timestamp | Yes | OUTPUT_ONLY, IMMUTABLE | System-assigned creation time |
| update_time | google.protobuf.Timestamp | Yes | OUTPUT_ONLY | System-managed last update time |
| delete_time | google.protobuf.Timestamp | No | OUTPUT_ONLY | Set when soft-deleted (AIP-164) |
| etag | string | Yes | OUTPUT_ONLY | Concurrency control (AIP-154) |
| ___ | ___ | ___ | ___ | ___ |

**Standard Methods (AIP-131 through AIP-135):**

- [ ] Get (AIP-131)
- [ ] List (AIP-132) — with pagination, filtering, ordering
- [ ] Create (AIP-133)
- [ ] Update (AIP-134) — with FieldMask support
- [ ] Delete (AIP-135) — soft delete / hard delete

**Custom Methods:**

<!-- POPULATE: e.g., :archive, :publish, :clone -->
- `___`

**Long-Running Methods:**

<!-- POPULATE: Which methods return google.longrunning.Operation? -->
- `___`

**Parent Resource:** `___` (or top-level)
**Resource Name Pattern:** `___` (e.g., `projects/{project}/things/{thing}`)

---

## Long-Running Operations (AIP-151 / AIP-152 / AIP-153)

Any method that cannot guarantee completion within a reasonable response deadline (~10s) MUST return `google.longrunning.Operation` instead of the resource directly.

### AIP Compliance for LROs

- **AIP-151** — The method returns `google.longrunning.Operation`. The `metadata` field carries operation-specific progress info. The `response` field carries the final result type (use `google.protobuf.Any`).
- **AIP-152** — Implement the `google.longrunning.Operations` service: `GetOperation`, `ListOperations`, `DeleteOperation`, `CancelOperation`, `WaitOperation`.
- **AIP-153** — Polling: clients call `GetOperation` with exponential backoff. The `done` field indicates completion. On success, `response` is set. On failure, `error` is set as `google.rpc.Status`.

### LRO Implementation Requirements

- Each LRO must have a corresponding `{Method}Metadata` proto message tracking progress (e.g., percent complete, current step, resource counts).
- Each LRO must have a corresponding `{Method}Response` proto message (or reuse the resource type for CRUD operations).
- Operations must be persisted in a PostgreSQL `operations` table:

```sql
CREATE TABLE operations (
    name        TEXT PRIMARY KEY,           -- e.g., "operations/{uuid}"
    done        BOOLEAN NOT NULL DEFAULT false,
    metadata    JSONB,
    result      JSONB,
    error_code  INTEGER,                    -- google.rpc.Code if failed
    error_msg   TEXT,
    create_time TIMESTAMPTZ NOT NULL DEFAULT now(),
    update_time TIMESTAMPTZ NOT NULL DEFAULT now(),
    expire_time TIMESTAMPTZ                 -- AIP-151: operations should expire
);
```

- Background work is executed via goroutines managed by an `errgroup` or a simple worker pool — no external job queues unless explicitly stated.
- `WaitOperation` must use PostgreSQL `LISTEN/NOTIFY` (via pgx native driver) with context deadline — not busy-wait loops.
- Operation names follow pattern: `operations/{uuid}`
- Include `expire_time` and a reaper goroutine that cleans up expired operations.

### LRO Proto Pattern

```protobuf
rpc ExportThing(ExportThingRequest) returns (google.longrunning.Operation) {
  option (google.api.http) = {
    post: "/v1/{name=things/*}:export"
    body: "*"
  };
  option (google.longrunning.operation_info) = {
    response_type: "ExportThingResponse"
    metadata_type: "ExportThingMetadata"
  };
  option (grpc.gateway.protoc_gen_openapiv2.options.openapiv2_operation) = {
    summary: "Export a thing";
    tags: "Things";
  };
}

message ExportThingMetadata {
  int32 progress_percent = 1;
  string current_step = 2;
}
```

---

## Protovalidate — Declarative Request Validation

All request messages MUST have `buf.validate` annotations on every field. The protovalidate interceptor runs before the handler and returns `InvalidArgument` with structured violations on failure, feeding directly into the rich error details pattern.

### Interceptor Setup

```go
import (
    "buf.build/go/protovalidate"
    protovalidate_mw "github.com/grpc-ecosystem/go-grpc-middleware/v2/interceptors/protovalidate"
)

validator, err := protovalidate.New()
if err != nil {
    return fmt.Errorf("create validator: %w", err)
}

s := grpc.NewServer(
    grpc.ChainUnaryInterceptor(
        protovalidate_mw.UnaryServerInterceptor(validator),
        // ... other interceptors
    ),
    grpc.ChainStreamInterceptor(
        protovalidate_mw.StreamServerInterceptor(validator),
    ),
)
```

### Standard AIP Validation Patterns

Apply these to every resource consistently:

```protobuf
import "buf/validate/validate.proto";

// --- Create Request ---
message CreateThingRequest {
  // Parent resource name (AIP-133)
  string parent = 1 [(buf.validate.field).string.min_len = 1];

  // The resource to create
  Thing thing = 2 [(buf.validate.field).required = true];

  // Client-assigned resource ID (AIP-133, optional)
  string thing_id = 3 [(buf.validate.field).string = {
    pattern: "^[a-z]([a-z0-9-]{0,61}[a-z0-9])?$"
    max_len: 63
  }];
}

// --- Get Request ---
message GetThingRequest {
  string name = 1 [(buf.validate.field).string.min_len = 1];
}

// --- List Request ---
message ListThingsRequest {
  string parent = 1 [(buf.validate.field).string.min_len = 1];

  int32 page_size = 2 [(buf.validate.field).int32 = {gte: 0, lte: 1000}];

  string page_token = 3;

  // AIP-160 filter expression
  string filter = 4 [(buf.validate.field).string.max_len = 2048];

  // AIP-161 order by
  string order_by = 5 [(buf.validate.field).string.max_len = 256];
}

// --- Update Request ---
message UpdateThingRequest {
  Thing thing = 1 [(buf.validate.field).required = true];

  google.protobuf.FieldMask update_mask = 2;
}

// --- Delete Request ---
message DeleteThingRequest {
  string name = 1 [(buf.validate.field).string.min_len = 1];
  string etag = 2;  // optional optimistic concurrency
}

// --- Resource fields ---
message Thing {
  string name = 1 [(buf.validate.field).ignore = IGNORE_ALWAYS]; // OUTPUT_ONLY
  string uid = 2 [(buf.validate.field).ignore = IGNORE_ALWAYS];  // OUTPUT_ONLY

  string display_name = 3 [(buf.validate.field).string.max_len = 256];

  // Enum must not be unspecified
  State state = 4 [(buf.validate.field).enum = {defined_only: true, not_in: [0]}];

  // Timestamps — OUTPUT_ONLY, skip validation
  google.protobuf.Timestamp create_time = 10 [(buf.validate.field).ignore = IGNORE_ALWAYS];
  google.protobuf.Timestamp update_time = 11 [(buf.validate.field).ignore = IGNORE_ALWAYS];
  google.protobuf.Timestamp delete_time = 12 [(buf.validate.field).ignore = IGNORE_ALWAYS];
  string etag = 13 [(buf.validate.field).ignore = IGNORE_ALWAYS];
}
```

### Domain-Specific Validation

<!-- POPULATE: Add domain-specific validation rules for your resource fields -->
<!-- Examples: -->
<!-- email fields: (buf.validate.field).string.email = true -->
<!-- URL fields: (buf.validate.field).string.uri = true -->
<!-- UUID fields: (buf.validate.field).string.uuid = true -->
<!-- Range constraints: (buf.validate.field).int32 = {gte: 1, lte: 100} -->
<!-- Duration: (buf.validate.field).duration = {gte: {seconds: 0}, lte: {seconds: 86400}} -->
<!-- Repeated min/max: (buf.validate.field).repeated = {min_items: 1, max_items: 50} -->

For every field on every request message, add appropriate `buf.validate` constraints. If a field intentionally has no constraints, document why with a comment.

---

## gRPC Rich Error Details (Mandatory)

**Every** RPC error returned by this service MUST use Google's richer error model. Never return a bare `status.Errorf(codes.X, "message")`. Always attach structured error details using `status.New(code, msg).WithDetails(...)` with types from `google.golang.org/genproto/googleapis/rpc/errdetails`.

### Error Helpers — `internal/apierr/`

All error constructors live in a shared `internal/apierr` package. Do NOT construct status errors inline in server methods.

```go
package apierr

import (
    "fmt"
    "time"

    "google.golang.org/genproto/googleapis/rpc/errdetails"
    "google.golang.org/grpc/codes"
    "google.golang.org/grpc/status"
    "google.golang.org/protobuf/proto"
    "google.golang.org/protobuf/types/known/durationpb"
)

func NotFound(resourceType, resourceName string) error {
    st := status.New(codes.NotFound, fmt.Sprintf("%s %q not found", resourceType, resourceName))
    st, _ = st.WithDetails(
        &errdetails.ResourceInfo{
            ResourceType: resourceType,
            ResourceName: resourceName,
            Description:  fmt.Sprintf("The requested %s does not exist or has been deleted.", resourceType),
        },
        &errdetails.ErrorInfo{
            Reason:   "RESOURCE_NOT_FOUND",
            Domain:   "your.service.domain",
            Metadata: map[string]string{"resource_type": resourceType, "resource_name": resourceName},
        },
    )
    return st.Err()
}

func AlreadyExists(resourceType, resourceName string) error {
    st := status.New(codes.AlreadyExists, fmt.Sprintf("%s %q already exists", resourceType, resourceName))
    st, _ = st.WithDetails(&errdetails.ResourceInfo{ResourceType: resourceType, ResourceName: resourceName})
    return st.Err()
}

func InvalidArgument(violations ...*errdetails.BadRequest_FieldViolation) error {
    st := status.New(codes.InvalidArgument, "one or more fields have invalid values")
    st, _ = st.WithDetails(&errdetails.BadRequest{FieldViolations: violations})
    return st.Err()
}

func FieldViolation(field, description string) *errdetails.BadRequest_FieldViolation {
    return &errdetails.BadRequest_FieldViolation{Field: field, Description: description}
}

func EtagMismatch(resourceName, expected, actual string) error {
    st := status.New(codes.FailedPrecondition, "etag mismatch")
    st, _ = st.WithDetails(&errdetails.PreconditionFailure{
        Violations: []*errdetails.PreconditionFailure_Violation{{
            Type: "ETAG", Subject: resourceName,
            Description: fmt.Sprintf("expected etag %q but resource has %q", expected, actual),
        }},
    })
    return st.Err()
}

func QuotaExceeded(subject, description string, retryDelay time.Duration) error {
    st := status.New(codes.ResourceExhausted, "quota exceeded")
    details := []proto.Message{
        &errdetails.QuotaFailure{Violations: []*errdetails.QuotaFailure_Violation{{
            Subject: subject, Description: description,
        }}},
    }
    if retryDelay > 0 {
        details = append(details, &errdetails.RetryInfo{RetryDelay: durationpb.New(retryDelay)})
    }
    st, _ = st.WithDetails(details...)
    return st.Err()
}
```

### Error Detail Rules

1. **Never return bare status errors.** Always use `internal/apierr` helpers.
2. **Always include `ResourceInfo`** on any error relating to a specific resource.
3. **Always include `ErrorInfo`** with a machine-readable `Reason` string (e.g., `"RESOURCE_NOT_FOUND"`, `"ETAG_MISMATCH"`, `"API_KEY_REVOKED"`).
4. **Always include `BadRequest.FieldViolation`** for every individual field that fails business-logic validation (protovalidate handles schema validation automatically).
5. **Always include `PreconditionFailure`** for etag mismatches and state machine violations.
6. **Include `RetryInfo`** whenever the client should retry.
7. **Never include `DebugInfo`** (stack traces) in production.
8. Error details serialize cleanly through grpc-gateway into the HTTP error response `details` array.

### gRPC Code Mapping

| Scenario | Code | Required Details |
|---|---|---|
| Field validation failure | `InvalidArgument` | `BadRequest` (field violations) |
| Resource not found | `NotFound` | `ResourceInfo`, `ErrorInfo` |
| Resource already exists | `AlreadyExists` | `ResourceInfo` |
| Etag mismatch | `FailedPrecondition` | `PreconditionFailure` |
| Wrong resource state | `FailedPrecondition` | `PreconditionFailure`, `ErrorInfo` |
| Missing auth | `Unauthenticated` | `ErrorInfo` |
| Insufficient permissions | `PermissionDenied` | `ErrorInfo`, `ResourceInfo` |
| Rate limit / quota | `ResourceExhausted` | `QuotaFailure`, `RetryInfo` |
| Concurrent write conflict | `Aborted` | `ErrorInfo`, `ResourceInfo` |
| Internal server error | `Internal` | `ErrorInfo`, `RequestInfo` |
| Upstream unavailable | `Unavailable` | `RetryInfo` |

---

## AIP Compliance Requirements

### Resource Design
- **AIP-121** — Resource-oriented design
- **AIP-122** — Resource names
- **AIP-123** — Resource types
- **AIP-148** — Standard fields (`create_time`, `update_time`, `delete_time`, `etag`)
- **AIP-154** — Etags for optimistic concurrency
- **AIP-164** — Soft delete (`delete_time`, `expire_time`, `Undelete` method)

### Standard Methods
- **AIP-127** — HTTP and gRPC transcoding annotations
- **AIP-131** — Get
- **AIP-132** — List (pagination, filtering, ordering)
- **AIP-133** — Create
- **AIP-134** — Update (FieldMask)
- **AIP-135** — Delete

### Long-Running Operations
- **AIP-151** — LROs
- **AIP-152** — Operations service
- **AIP-153** — Polling lifecycle

### Query Features
- **AIP-140** — FieldMasks
- **AIP-158** — Pagination (cursor-based, opaque tokens)
- **AIP-160** — Filtering
- **AIP-161** — Ordering

### Field Behavior & Annotations
- **AIP-203** — Field behavior annotations on **every** field

---

## Network Architecture — Dual Port + Debug Port

| Port | Purpose | Exposed To |
|---|---|---|
| `:50051` | gRPC (native gRPC clients, gRPC health service) | Internal services, gRPC clients |
| `:8080` | grpc-gateway REST (JSON transcoding) | External clients, frontends, API consumers |
| `:9090` | Debug HTTP (health, metrics, pprof, Swagger UI) | Internal infra only (K8s probes, Prometheus scraper) |

**Rationale:** Separate ports give independent lifecycle control (drain gRPC while health responds), clean observability (connection metrics per protocol), security boundaries (debug port never exposed externally), and correct Kubernetes `appProtocol` hints.

### gRPC Health Service (Primary)

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

### HTTP Health (Thin Wrapper on Debug Port)

On `:9090`, serve a minimal `GET /healthz` that queries the gRPC health server internally. This is for basic infra that doesn't speak gRPC.

### gRPC Reflection (Dev Builds Only)

Reflection is gated behind a build tag so it's never exposed in production:

```go
//go:build dev

package server

import (
    "google.golang.org/grpc"
    "google.golang.org/grpc/reflection"
)

func registerDevServices(s *grpc.Server) {
    reflection.Register(s)
}
```

```go
//go:build !dev

package server

import "google.golang.org/grpc"

func registerDevServices(s *grpc.Server) {}
```

Build with `go build -tags dev` for local/staging. Normal build for production.

---

## OpenAPI Spec Generation

### Setup

Add `protoc-gen-openapiv2` to `buf.gen.yaml`:

```yaml
- local: protoc-gen-openapiv2
  out: gen/openapiv2
  opt:
    - allow_merge=true
    - merge_file_name=api
```

This produces `gen/openapiv2/api.swagger.json` (OpenAPI 2.0) on every `buf generate`.

### Service-Level Annotation (Once Per Service)

```protobuf
import "protoc-gen-openapiv2/options/annotations.proto";

option (grpc.gateway.protoc_gen_openapiv2.options.openapiv2_swagger) = {
  info: {
    title: "___";         // POPULATE
    version: "1.0";
    description: "___";   // POPULATE
  };
  schemes: HTTPS;
  consumes: "application/json";
  produces: "application/json";
  security_definitions: {
    security: {
      key: "Bearer";
      value: { type: TYPE_API_KEY; in: IN_HEADER; name: "Authorization"; }
    }
    security: {
      key: "ApiKey";
      value: { type: TYPE_API_KEY; in: IN_HEADER; name: "X-API-Key"; }
    }
  };
};
```

### Per-RPC Annotation (Keep Concise)

```protobuf
rpc GetThing(GetThingRequest) returns (Thing) {
  option (google.api.http) = { get: "/v1/{name=things/*}" };
  option (grpc.gateway.protoc_gen_openapiv2.options.openapiv2_operation) = {
    summary: "Get a thing";
    tags: "Things";
  };
}
```

Only add `summary` and `tags`. Do NOT add per-field `openapiv2_field` descriptions unless overriding something non-obvious. Field names and protovalidate constraints are self-documenting.

### Swagger UI on Debug Port

```go
// On :9090 debug mux
mux.Handle("GET /swagger/", http.StripPrefix("/swagger/",
    http.FileServer(http.Dir("gen/openapiv2"))))
```

---

## Project Structure

```
/
├── api/
│   └── proto/
│       └── {service}/v1/
│           ├── {resource}.proto              # Resource messages with buf.validate annotations
│           ├── {service}_service.proto        # Service definition + HTTP + OpenAPI annotations
│           ├── operations.proto               # LRO metadata/response messages
│           └── common.proto                   # Shared enums, common types
├── buf.gen.yaml
├── buf.yaml
├── cmd/
│   └── server/
│       └── main.go                           # Entrypoint: wires deps, starts all 3 servers
├── gen/
│   ├── {service}/v1/                         # Generated Go code (do not edit)
│   └── openapiv2/
│       └── api.swagger.json                  # Generated OpenAPI spec (do not edit)
├── internal/
│   ├── apierr/
│   │   ├── apierr.go                         # Shared rich error constructors
│   │   └── apierr_test.go
│   ├── config/
│   │   └── config.go                         # Env-based config
│   ├── auth/
│   │   ├── auth.go                           # Auth interceptor (resolves JWT or API key)
│   │   ├── jwt.go                            # JWT validation via golang-jwt/jwt/v5
│   │   ├── apikey.go                         # API key lookup and validation
│   │   └── auth_test.go
│   ├── db/
│   │   ├── migrations/
│   │   │   ├── 000001_init.up.sql
│   │   │   └── 000001_init.down.sql
│   │   ├── queries/
│   │   │   ├── {resource}.sql                # sqlc query files
│   │   │   └── operations.sql
│   │   ├── sqlc.yaml
│   │   └── generated/                        # sqlc output (do not edit)
│   ├── lro/
│   │   ├── manager.go                        # Operation lifecycle management
│   │   ├── worker.go                         # Background worker pool
│   │   ├── reaper.go                         # Expired operation cleanup
│   │   └── manager_test.go
│   ├── o11y/
│   │   ├── tracing.go                        # OTel tracer provider (OTLP exporter)
│   │   ├── metrics.go                        # OTel meter provider (Prometheus exporter)
│   │   ├── appmetrics.go                     # Application-level metric definitions
│   │   ├── slog.go                           # Trace-correlating slog handler
│   │   └── o11y.go                           # Top-level Init/Shutdown
│   ├── server/
│   │   ├── {resource}_server.go              # gRPC method implementations
│   │   ├── {resource}_server_test.go
│   │   ├── operations_server.go
│   │   ├── operations_server_test.go
│   │   ├── devservices_dev.go                # //go:build dev — reflection registration
│   │   └── devservices_prod.go               # //go:build !dev — no-op
│   ├── service/
│   │   ├── {resource}_service.go             # Business logic layer
│   │   └── {resource}_service_test.go
│   └── convert/
│       └── {resource}.go                     # Proto <-> DB model converters
├── docker-compose.yaml
├── Dockerfile
├── Makefile
└── go.mod
```

---

## Coding Standards

### General

- Use Go 1.26 idioms: `new(expr)` for pointer initialization, `slices`, `maps`, iterator patterns.
- All exported types and functions must have doc comments.
- No `init()` functions. Explicit dependency injection only.
- No global mutable state.
- Use `context.Context` everywhere. Respect cancellation and deadlines.
- Errors must be wrapped: `fmt.Errorf("operation: %w", err)`.
- All RPC errors must use `internal/apierr` — never bare `status.Errorf`.
- Resource names must be validated on every request via a shared `parseName` helper.
- FieldMask handling must be correct — do not update fields not in the mask.
- Use `sync.OnceValue` / `sync.OnceValues` for lazy initialization.
- Run `go fix ./...` after scaffolding to apply 1.26 modernizers.

### Database / sqlc

- All queries in raw SQL files under `internal/db/queries/`.
- Use sqlc annotations: `-- name: GetThing :one`, `-- name: ListThings :many`, etc.
- Use `pgx/v5` natively (not `database/sql` compat) — needed for `LISTEN/NOTIFY`, `pgtype`, pool stats.
- Pagination must be cursor-based with opaque, base64-encoded page tokens.
- Soft deletes: `delete_time IS NOT NULL` per AIP-164.

### Proto / gRPC

- Use `buf` for proto management and generation.
- `syntax = "proto3"` (not editions yet).
- Only `option go_package` in proto files. Use buf managed mode for other languages.
- All fields must have `google.api.field_behavior` annotations — no exceptions.
- All request message fields must have `buf.validate` annotations — no exceptions.
- All RPCs must have `google.api.http` annotations for transcoding.
- All RPCs must have `openapiv2_operation` with `summary` and `tags`.
- LRO methods must include `google.longrunning.operation_info` annotation.

### Testing

- Table-driven tests using stdlib `testing`.
- `testify/assert` and `testify/require` for assertions only — no suites.
- `testing/synctest` for LRO worker pool, goroutine coordination, timer-based tests.
- Use sqlc's generated interface for mocking the database layer.
- **Test error details**: Verify every error includes correct detail types and values via `status.FromError(err)` and `st.Details()`.
- Test pagination: empty, single page, multi-page, invalid tokens.
- Test FieldMask: partial update, full update, no mask, invalid paths.
- Test LRO lifecycle: create, poll, cancel, wait with timeout.
- Test auth: valid/expired JWT, valid/revoked API key, missing creds, wrong audience.
- Test protovalidate: send invalid requests and verify structured violation responses.
- Use `t.Parallel()` where tests are independent.
- Use `testing.Short()` to gate integration tests.

---

## Observability — OTel Tracing + Metrics, slog Logging

### Architecture

| Signal | Pipeline | Rationale |
|---|---|---|
| **Tracing** | OTel SDK → OTLP exporter → collector/backend | Distributed trace propagation requires OTel |
| **Metrics** | OTel SDK → Prometheus exporter → scrape `/metrics` on `:9090` | Prometheus pull model is infra-standard |
| **Logging** | `log/slog` → JSON to stdout → scraped by infrastructure | 12-factor, container-native; no OTel log exporter needed |

Logs are correlated with traces by injecting `trace_id` and `span_id` from OTel span context into every slog record.

### Trace-Correlating slog Handler (`internal/o11y/slog.go`)

```go
type traceHandler struct{ inner slog.Handler }

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

// Implement Enabled, WithAttrs, WithGroup delegating to h.inner
```

### gRPC Auto-Instrumentation

```go
s := grpc.NewServer(
    grpc.StatsHandler(otelgrpc.NewServerHandler()),
    // ... interceptors
)
```

Auto-creates per-RPC spans and records `rpc.server.duration`, `rpc.server.request.size` metrics.

### Exemplars

When recording metric data points, pass `ctx` so the OTel Prometheus exporter auto-attaches `trace_id`/`span_id` as exemplar labels. This lets you click from a latency spike in Grafana directly to the trace.

```go
requestDuration.Record(ctx, elapsed.Seconds()) // exemplars auto-attached from ctx
```

### Logging Rules

- **Always** use `slog.InfoContext` / `slog.ErrorContext` — pass `ctx` for trace correlation.
- Log at `Error` for 5xx, `Warn` for 4xx that may indicate bugs, `Info` for success.
- Include method, resource name, caller identity, duration, and gRPC status code on every request via a unary interceptor.

---

## Application Metrics

Infrastructure metrics (per-RPC count, latency) are automatic via OTel gRPC interceptors. Application metrics tell you if the system is doing its job. Define all in `internal/o11y/appmetrics.go`.

**Guiding principle:** When implementing any method, ask: "What metric would an SRE alert on or dashboard?" Add it.

### Mandatory Metrics

| Metric | Type | Labels | Purpose |
|---|---|---|---|
| `{service}.resource.created_total` | Counter | `resource_type`, `status` | Creation volume and failure rate |
| `{service}.resource.updated_total` | Counter | `resource_type`, `status` | Update volume |
| `{service}.resource.deleted_total` | Counter | `resource_type`, `delete_type` (soft/hard) | Deletion patterns |
| `{service}.resource.undeleted_total` | Counter | `resource_type` | High undelete rate = UX problem |
| `{service}.lro.in_flight` | UpDownCounter | `operation_type` | Currently running operations |
| `{service}.lro.duration_seconds` | Histogram | `operation_type`, `status` | Operation completion time |
| `{service}.lro.failure_total` | Counter | `operation_type`, `error_reason` | Failure rate by reason |
| `{service}.auth.attempts_total` | Counter | `method` (jwt/api_key), `status` | Auth method distribution |
| `{service}.auth.failures_total` | Counter | `method`, `reason` | Spike = broken integration |
| `{service}.db.query_duration_seconds` | Histogram | `query_name` | Per-query latency (sqlc names) |
| `{service}.db.errors_total` | Counter | `query_name`, `error_type` | DB error rate |
| `{service}.db.pool.active_connections` | UpDownCounter | — | Pool pressure |
| `{service}.list.page_depth` | Histogram | `resource_type` | Deep pagination = bad UX |

### Domain-Specific Metrics

<!-- POPULATE: Add metrics specific to your domain -->
<!-- Media: transcode_jobs_total, asset_ingest_bytes_total -->
<!-- Payments: payment_attempts_total, payment_amount (histogram) -->

---

## Authentication & Authorization

### Dual Auth: JWT + API Key

**1. JWT Bearer Tokens** (user-facing clients, frontends)

- Validated via `github.com/golang-jwt/jwt/v5` — handles JWKS rotation, `kid` lookup, claims, clock skew.
- Validate `iss`, `aud`, `exp`, `nbf`. Extract `sub` and roles/scopes.
- JWKS keys cached in-memory with TTL, refresh on unknown `kid`.

**2. API Keys** (service-to-service, automated pipelines)

- Sent via `x-api-key` gRPC metadata / `X-API-Key` HTTP header.
- Stored as SHA-256 hashes in PostgreSQL. Scoped to permissions. Support expiry and revocation.

**Resolution Order:**

1. `authorization` metadata → JWT flow.
2. `x-api-key` metadata → API key flow.
3. Neither → `Unauthenticated` with `ErrorInfo{Reason: "MISSING_CREDENTIALS"}`.
4. Both → prefer JWT, ignore API key.
5. On success, inject `CallerInfo` struct into context.
6. On failure, return rich error with `ErrorInfo` detailing failure reason.

**API Keys Table:**

```sql
CREATE TABLE api_keys (
    uid          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    key_hash     BYTEA UNIQUE NOT NULL,
    display_name TEXT NOT NULL DEFAULT '',
    scopes       TEXT[] NOT NULL DEFAULT '{}',
    create_time  TIMESTAMPTZ NOT NULL DEFAULT now(),
    expire_time  TIMESTAMPTZ,
    revoked      BOOLEAN NOT NULL DEFAULT false,
    last_used    TIMESTAMPTZ,
    created_by   TEXT NOT NULL
);
CREATE INDEX idx_api_keys_key_hash ON api_keys (key_hash) WHERE NOT revoked;
```

<!-- POPULATE -->
**JWT Issuer (`iss`):** `___`
**JWT Audience (`aud`):** `___`
**JWT Signing Algorithm:** `___` (e.g., RS256, ES256)
**JWKS Endpoint:** `___`
**Authorization Model:** `___`

---

## Database Schema Conventions

- Table names: `snake_case`, plural (e.g., `things`)
- Primary key: `uid UUID DEFAULT gen_random_uuid() PRIMARY KEY`
- Resource name: `name TEXT UNIQUE NOT NULL`
- Timestamps: `create_time TIMESTAMPTZ NOT NULL DEFAULT now()`, `update_time TIMESTAMPTZ NOT NULL DEFAULT now()`, `delete_time TIMESTAMPTZ`
- Etag: `etag TEXT NOT NULL DEFAULT gen_random_uuid()::text` — regenerated on every update
- `CHECK` constraints where appropriate
- All foreign keys: explicit `ON DELETE` behavior
- Indexes on `WHERE`, `ORDER BY`, `JOIN` columns
- Partial indexes for soft delete: `WHERE delete_time IS NULL`

---

## Additional Context

<!-- POPULATE -->
<!-- - "This service is consumed by a React frontend via grpc-web" -->
<!-- - "This runs on GKE with Cloud SQL" -->
<!-- - "Some operations take 5-30 minutes and must be LROs" -->
<!-- - "API keys are provisioned by an admin CLI" -->
<!-- - "We need idempotency keys on Create methods (AIP-155)" -->

---

## Initial Task

<!-- POPULATE -->
<!-- - "Scaffold the full project with working Get/List/Create for the Widget resource" -->
<!-- - "Write proto definitions with all buf.validate annotations and OpenAPI annotations" -->
<!-- - "Implement the auth interceptor with dual JWT + API key and rich error details" -->
<!-- - "Build the LRO manager, operations table, and operations server" -->
<!-- - "Set up the full o11y stack: OTel tracing, Prometheus metrics, slog with trace correlation" -->

`___`
