---
title: Go Coding Standards
impact: HIGH
impactDescription: foundational conventions for all Go code in the service
tags: go, stdlib, dependencies, idioms, error-handling, conventions
---

## Go Coding Standards

> Read this when: writing any Go code in the service — this is the baseline that all other rules build on.

### Dependency Philosophy

Prefer the Go standard library where it genuinely covers the need. Use well-maintained, permissively licensed third-party packages where rolling your own would be foolish or error-prone (see Approved Dependencies below). Every dependency must be justified. Do not reinvent JWT validation, UUID generation, or database drivers.

### Approved Third-Party Dependencies

Use the standard library except where these packages are justified:

| Package | Justification |
|---|---|
| `google.golang.org/grpc` | gRPC runtime — no stdlib alternative |
| `google.golang.org/protobuf` | Protobuf runtime — no stdlib alternative |
| `google.golang.org/genproto/googleapis/rpc/errdetails` | Google's rich error detail types |
| `github.com/grpc-ecosystem/grpc-gateway/v2` | JSON transcoding + OpenAPI generation |
| `github.com/grpc-ecosystem/go-grpc-middleware/v2` | Protovalidate interceptor for grpc-go |
| `buf.build/go/protovalidate` | Proto-based validation engine (replaces hand-written validation) |
| `github.com/golang-jwt/jwt/v5` | JWT: JWKS rotation, `kid` lookup, claims, clock skew. Security-critical; do not hand-roll. |
| `github.com/google/uuid` | UUID generation — no stdlib UUID. |
| `github.com/jackc/pgx/v5` | PostgreSQL: native driver for LISTEN/NOTIFY, pgtype, pool stats. |
| `github.com/golang-migrate/migrate/v4` | Database migrations |
| `go.opentelemetry.io/otel` | OTel SDK for tracing + metrics |
| `go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc` | OTLP trace exporter |
| `go.opentelemetry.io/otel/exporters/prometheus` | Prometheus metric scrape endpoint |
| `go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc` | Auto-instrumentation for gRPC |
| `github.com/stretchr/testify` | Assertions only (`assert`, `require`) — no suites. |

**Do NOT add** without explicit justification in a code comment: web frameworks (chi, gin, echo), config libraries (viper, envconfig), logging libraries (zerolog, zap), ORM/query builders.

### Go 1.25 / 1.26 Features to Adopt

| Feature | Version | Usage |
|---|---|---|
| `new(expr)` | 1.26 | Inline pointer init: `new(42)`, `new("active")` for proto builders, test fixtures, optional fields |
| Green Tea GC | 1.26 (default) | Free 10-40% GC overhead reduction. No code changes — just set `go 1.26` in `go.mod`. |
| `testing/synctest` | 1.25 (stable) | Test LRO workers, goroutine coordination, timer-based tests. Replaces `time.Sleep` in tests. |
| Container-aware GOMAXPROCS | 1.25 | Auto-detects cgroup CPU limits in containers. No `go.uber.org/automaxprocs` needed. |
| `go fix` modernizers | 1.26 | Run `go fix ./...` post-scaffolding to apply 1.26 idioms. |
| `GOEXPERIMENT=goroutineleakprofile` | 1.26 (experimental) | Enable in dev/staging for `/debug/pprof/goroutineleak` — catches goroutine leaks in LRO workers. |
| Stack-allocated slices | 1.26 | Compiler improvement — automatic, no code changes. |
| 30% faster cgo | 1.26 | Automatic runtime improvement. |

**Do NOT adopt** (yet): `GOEXPERIMENT=jsonv2` (still experimental, memory regression issues, grpc-gateway uses `protojson` anyway), `GOEXPERIMENT=simd` (irrelevant for CRUD services).

### General Conventions

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
