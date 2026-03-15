# gRPC Service Guidelines

**Version 2.0.0**

> **Note:**
> This document is mainly for agents and LLMs to follow when building,
> maintaining, or reviewing production gRPC services in Go. Humans may also
> find it useful, but guidance here is optimized for automation and consistency
> by AI-assisted workflows.

---

## Abstract

Production-grade gRPC service patterns in Go at Google L5+ standards. Covers
idiomatic Go, clean architecture, comprehensive error handling, observability,
and strict adherence to Google AIP (API Improvement Proposals). All code must
reflect production quality — no toy code, no shortcuts. This compiled reference
concatenates all individual rule files into a single authoritative document.

---

## Table of Contents

1. [Go Coding Standards](#1-go-coding-standards)
   - 1.1 [Dependency Philosophy](#11-dependency-philosophy)
   - 1.2 [Approved Third-Party Dependencies](#12-approved-third-party-dependencies)
   - 1.3 [Go 1.25 / 1.26 Features to Adopt](#13-go-125--126-features-to-adopt)
   - 1.4 [General Conventions](#14-general-conventions)
2. [Project Structure](#2-project-structure)
   - 2.1 [Directory Layout](#21-directory-layout)
   - 2.2 [Where Things Go](#22-where-things-go)
   - 2.3 [Adding a New Resource](#23-adding-a-new-resource)
3. [AIP Compliance Reference](#3-aip-compliance-reference)
   - 3.1 [Resource Design](#31-resource-design)
   - 3.2 [Standard Methods](#32-standard-methods)
   - 3.3 [Long-Running Operations](#33-long-running-operations)
   - 3.4 [Query Features](#34-query-features)
   - 3.5 [Field Behavior & Annotations](#35-field-behavior--annotations)
   - 3.6 [Standard Field Behavior Mapping](#36-standard-field-behavior-mapping)
   - 3.7 [Implementing a Method Checklist](#37-implementing-a-method-checklist)
4. [Proto Definitions & Validation](#4-proto-definitions--validation)
   - 4.1 [Proto File Conventions](#41-proto-file-conventions)
   - 4.2 [ID Model — No `uid` Field](#42-id-model--no-uid-field)
   - 4.3 [Resource Message Pattern](#43-resource-message-pattern)
   - 4.4 [Standard Field Behavior](#44-standard-field-behavior)
   - 4.5 [Protovalidate Integration](#45-protovalidate-integration)
   - 4.6 [Request Validation Patterns](#46-request-validation-patterns)
   - 4.7 [Standard Method Patterns](#47-standard-method-patterns)
   - 4.8 [Service Patterns](#48-service-patterns)
   - 4.9 [Custom Method Patterns](#49-custom-method-patterns)
   - 4.10 [Singleton Sub-Resource Pattern](#410-singleton-sub-resource-pattern)
   - 4.11 [Multi-Pattern Resources](#411-multi-pattern-resources)
   - 4.12 [API Linter Disable Patterns](#412-api-linter-disable-patterns)
   - 4.13 [Enum Patterns](#413-enum-patterns)
   - 4.14 [Domain-Specific Validation Examples](#414-domain-specific-validation-examples)
   - 4.15 [Verification Workflow](#415-verification-workflow)
   - 4.16 [Proto Coding Standards Summary](#416-proto-coding-standards-summary)
5. [Rich gRPC Error Details](#5-rich-grpc-error-details)
   - 5.1 [Core Rule](#51-core-rule)
   - 5.2 [Error Detail Types](#52-error-detail-types)
   - 5.3 [`internal/apierr` Package](#53-internalapierr-package)
   - 5.4 [Error Rules](#54-error-rules)
   - 5.5 [gRPC Code Mapping](#55-grpc-code-mapping)
   - 5.6 [Testing Error Details](#56-testing-error-details)
6. [Database — PostgreSQL, sqlc, Migrations](#6-database--postgresql-sqlc-migrations)
   - 6.1 [Schema Conventions](#61-schema-conventions)
   - 6.2 [pgx Native Driver](#62-pgx-native-driver)
   - 6.3 [sqlc Configuration](#63-sqlc-configuration)
   - 6.4 [sqlc Query Patterns](#64-sqlc-query-patterns)
   - 6.5 [Pagination](#65-pagination)
   - 6.6 [Transactions](#66-transactions)
   - 6.7 [Etag Validation](#67-etag-validation)
7. [Authentication — JWT + API Keys](#7-authentication--jwt--api-keys)
   - 7.1 [Dual Auth Pattern](#71-dual-auth-pattern)
   - 7.2 [Resolution Order](#72-resolution-order)
   - 7.3 [API Keys Table Schema](#73-api-keys-table-schema)
   - 7.4 [CallerInfo Context](#74-callerinfo-context)
   - 7.5 [Auth Metrics](#75-auth-metrics)
   - 7.6 [Configuration](#76-configuration)
   - 7.7 [Testing Auth](#77-testing-auth)
8. [Long-Running Operations — AIP-151/152/153](#8-long-running-operations--aip-151152153)
   - 8.1 [When to Use LROs](#81-when-to-use-lros)
   - 8.2 [AIP Compliance](#82-aip-compliance)
   - 8.3 [Proto Pattern](#83-proto-pattern)
   - 8.4 [Operations Table](#84-operations-table)
   - 8.5 [Implementation Architecture](#85-implementation-architecture)
   - 8.6 [Operation Name Pattern](#86-operation-name-pattern)
   - 8.7 [LRO Metrics](#87-lro-metrics)
   - 8.8 [LRO Logging](#88-lro-logging)
9. [Observability — Tracing, Metrics, Logging](#9-observability--tracing-metrics-logging)
   - 9.1 [Three-Signal Architecture](#91-three-signal-architecture)
   - 9.2 [`internal/o11y/` Package Structure](#92-internalo11y-package-structure)
   - 9.3 [Tracer Provider](#93-tracer-provider)
   - 9.4 [Prometheus Metrics + Exemplars](#94-prometheus-metrics--exemplars)
   - 9.5 [Trace-Correlating slog Handler](#95-trace-correlating-slog-handler)
   - 9.6 [gRPC Auto-Instrumentation](#96-grpc-auto-instrumentation)
   - 9.7 [slog Conventions](#97-slog-conventions)
   - 9.8 [Application Metrics](#98-application-metrics)
10. [Testing Patterns](#10-testing-patterns)
    - 10.1 [Conventions](#101-conventions)
    - 10.2 [Test Categories](#102-test-categories)
    - 10.3 [Verifying Error Details](#103-verifying-error-details)
    - 10.4 [Table-Driven Test Pattern](#104-table-driven-test-pattern)
    - 10.5 [Database Mocking](#105-database-mocking)
    - 10.6 [Integration Tests](#106-integration-tests)
    - 10.7 [testing/synctest for Concurrent Code](#107-testingsynctest-for-concurrent-code)
11. [Network Architecture — Three Ports](#11-network-architecture--three-ports)
    - 11.1 [Port Layout](#111-port-layout)
    - 11.2 [Rationale](#112-rationale)
    - 11.3 [Debug Mux](#113-debug-mux)
    - 11.4 [K8s Probe Configuration](#114-k8s-probe-configuration)
12. [OpenAPI Spec Generation](#12-openapi-spec-generation)
    - 12.1 [buf.gen.yaml Setup](#121-bufgenyaml-setup)
    - 12.2 [Service-Level Annotation](#122-service-level-annotation)
    - 12.3 [Per-RPC Annotation](#123-per-rpc-annotation)
    - 12.4 [Swagger UI](#124-swagger-ui)
13. [gRPC Health Service](#13-grpc-health-service)
    - 13.1 [Primary Health Mechanism](#131-primary-health-mechanism)
    - 13.2 [What to Check](#132-what-to-check)
    - 13.3 [HTTP Health Wrapper](#133-http-health-wrapper)
    - 13.4 [Graceful Shutdown](#134-graceful-shutdown)
14. [gRPC Reflection — Dev Builds Only](#14-grpc-reflection--dev-builds-only)
    - 14.1 [Why Gate Behind Build Tags](#141-why-gate-behind-build-tags)
    - 14.2 [Implementation](#142-implementation)
    - 14.3 [Usage](#143-usage)
    - 14.4 [Development Workflow](#144-development-workflow)
15. [References](#15-references)

---

## 1. Go Coding Standards

Foundational conventions for all Go code in the service — the baseline that all other rules build on.

### 1.1 Dependency Philosophy

Prefer the Go standard library where it genuinely covers the need. Use well-maintained, permissively licensed third-party packages where rolling your own would be foolish or error-prone (see Approved Dependencies below). Every dependency must be justified. Do not reinvent JWT validation, UUID generation, or database drivers.

### 1.2 Approved Third-Party Dependencies

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

### 1.3 Go 1.25 / 1.26 Features to Adopt

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

### 1.4 General Conventions

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

---

## 2. Project Structure

Standard directory layout for scaffolding and navigating the service.

### 2.1 Directory Layout

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
│   │   └── config.go                         # Env-based config using os.Getenv
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
│   │   │   └── operations.sql                # LRO persistence queries
│   │   ├── sqlc.yaml
│   │   └── generated/                        # sqlc output (do not edit)
│   │       ├── db.go
│   │       ├── models.go
│   │       ├── {resource}.sql.go
│   │       └── operations.sql.go
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
│   │   ├── operations_server.go              # google.longrunning.Operations impl
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

### 2.2 Where Things Go

| What | Where | Why |
|---|---|---|
| Proto definitions | `api/proto/{service}/v1/` | Versioned API contract, separate from implementation |
| Generated code | `gen/` | Never edit — regenerated by `buf generate` |
| Entrypoint | `cmd/server/main.go` | Wires all dependencies, starts 3 servers (gRPC, REST, debug) |
| Error constructors | `internal/apierr/` | Shared by all server methods, never inline status errors |
| SQL queries | `internal/db/queries/` | Raw SQL for sqlc to compile into type-safe Go |
| sqlc output | `internal/db/generated/` | Never edit — regenerated by `sqlc generate` |
| gRPC handlers | `internal/server/` | Thin layer that calls service layer, handles proto conversion |
| Business logic | `internal/service/` | Core logic, calls DB layer, records metrics |
| Proto converters | `internal/convert/` | Proto <-> DB model mapping, keeps server/service layers clean |
| Telemetry setup | `internal/o11y/` | Tracing, metrics, slog — initialized once in main |
| LRO lifecycle | `internal/lro/` | Manager, workers, reaper — all operation concerns |

### 2.3 Adding a New Resource

1. Create `api/proto/{service}/v1/{resource}.proto` — messages with `buf.validate` annotations
2. Add RPCs to `{service}_service.proto` — with `google.api.http` and `openapiv2_operation` annotations
3. Run `buf generate`
4. Create `internal/db/queries/{resource}.sql` — sqlc queries
5. Run `sqlc generate`
6. Create `internal/convert/{resource}.go` — proto <-> DB converters
7. Create `internal/service/{resource}_service.go` — business logic
8. Create `internal/server/{resource}_server.go` — gRPC handler
9. Register in `cmd/server/main.go`

---

## 3. AIP Compliance Reference

Ensures all API design follows Google API Improvement Proposals. Reference AIPs by number in code comments and PR descriptions.

### 3.1 Resource Design

| AIP | Topic | Key Requirement |
|---|---|---|
| AIP-121 | Resource-oriented design | APIs are modeled as resources with standard methods |
| AIP-122 | Resource names | Full resource name as `name` field (e.g., `projects/{project}/things/{thing}`) |
| AIP-123 | Resource types | Type string format: `{service}.googleapis.com/{ResourceType}` |
| AIP-148 | Standard fields | `create_time`, `update_time`, `delete_time`, `etag` on every resource. No `uid` — `name` is the sole external ID. |
| AIP-154 | Etags | Optimistic concurrency — etag regenerated on every update, checked before writes |
| AIP-164 | Soft delete | `delete_time` set on delete, `Undelete` method to restore, `expire_time` for permanent cleanup |

### 3.2 Standard Methods

| AIP | Method | HTTP | Key Details |
|---|---|---|---|
| AIP-131 | Get | `GET /v1/{name=resources/*}` | Return single resource by name |
| AIP-132 | List | `GET /v1/{parent=...}/resources` | Pagination via `page_size`/`page_token`, `next_page_token` |
| AIP-133 | Create | `POST /v1/{parent=...}/resources` | Optional `resource_id` for client-assigned names |
| AIP-134 | Update | `PATCH /v1/{resource.name=...}` | `update_mask` (FieldMask) — only update specified fields |
| AIP-135 | Delete | `DELETE /v1/{name=resources/*}` | Soft delete by default, optional `force` for hard delete |
| AIP-127 | HTTP transcoding | — | `google.api.http` annotations on every RPC |

### 3.3 Long-Running Operations

| AIP | Topic | Key Requirement |
|---|---|---|
| AIP-151 | LRO definition | Method returns `google.longrunning.Operation` with `metadata` and `response` types |
| AIP-152 | Operations service | Implement `GetOperation`, `ListOperations`, `DeleteOperation`, `CancelOperation`, `WaitOperation` |
| AIP-153 | Polling lifecycle | Clients poll `GetOperation` with exponential backoff; `done` field indicates completion |

### 3.4 Query Features

| AIP | Topic | Key Requirement |
|---|---|---|
| AIP-140 | FieldMasks | Partial updates — only modify fields in the mask |
| AIP-158 | Pagination | Cursor-based with opaque, base64-encoded page tokens (never raw offsets) |
| AIP-160 | Filtering | CEL-like filter expressions on List methods |
| AIP-161 | Ordering | `order_by` field on List methods |

### 3.5 Field Behavior & Annotations

| AIP | Topic | Key Requirement |
|---|---|---|
| AIP-203 | Field behavior | `google.api.field_behavior` annotation on **every** field — REQUIRED, OUTPUT_ONLY, IMMUTABLE, OPTIONAL. No exceptions. |

### 3.6 Standard Field Behavior Mapping

| Field | Behavior | Notes |
|---|---|---|
| `name` | IDENTIFIER | Sole external ID, server-assigned from `parent` + `resource_id` |
| `display_name` | REQUIRED or OPTIONAL | Mutable, human-readable |
| `description` | OPTIONAL | Mutable |
| `create_time` | OUTPUT_ONLY | Server-assigned at creation |
| `update_time` | OUTPUT_ONLY | Updated by server on every write |
| `delete_time` | OUTPUT_ONLY | Set on soft delete, cleared on undelete |
| `purge_time` | OUTPUT_ONLY | Set on soft delete, ~30 days after |
| `etag` | OUTPUT_ONLY | Regenerated on every update |
| `annotations` | OPTIONAL | Free-form key-value pairs |
| `state` | OUTPUT_ONLY | Lifecycle enum (ACTIVE, DELETE_REQUESTED) |

### 3.7 Implementing a Method Checklist

When implementing any standard method, verify:

- [ ] `google.api.http` annotation present (AIP-127)
- [ ] `google.api.field_behavior` on every field in request and response (AIP-203)
- [ ] `buf.validate` constraints on every request field
- [ ] `openapiv2_operation` with `summary` and `tags`
- [ ] Resource name validated via `parseName` helper (AIP-122)
- [ ] Etag checked on updates if client provides one (AIP-154)
- [ ] Soft delete respects `delete_time` semantics (AIP-164)
- [ ] FieldMask honored on updates (AIP-140)
- [ ] Pagination uses cursor tokens, not offsets (AIP-158)
- [ ] Error responses use rich details via `internal/apierr`

---

## 4. Proto Definitions & Validation

Foundation for API contract and request validation. All examples are drawn from
the Pivox codebase and represent the canonical patterns.

### 4.1 Proto File Conventions

- `syntax = "proto3"` — do NOT use editions yet (ecosystem not ready, especially grpc-gateway)
- Only specify `option go_package`; use buf managed mode for other languages
- Proto package naming: `pivox.{domain}.v1` (e.g. `pivox.api.v1`, `pivox.iam.v1`)
- Go package option: `option go_package = "pivox/{domain}/v1;{domain}v1";`
- `google.api.field_behavior` annotations on **every** field — no exceptions (AIP-203)
- `google.api.http` annotations on every RPC for JSON transcoding (AIP-127)
- `google.api.resource` annotations on every resource message
- `google.api.resource_reference` on every field that references another resource
- `buf.validate` annotations on every request message field

### 4.2 ID Model — No `uid` Field

Resources do NOT have a separate `uid` field. Every resource has exactly one
external identifier in `name`. The `name` field uses `IDENTIFIER` behavior
(not `OUTPUT_ONLY` + `IMMUTABLE`).

| Resource | `name` pattern | ID type |
|----------|---------------|---------|
| Organization | `organizations/{slug}` | Immutable slug |
| Project | `organizations/{slug}/projects/{slug}` | Immutable slug |
| Tag Key | `organizations/{org}/tagKeys/{uuid}` | UUID |
| User | `organizations/{org}/users/{uuid}` | UUID |
| Group | `organizations/{org}/groups/{uuid}` | UUID |
| Role | `organizations/{org}/roles/{uuid}` | UUID |
| API Key | `organizations/{org}/keys/{key_id}` | String key_id |
| Invitation | `organizations/{org}/invitations/{uuid}` | UUID |

Do NOT import `google/api/field_info.proto` or use `(google.api.field_info).format = UUID4`.

### 4.3 Resource Message Pattern

```protobuf
message Group {
  option (google.api.resource) = {
    type: "pivox.iam/Group"
    pattern: "organizations/{organization}/groups/{group}"
    plural: "groups"
    singular: "group"
  };

  // The resource name of the group.
  // Format: `organizations/{organization}/groups/{group}`
  string name = 1 [(google.api.field_behavior) = IDENTIFIER];

  // Required. A human-readable name for the group.
  string display_name = 2 [
    (google.api.field_behavior) = REQUIRED,
    (buf.validate.field).string = {min_len: 1, max_len: 63}
  ];

  // Optional. A longer description.
  string description = 3 [
    (google.api.field_behavior) = OPTIONAL,
    (buf.validate.field).string.max_len = 256
  ];

  // Output only. Timestamp when the group was created.
  google.protobuf.Timestamp create_time = 4
      [(google.api.field_behavior) = OUTPUT_ONLY];

  // Output only. Timestamp when the group was last modified.
  google.protobuf.Timestamp update_time = 5
      [(google.api.field_behavior) = OUTPUT_ONLY];

  // Output only. Timestamp when the group was soft-deleted.
  google.protobuf.Timestamp delete_time = 6
      [(google.api.field_behavior) = OUTPUT_ONLY];

  // Output only. Timestamp when the group will be permanently purged.
  google.protobuf.Timestamp purge_time = 7
      [(google.api.field_behavior) = OUTPUT_ONLY];

  // Output only. A checksum for optimistic concurrency control.
  string etag = 8 [(google.api.field_behavior) = OUTPUT_ONLY];

  // Optional. Free-form annotations.
  map<string, string> annotations = 9 [(google.api.field_behavior) = OPTIONAL];
}
```

**Key points:**
- `name` uses `IDENTIFIER` — not `OUTPUT_ONLY` + `IMMUTABLE`
- No `uid` field
- Output-only fields do NOT need `buf.validate` annotations (no `IGNORE_ALWAYS`)
- `etag` is `OUTPUT_ONLY` (server-generated), not `OPTIONAL`
- Soft-delete resources include `delete_time` and `purge_time`
- `annotations` (not `labels`) for free-form key-value metadata

### 4.4 Standard Field Behavior

| Field | Behavior | Notes |
|---|---|---|
| `name` | IDENTIFIER | Sole external ID, server-assigned from parent + id |
| `display_name` | REQUIRED or OPTIONAL | Mutable, human-readable |
| `description` | OPTIONAL | Mutable |
| `create_time` | OUTPUT_ONLY | Server-assigned at creation |
| `update_time` | OUTPUT_ONLY | Updated by server on every write |
| `delete_time` | OUTPUT_ONLY | Set on soft delete |
| `purge_time` | OUTPUT_ONLY | Set on soft delete, ~30 days after |
| `etag` | OUTPUT_ONLY | Regenerated on every update |
| `annotations` | OPTIONAL | Free-form key-value pairs |
| `state` | OUTPUT_ONLY | Lifecycle enum (ACTIVE, DELETE_REQUESTED) |

### 4.5 Protovalidate Integration

All request messages MUST have `buf.validate` annotations on every field. Validation is enforced by a gRPC interceptor — it runs before your handler.

**Setup:**

```go
import (
    "buf.build/go/protovalidate"
    protovalidate_middleware "github.com/grpc-ecosystem/go-grpc-middleware/v2/interceptors/protovalidate"
)

validator, err := protovalidate.New()
srv := grpc.NewServer(
    grpc.ChainUnaryInterceptor(
        protovalidate_middleware.UnaryServerInterceptor(validator),
    ),
)
```

**Division of Labor:**

| Responsibility | Owner | Examples |
|---|---|---|
| Declarative schema constraints | Protovalidate (proto annotations) | Field format, length, required, patterns |
| Business logic errors | `internal/apierr` package | Etag mismatch, wrong state, not found, already exists |

### 4.6 Request Validation Patterns

**Resource name fields — use CEL expressions:**

```protobuf
string name = 1 [
  (google.api.field_behavior) = REQUIRED,
  (google.api.resource_reference) = {type: "pivox.iam/Group"},
  (buf.validate.field).cel = {
    id: "required"
    message: "value is required"
    expression: "this.size() > 0"
  }
];
```

**Parent fields — use `child_type` reference:**

```protobuf
string parent = 1 [
  (google.api.field_behavior) = REQUIRED,
  (google.api.resource_reference) = {child_type: "pivox.iam/Group"},
  (buf.validate.field).cel = {
    id: "required"
    message: "value is required"
    expression: "this.size() > 0"
  }
];
```

Always use `resource_reference` with either `type` (for direct references) or
`child_type` (for parent fields). Always pair with a CEL `required` check.

### 4.7 Standard Method Patterns

**Create Request:**

```protobuf
message CreateGroupRequest {
  string parent = 1 [
    (google.api.field_behavior) = REQUIRED,
    (google.api.resource_reference) = {child_type: "pivox.iam/Group"},
    (buf.validate.field).cel = {
      id: "required"
      message: "value is required"
      expression: "this.size() > 0"
    }
  ];

  Group group = 2 [
    (google.api.field_behavior) = REQUIRED,
    (buf.validate.field).required = true
  ];

  // Optional. Server-generated ID if not provided.
  string group_id = 3 [(google.api.field_behavior) = OPTIONAL];
}
```

The `{resource}_id` field is always OPTIONAL (server generates UUID if omitted).
Method signature includes the id: `option (google.api.method_signature) = "parent,group,group_id";`

**List Request:**

```protobuf
message ListGroupsRequest {
  string parent = 1 [
    (google.api.field_behavior) = REQUIRED,
    (google.api.resource_reference) = {child_type: "pivox.iam/Group"},
    (buf.validate.field).cel = {
      id: "required"
      message: "value is required"
      expression: "this.size() > 0"
    }
  ];

  int32 page_size = 2 [
    (google.api.field_behavior) = OPTIONAL,
    (buf.validate.field).int32 = {gte: 0, lte: 1000}
  ];

  string page_token = 3 [(google.api.field_behavior) = OPTIONAL];
  string filter = 4 [(google.api.field_behavior) = OPTIONAL];
  string order_by = 5 [(google.api.field_behavior) = OPTIONAL];
  bool show_deleted = 6 [(google.api.field_behavior) = OPTIONAL];
}
```

**Update Request:**

```protobuf
message UpdateGroupRequest {
  Group group = 1 [
    (google.api.field_behavior) = REQUIRED,
    (buf.validate.field).required = true
  ];

  google.protobuf.FieldMask update_mask = 2
      [(google.api.field_behavior) = OPTIONAL];
}
```

**Delete Request:**

```protobuf
message DeleteGroupRequest {
  string name = 1 [
    (google.api.field_behavior) = REQUIRED,
    (google.api.resource_reference) = {type: "pivox.iam/Group"},
    (buf.validate.field).cel = {
      id: "required"
      message: "value is required"
      expression: "this.size() > 0"
    }
  ];

  string etag = 2 [(google.api.field_behavior) = OPTIONAL];
}
```

### 4.8 Service Patterns

**Full CRUD service:**

```protobuf
service Groups {
  option (google.api.default_host) = "api.pivox.io";

  rpc GetGroup(GetGroupRequest) returns (Group) {
    option (google.api.http) = {get: "/v1/{name=organizations/*/groups/*}"};
    option (google.api.method_signature) = "name";
  }

  rpc ListGroups(ListGroupsRequest) returns (ListGroupsResponse) {
    option (google.api.http) = {get: "/v1/{parent=organizations/*}/groups"};
    option (google.api.method_signature) = "parent";
  }

  rpc CreateGroup(CreateGroupRequest) returns (Group) {
    option (google.api.http) = {
      post: "/v1/{parent=organizations/*}/groups"
      body: "group"
    };
    option (google.api.method_signature) = "parent,group,group_id";
  }

  rpc UpdateGroup(UpdateGroupRequest) returns (Group) {
    option (google.api.http) = {
      patch: "/v1/{group.name=organizations/*/groups/*}"
      body: "group"
    };
    option (google.api.method_signature) = "group,update_mask";
  }

  rpc DeleteGroup(DeleteGroupRequest) returns (Group) {
    option (google.api.http) = {
      delete: "/v1/{name=organizations/*/groups/*}"
    };
    option (google.api.method_signature) = "name";
  }
}
```

**Read-only service (e.g. Firebase-synced users, system permissions):**

```protobuf
service Users {
  option (google.api.default_host) = "api.pivox.io";

  rpc GetUser(GetUserRequest) returns (User) {
    option (google.api.http) = {get: "/v1/{name=organizations/*/users/*}"};
    option (google.api.method_signature) = "name";
  }

  rpc ListUsers(ListUsersRequest) returns (ListUsersResponse) {
    option (google.api.http) = {get: "/v1/{parent=organizations/*}/users"};
    option (google.api.method_signature) = "parent";
  }
}
```

Read-only resources have all fields as OUTPUT_ONLY (except `name` which is IDENTIFIER).

### 4.9 Custom Method Patterns

**Membership management (Add/Remove/List members):**

URI suffix must match the RPC method name (api-linter rule `core::0136::http-uri-suffix`).

```protobuf
rpc AddGroupMembers(AddGroupMembersRequest)
    returns (AddGroupMembersResponse) {
  option (google.api.http) = {
    post: "/v1/{group=organizations/*/groups/*}:addGroupMembers"
    body: "*"
  };
  option (google.api.method_signature) = "group,members";
}

rpc RemoveGroupMembers(RemoveGroupMembersRequest)
    returns (RemoveGroupMembersResponse) {
  option (google.api.http) = {
    post: "/v1/{group=organizations/*/groups/*}:removeGroupMembers"
    body: "*"
  };
  option (google.api.method_signature) = "group,members";
}

rpc ListGroupMembers(ListGroupMembersRequest)
    returns (ListGroupMembersResponse) {
  option (google.api.http) = {
    get: "/v1/{group=organizations/*/groups/*}/members"
  };
  option (google.api.method_signature) = "group";
}
```

Membership request patterns:

```protobuf
message AddGroupMembersRequest {
  string group = 1 [
    (google.api.field_behavior) = REQUIRED,
    (google.api.resource_reference) = {type: "pivox.iam/Group"},
    (buf.validate.field).cel = {
      id: "required"
      message: "value is required"
      expression: "this.size() > 0"
    }
  ];

  repeated string members = 2 [
    (google.api.field_behavior) = REQUIRED,
    (buf.validate.field).repeated = {min_items: 1, max_items: 100}
  ];
}
```

**Polymorphic member references:** When a member can be a user OR a group
(like RoleMember), use a plain `string member` without `resource_reference`
since it's polymorphic. Server validates the resource name format.

**State transition methods (Accept/Decline):**

```protobuf
rpc AcceptInvitation(AcceptInvitationRequest)
    returns (AcceptInvitationResponse) {
  option (google.api.http) = {
    post: "/v1/{name=organizations/*/invitations/*}:accept"
    body: "*"
  };
  option (google.api.method_signature) = "name";
}
```

### 4.10 Singleton Sub-Resource Pattern

For one-per-parent resources (e.g. InvitationPolicy per org):

```protobuf
message InvitationPolicy {
  option (google.api.resource) = {
    type: "pivox.api/InvitationPolicy"
    pattern: "organizations/{organization}/invitationPolicy"
    plural: "invitationPolicies"
    singular: "invitationPolicy"
  };

  string name = 1 [(google.api.field_behavior) = IDENTIFIER];
  // ... fields ...
}
```

Singletons have Get + Update RPCs (no Create/Delete/List):

```protobuf
rpc GetInvitationPolicy(GetInvitationPolicyRequest)
    returns (InvitationPolicy) {
  option (google.api.http) = {
    get: "/v1/{name=organizations/*/invitationPolicy}"
  };
}

rpc UpdateInvitationPolicy(UpdateInvitationPolicyRequest)
    returns (InvitationPolicy) {
  option (google.api.http) = {
    patch: "/v1/{invitation_policy.name=organizations/*/invitationPolicy}"
    body: "invitation_policy"
  };
}
```

### 4.11 Multi-Pattern Resources

Some resources can exist under multiple parents (e.g. TagKeys under
orgs or projects):

```protobuf
message TagKey {
  option (google.api.resource) = {
    type: "pivox.api/TagKey"
    pattern: "organizations/{organization}/tagKeys/{tag_key}"
    pattern: "organizations/{organization}/projects/{project}/tagKeys/{tag_key}"
    plural: "tagKeys"
    singular: "tagKey"
  };
}
```

RPCs use `additional_bindings` for the second pattern:

```protobuf
rpc GetTagKey(GetTagKeyRequest) returns (TagKey) {
  option (google.api.http) = {
    get: "/v1/{name=organizations/*/tagKeys/*}"
    additional_bindings {
      get: "/v1/{name=organizations/*/projects/*/tagKeys/*}"
    }
  };
}
```

### 4.12 API Linter Disable Patterns

When ListMembers RPCs use a non-standard parent field (e.g. `group` instead of
`parent`), disable the relevant linter rules on the **message**, not the RPC:

```protobuf
// (-- api-linter: core::0132::request-parent-required=disabled
//     aip.dev/not-precedent: ListGroupMembers uses `group` as the parent-like field. --)
// (-- api-linter: core::0132::request-required-fields=disabled
//     aip.dev/not-precedent: ListGroupMembers uses `group` as the parent-like field. --)
// (-- api-linter: core::0132::request-unknown-fields=disabled
//     aip.dev/not-precedent: ListGroupMembers uses `group` as the parent-like field. --)
message ListGroupMembersRequest { ... }

// (-- api-linter: core::0132::response-unknown-fields=disabled
//     aip.dev/not-precedent: Response contains GroupMember, a custom sub-resource. --)
message ListGroupMembersResponse { ... }
```

Also disable on SetIamPolicy RPCs which return Policy instead of the standard response:

```protobuf
// (-- api-linter: core::0136::response-message-name=disabled
//     aip.dev/not-precedent: SetIamPolicy returns Policy per IAM convention. --)
rpc SetIamPolicy(...) returns (pivox.iam.v1.Policy) { ... }
```

### 4.13 Enum Patterns

Enums nested inside the resource message they belong to:

```protobuf
message Organization {
  enum State {
    STATE_UNSPECIFIED = 0;
    ACTIVE = 1;
    DELETE_REQUESTED = 2;
  }

  State state = 4 [(google.api.field_behavior) = OUTPUT_ONLY];
}
```

Standalone enums (shared across resources) at package level:

```protobuf
enum Aggregation {
  AGGREGATION_UNSPECIFIED = 0;
  AGGREGATION_COUNT = 1;
  AGGREGATION_SUM = 2;
}
```

### 4.14 Domain-Specific Validation Examples

```protobuf
// Email — use CEL for custom validation
string email = 2 [
  (google.api.field_behavior) = REQUIRED,
  (google.api.field_behavior) = IMMUTABLE,
  (buf.validate.field).cel = {
    id: "valid_email"
    message: "must be a valid email address"
    expression: "this.matches('^[^@]+@[^@]+\\\\.[^@]+$')"
  }
];

// Domain name
string domain = 3 [
  (google.api.field_behavior) = IMMUTABLE,
  (buf.validate.field).cel = {
    id: "valid_domain"
    message: "must be a valid domain name"
    expression: "this.matches('^([a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?\\\\.)+[a-zA-Z]{2,}$')"
  }
];

// Slug/ID pattern
string organization_id = 2 [
  (google.api.field_behavior) = OPTIONAL,
  (buf.validate.field).string = {pattern: "^[a-z0-9-]+$"}
];

// Range constraints
int32 page_size = 2 [
  (google.api.field_behavior) = OPTIONAL,
  (buf.validate.field).int32 = {gte: 0, lte: 1000}
];

// Repeated min/max (membership operations)
repeated string members = 2 [
  (google.api.field_behavior) = REQUIRED,
  (buf.validate.field).repeated = {min_items: 1, max_items: 100}
];

// String length constraints
string display_name = 2 [
  (google.api.field_behavior) = REQUIRED,
  (buf.validate.field).string = {min_len: 1, max_len: 63}
];

string description = 3 [
  (google.api.field_behavior) = OPTIONAL,
  (buf.validate.field).string.max_len = 256
];
```

### 4.15 Verification Workflow

After writing or modifying protos:

```bash
# 1. Build
buf build

# 2. Generate Go code
buf generate

# 3. Lint new protos
api-linter --config api/proto/api-linter.yaml \
  --proto-path api/proto \
  api/proto/pivox/iam/v1/groups.proto \
  --output-format yaml

# 4. Verify no uid fields remain
grep -r "string uid" api/proto/pivox/ || echo "Clean"
grep -r "UUID4" api/proto/pivox/ || echo "Clean"
```

All four must pass before considering the proto work complete.

### 4.16 Proto Coding Standards Summary

- `syntax = "proto3"` (editions when grpc-gateway supports it)
- Only `option go_package` in proto files
- All fields must have `google.api.field_behavior` — no exceptions (AIP-203)
- All request message fields must have `buf.validate` annotations — no exceptions
- All RPCs must have `google.api.http` annotations (AIP-127)
- All resource messages must have `google.api.resource` with type and pattern
- All resource reference fields must have `google.api.resource_reference`
- No `uid` field on any resource — `name` is the sole external identifier
- No `google/api/field_info.proto` import
- LRO methods must include `google.longrunning.operation_info` annotation
- API linter disables go on messages, not RPCs (for request/response rules)
- URI suffix on custom methods must match the RPC name

---

## 5. Rich gRPC Error Details

Ensures machine-readable errors for all API consumers.

### 5.1 Core Rule

**Every** RPC error MUST use Google's richer error model. **Never** return a bare `status.Errorf(codes.X, "message")`. Always attach structured error details via `internal/apierr` package helpers.

### 5.2 Error Detail Types

| Error Detail Type | When to Use | Example |
|---|---|---|
| `errdetails.BadRequest` | Field validation failures | `display_name` exceeds max length |
| `errdetails.PreconditionFailure` | Precondition not met | Etag mismatch, resource in wrong state |
| `errdetails.ResourceInfo` | Error relates to a specific resource | Not found, already exists, deleted |
| `errdetails.ErrorInfo` | Machine-readable error identity | Reason code clients can switch on |
| `errdetails.QuotaFailure` | Rate limit or quota exceeded | API key rate limit hit |
| `errdetails.RetryInfo` | Client should retry after delay | Transient failure, rate limiting |
| `errdetails.RequestInfo` | Request ID for debugging | Traceability for support |
| `errdetails.DebugInfo` | Stack traces (internal debug) | **Non-production only** |
| `errdetails.Help` | Link to documentation | API docs for violated constraint |

### 5.3 `internal/apierr` Package

All error constructors live here. Never construct status errors inline in server methods.

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
            Reason: "RESOURCE_NOT_FOUND",
            Domain: "your.service.domain", // POPULATE
            Metadata: map[string]string{
                "resource_type": resourceType,
                "resource_name": resourceName,
            },
        },
    )
    return st.Err()
}

func AlreadyExists(resourceType, resourceName string) error {
    st := status.New(codes.AlreadyExists, fmt.Sprintf("%s %q already exists", resourceType, resourceName))
    st, _ = st.WithDetails(
        &errdetails.ResourceInfo{
            ResourceType: resourceType,
            ResourceName: resourceName,
        },
    )
    return st.Err()
}

func InvalidArgument(violations ...*errdetails.BadRequest_FieldViolation) error {
    st := status.New(codes.InvalidArgument, "one or more fields have invalid values")
    st, _ = st.WithDetails(
        &errdetails.BadRequest{FieldViolations: violations},
    )
    return st.Err()
}

func FieldViolation(field, description string) *errdetails.BadRequest_FieldViolation {
    return &errdetails.BadRequest_FieldViolation{
        Field:       field,
        Description: description,
    }
}

func EtagMismatch(resourceName, expected, actual string) error {
    st := status.New(codes.FailedPrecondition, "etag mismatch")
    st, _ = st.WithDetails(
        &errdetails.PreconditionFailure{
            Violations: []*errdetails.PreconditionFailure_Violation{{
                Type:        "ETAG",
                Subject:     resourceName,
                Description: fmt.Sprintf("expected etag %q but resource has %q", expected, actual),
            }},
        },
    )
    return st.Err()
}

func QuotaExceeded(subject, description string, retryDelay time.Duration) error {
    st := status.New(codes.ResourceExhausted, "quota exceeded")
    details := []proto.Message{
        &errdetails.QuotaFailure{
            Violations: []*errdetails.QuotaFailure_Violation{{
                Subject:     subject,
                Description: description,
            }},
        },
    }
    if retryDelay > 0 {
        details = append(details, &errdetails.RetryInfo{
            RetryDelay: durationpb.New(retryDelay),
        })
    }
    st, _ = st.WithDetails(details...)
    return st.Err()
}
```

### 5.4 Error Rules

1. **Never return bare status errors.** Always use `internal/apierr` helpers.
2. **Always include `ResourceInfo`** on resource-specific errors (NotFound, AlreadyExists, PermissionDenied).
3. **Always include `ErrorInfo`** with a machine-readable `Reason` string (e.g., `"RESOURCE_NOT_FOUND"`, `"ETAG_MISMATCH"`, `"API_KEY_REVOKED"`). This is what clients switch on.
4. **Always include `BadRequest.FieldViolation`** for every individual field that fails business-logic validation — not one error for the whole request. (Protovalidate handles schema validation automatically via interceptor.)
5. **Always include `PreconditionFailure`** for etag mismatches, state violations, or conditional failures.
6. **Include `RetryInfo`** when the client should retry (rate limits, transient failures).
7. **Never include `DebugInfo`** in production. Gate behind build tag or env flag.
8. **Error details serialize through grpc-gateway** into the HTTP `details` array automatically.

### 5.5 gRPC Code Mapping

| Scenario | gRPC Code | Required Details |
|---|---|---|
| Field validation failure | `InvalidArgument` | `BadRequest` with field violations |
| Resource not found | `NotFound` | `ResourceInfo`, `ErrorInfo` |
| Resource already exists | `AlreadyExists` | `ResourceInfo` |
| Etag mismatch | `FailedPrecondition` | `PreconditionFailure` |
| Resource in wrong state | `FailedPrecondition` | `PreconditionFailure`, `ErrorInfo` |
| Missing auth credentials | `Unauthenticated` | `ErrorInfo` with reason |
| Insufficient permissions | `PermissionDenied` | `ErrorInfo`, `ResourceInfo` |
| Rate limit / quota exceeded | `ResourceExhausted` | `QuotaFailure`, `RetryInfo` |
| Conflict (concurrent write) | `Aborted` | `ErrorInfo`, `ResourceInfo` |
| Client cancelled | `Cancelled` | — |
| Deadline exceeded | `DeadlineExceeded` | `RetryInfo` if retriable |
| Internal server error | `Internal` | `ErrorInfo`, `RequestInfo` (no stack traces) |
| Unimplemented method | `Unimplemented` | — |
| Upstream unavailable | `Unavailable` | `RetryInfo` |

### 5.6 Testing Error Details

Always verify error detail types and values:

```go
func TestCreateThing_AlreadyExists(t *testing.T) {
    // ... set up duplicate ...
    _, err := svc.CreateThing(ctx, req)
    require.Error(t, err)

    st, ok := status.FromError(err)
    require.True(t, ok)
    assert.Equal(t, codes.AlreadyExists, st.Code())

    details := st.Details()
    require.Len(t, details, 1)

    ri, ok := details[0].(*errdetails.ResourceInfo)
    require.True(t, ok)
    assert.Equal(t, "Thing", ri.ResourceType)
    assert.Contains(t, ri.ResourceName, "things/")
}
```

---

## 6. Database — PostgreSQL, sqlc, Migrations

Ensures correct schema design and type-safe queries.

### 6.1 Schema Conventions

- Table names: `snake_case`, plural (e.g., `things`)
- Primary key: `uid UUID DEFAULT gen_random_uuid() PRIMARY KEY`
- Resource name: `name TEXT UNIQUE NOT NULL` (AIP-122 full resource name)
- Timestamps: `create_time TIMESTAMPTZ NOT NULL DEFAULT now()`, `update_time TIMESTAMPTZ NOT NULL DEFAULT now()`, `delete_time TIMESTAMPTZ` (nullable for soft delete)
- Etag: `etag TEXT NOT NULL DEFAULT gen_random_uuid()::text` — regenerated on every update
- `CHECK` constraints where appropriate (e.g., enum-like state columns)
- All foreign keys must have explicit `ON DELETE` behavior
- Indexes on any column used in `WHERE`, `ORDER BY`, or `JOIN`
- Partial indexes for soft delete: `WHERE delete_time IS NULL`

**Example Migration:**

```sql
-- 000001_init.up.sql
CREATE TABLE things (
    uid           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name          TEXT UNIQUE NOT NULL,
    display_name  TEXT NOT NULL DEFAULT '',
    state         TEXT NOT NULL DEFAULT 'ACTIVE' CHECK (state IN ('ACTIVE', 'ARCHIVED')),
    create_time   TIMESTAMPTZ NOT NULL DEFAULT now(),
    update_time   TIMESTAMPTZ NOT NULL DEFAULT now(),
    delete_time   TIMESTAMPTZ,
    etag          TEXT NOT NULL DEFAULT gen_random_uuid()::text,
    -- domain fields...
);

CREATE INDEX idx_things_active ON things (name) WHERE delete_time IS NULL;
CREATE INDEX idx_things_parent ON things (split_part(name, '/', 1), split_part(name, '/', 2)) WHERE delete_time IS NULL;
```

### 6.2 pgx Native Driver

Use `pgx/v5` natively (not through `database/sql` stdlib interface). Reasons:
- `LISTEN/NOTIFY` for `WaitOperation` (LRO)
- `pgtype` for proper UUID, timestamptz, jsonb handling
- Connection pool stats for observability metrics
- Better performance (no `database/sql` abstraction overhead)

### 6.3 sqlc Configuration

```yaml
# internal/db/sqlc.yaml
version: "2"
sql:
  - engine: "postgresql"
    queries: "queries/"
    schema: "migrations/"
    gen:
      go:
        package: "db"
        out: "generated"
        sql_package: "pgx/v5"
        emit_json_tags: true
        emit_interface: true        # For mocking in tests
        emit_empty_slices: true     # Return [] not null for empty lists
        overrides:
          - db_type: "uuid"
            go_type: "github.com/google/uuid.UUID"
          - db_type: "timestamptz"
            go_type: "time.Time"
          - db_type: "jsonb"
            go_type: "json.RawMessage"
            import: "encoding/json"
```

### 6.4 sqlc Query Patterns

```sql
-- name: GetThing :one
SELECT * FROM things
WHERE name = $1 AND delete_time IS NULL;

-- name: ListThings :many
SELECT * FROM things
WHERE split_part(name, '/', 1) || '/' || split_part(name, '/', 2) = $1
  AND delete_time IS NULL
  AND (sqlc.narg('cursor')::text IS NULL OR name > sqlc.narg('cursor'))
ORDER BY name ASC
LIMIT $2;

-- name: CreateThing :one
INSERT INTO things (name, display_name)
VALUES ($1, $2)
RETURNING *;

-- name: UpdateThing :one
UPDATE things
SET display_name = COALESCE(sqlc.narg('display_name'), display_name),
    update_time = now(),
    etag = gen_random_uuid()::text
WHERE name = $1 AND delete_time IS NULL
RETURNING *;

-- name: SoftDeleteThing :one
UPDATE things
SET delete_time = now(), update_time = now(), etag = gen_random_uuid()::text
WHERE name = $1 AND delete_time IS NULL
RETURNING *;

-- name: UndeleteThing :one
UPDATE things
SET delete_time = NULL, update_time = now(), etag = gen_random_uuid()::text
WHERE name = $1 AND delete_time IS NOT NULL
RETURNING *;

-- name: HardDeleteThing :exec
DELETE FROM things WHERE name = $1;

-- name: CountThings :one
SELECT count(*) FROM things
WHERE split_part(name, '/', 1) || '/' || split_part(name, '/', 2) = $1
  AND delete_time IS NULL;
```

### 6.5 Pagination

- **Cursor-based** with opaque, base64-encoded page tokens (never raw offsets)
- Page token encodes the last resource name from the previous page
- Default page size: 25. Max: 1000. If 0 or unset, use default.
- Query fetches `page_size + 1` rows to detect `next_page_token`

```go
func encodePageToken(cursor string) string {
    return base64.StdEncoding.EncodeToString([]byte(cursor))
}

func decodePageToken(token string) (string, error) {
    b, err := base64.StdEncoding.DecodeString(token)
    if err != nil {
        return "", apierr.InvalidArgument(apierr.FieldViolation("page_token", "invalid page token"))
    }
    return string(b), nil
}
```

### 6.6 Transactions

Use `pgx` transactions for multi-table writes:

```go
tx, err := pool.Begin(ctx)
if err != nil {
    return fmt.Errorf("begin tx: %w", err)
}
defer tx.Rollback(ctx) // no-op if committed

qtx := db.New(tx)
// ... multiple queries on qtx ...

if err := tx.Commit(ctx); err != nil {
    return fmt.Errorf("commit tx: %w", err)
}
```

### 6.7 Etag Validation

Before updating, check the etag if the client provided one:

```go
if req.GetThing().GetEtag() != "" && req.GetThing().GetEtag() != existing.Etag {
    return nil, apierr.EtagMismatch(existing.Name, req.GetThing().GetEtag(), existing.Etag)
}
```

---

## 7. Authentication — JWT + API Keys

Secures service endpoints with dual auth pattern.

### 7.1 Dual Auth Pattern

The service supports two authentication methods, resolved at the gRPC interceptor level:

**1. JWT Bearer Tokens (User-facing)**

- Sent via `Authorization: Bearer <token>` header (gRPC metadata)
- Validated using `github.com/golang-jwt/jwt/v5`
- Validate claims: `iss`, `aud`, `exp`, `nbf`
- Extract subject (`sub`) and roles/scopes from claims
- JWKS fetching: cache keys in-memory with TTL, refresh on unknown `kid`

**2. API Keys (Service-to-service)**

- Sent via `x-api-key` gRPC metadata (or `X-API-Key` HTTP header via transcoding)
- Keys stored as SHA-256 hashes in PostgreSQL `api_keys` table
- Scoped permissions, optional expiry, revocation support
- Lookup: hash incoming key -> query by hash -> validate not expired/revoked

### 7.2 Resolution Order

1. Check `authorization` metadata -> JWT flow
2. Check `x-api-key` metadata -> API key flow
3. Neither -> `Unauthenticated` (code 16) with `ErrorInfo{Reason: "MISSING_CREDENTIALS"}`
4. Both present -> prefer JWT, ignore API key
5. On success -> inject `CallerInfo` into context
6. On failure -> rich error with `ErrorInfo` detailing the reason

### 7.3 API Keys Table Schema

```sql
CREATE TABLE api_keys (
    uid          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    key_hash     BYTEA UNIQUE NOT NULL,               -- SHA-256 of the raw key
    display_name TEXT NOT NULL DEFAULT '',
    scopes       TEXT[] NOT NULL DEFAULT '{}',          -- e.g., {"things.read", "things.write"}
    create_time  TIMESTAMPTZ NOT NULL DEFAULT now(),
    expire_time  TIMESTAMPTZ,                          -- nullable = never expires
    revoked      BOOLEAN NOT NULL DEFAULT false,
    last_used    TIMESTAMPTZ,
    created_by   TEXT NOT NULL                          -- who provisioned this key
);

CREATE INDEX idx_api_keys_key_hash ON api_keys (key_hash) WHERE NOT revoked;
```

### 7.4 CallerInfo Context

```go
type CallerInfo struct {
    Subject string   // JWT sub or API key display name
    Method  string   // "jwt" or "api_key"
    Scopes  []string // From JWT claims or API key scopes
}

type contextKey struct{}

func WithCaller(ctx context.Context, ci *CallerInfo) context.Context {
    return context.WithValue(ctx, contextKey{}, ci)
}

func CallerFrom(ctx context.Context) (*CallerInfo, bool) {
    ci, ok := ctx.Value(contextKey{}).(*CallerInfo)
    return ci, ok
}
```

### 7.5 Auth Metrics

Record in the interceptor (see [Observability](#9-observability--tracing-metrics-logging) for metric definitions):

- `auth.attempts_total` — by method (jwt/api_key), status (ok/error)
- `auth.failures_total` — by method, reason (expired, revoked, invalid_sig, wrong_aud)

### 7.6 Configuration

```
JWT_ISSUER=___
JWT_AUDIENCE=___
JWT_SIGNING_ALGORITHM=___  (e.g., RS256, ES256)
JWKS_ENDPOINT=___
```

### 7.7 Testing Auth

Test cases that MUST be covered:

- Valid JWT with correct claims -> success
- Expired JWT -> `Unauthenticated` with reason `TOKEN_EXPIRED`
- JWT with wrong audience -> `Unauthenticated` with reason `WRONG_AUDIENCE`
- JWT with invalid signature -> `Unauthenticated` with reason `INVALID_SIGNATURE`
- Valid API key -> success
- Revoked API key -> `Unauthenticated` with reason `API_KEY_REVOKED`
- Expired API key -> `Unauthenticated` with reason `API_KEY_EXPIRED`
- No credentials -> `Unauthenticated` with reason `MISSING_CREDENTIALS`
- Both JWT and API key -> JWT is used

---

## 8. Long-Running Operations — AIP-151/152/153

Enables async operations with proper lifecycle management.

### 8.1 When to Use LROs

Any method that cannot guarantee completion within ~10s MUST return `google.longrunning.Operation` instead of the resource directly.

### 8.2 AIP Compliance

- **AIP-151** — Method returns `google.longrunning.Operation`. `metadata` carries progress info. `response` carries final result (via `google.protobuf.Any`).
- **AIP-152** — Implement `google.longrunning.Operations` service: `GetOperation`, `ListOperations`, `DeleteOperation`, `CancelOperation`, `WaitOperation`.
- **AIP-153** — Clients poll `GetOperation` with exponential backoff. `done` indicates completion.

### 8.3 Proto Pattern

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
    summary: "Export a thing"
    tags: "Things"
  };
}

message ExportThingMetadata {
  int32 progress_percent = 1 [(google.api.field_behavior) = OUTPUT_ONLY];
  string current_step = 2 [(google.api.field_behavior) = OUTPUT_ONLY];
}

message ExportThingResponse {
  string output_uri = 1 [(google.api.field_behavior) = OUTPUT_ONLY];
  int32 record_count = 2 [(google.api.field_behavior) = OUTPUT_ONLY];
}
```

Each LRO MUST have a corresponding `{Method}Metadata` and `{Method}Response` proto message.

### 8.4 Operations Table

```sql
CREATE TABLE operations (
    name        TEXT PRIMARY KEY,              -- "operations/{uuid}"
    done        BOOLEAN NOT NULL DEFAULT false,
    metadata    JSONB,                         -- serialized Any (metadata proto)
    result      JSONB,                         -- serialized Any (response proto) or error
    error_code  INTEGER,                       -- google.rpc.Code if failed
    error_msg   TEXT,
    create_time TIMESTAMPTZ NOT NULL DEFAULT now(),
    update_time TIMESTAMPTZ NOT NULL DEFAULT now(),
    expire_time TIMESTAMPTZ                    -- operations should expire
);

CREATE INDEX idx_operations_pending ON operations (done) WHERE NOT done;
```

### 8.5 Implementation Architecture

Located in `internal/lro/`.

**Manager (`manager.go`)** — Handles operation CRUD:
- `CreateOperation(ctx, opType) -> Operation` — inserts row, returns initial operation
- `UpdateMetadata(ctx, name, metadata)` — updates progress
- `CompleteOperation(ctx, name, response)` — marks done=true, sets result
- `FailOperation(ctx, name, code, msg)` — marks done=true, sets error
- `GetOperation(ctx, name) -> Operation`
- `ListOperations(ctx, filter, pageSize, pageToken) -> []Operation`

**Worker Pool (`worker.go`):**
- Background workers via `errgroup` — no external job queues
- Workers pick up operations and execute the actual work
- On cancellation, check `ctx.Done()` and update operation status

**WaitOperation (`manager.go`)** — Use pgx `LISTEN/NOTIFY` with context deadline, NOT busy-wait loops:

```go
func (m *Manager) WaitOperation(ctx context.Context, name string, timeout time.Duration) (*longrunningpb.Operation, error) {
    // Check if already done
    op, err := m.GetOperation(ctx, name)
    if err != nil { return nil, err }
    if op.Done { return op, nil }

    // Set up LISTEN
    conn, err := m.pool.Acquire(ctx)
    if err != nil { return nil, err }
    defer conn.Release()

    _, err = conn.Exec(ctx, "LISTEN operation_"+sanitize(name))
    if err != nil { return nil, err }

    // Wait with timeout
    waitCtx, cancel := context.WithTimeout(ctx, timeout)
    defer cancel()

    _, err = conn.Conn().WaitForNotification(waitCtx)
    // ... fetch and return updated operation ...
}
```

**Reaper (`reaper.go`)** — Background goroutine that cleans up expired operations:

```go
func (r *Reaper) Run(ctx context.Context) error {
    ticker := time.NewTicker(r.interval) // e.g., 1 hour
    defer ticker.Stop()
    for {
        select {
        case <-ctx.Done():
            return ctx.Err()
        case <-ticker.C:
            r.cleanExpired(ctx)
        }
    }
}
```

### 8.6 Operation Name Pattern

`operations/{uuid}` — always use this format.

### 8.7 LRO Metrics

See [Observability](#9-observability--tracing-metrics-logging) for metric definitions:
- `lro.in_flight` — gauge of currently running operations
- `lro.duration_seconds` — histogram of completion time
- `lro.failure_total` — failure rate by type and reason

### 8.8 LRO Logging

Log all state transitions:
- `INFO` — "operation created" (name, type)
- `INFO` — "operation progressed" (name, percent, step)
- `INFO` — "operation completed" (name, duration)
- `ERROR` — "operation failed" (name, error code, message)

---

## 9. Observability — Tracing, Metrics, Logging

Enables production monitoring and debugging.

### 9.1 Three-Signal Architecture

| Signal | Pipeline | Rationale |
|---|---|---|
| **Tracing** | OTel SDK -> OTLP exporter -> collector/backend | Distributed traces require OTel — no stdlib alternative |
| **Metrics** | OTel SDK -> Prometheus exporter -> `/metrics` scrape | Prometheus pull model is infra-standard; OTel bridges cleanly |
| **Logging** | `log/slog` -> JSON to stdout -> infra scrapes | 12-factor, container-native; no OTel log exporter needed |

Logs are **correlated** with traces by injecting `trace_id` and `span_id` from OTel span context into every slog record.

### 9.2 `internal/o11y/` Package Structure

- `tracing.go` — OTel TracerProvider setup (OTLP exporter)
- `metrics.go` — OTel MeterProvider setup (Prometheus exporter)
- `slog.go` — Trace-correlating slog handler wrapper
- `appmetrics.go` — Application metric definitions
- `o11y.go` — Top-level `Init()` / `Shutdown()` for all telemetry

### 9.3 Tracer Provider

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

### 9.4 Prometheus Metrics + Exemplars

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

### 9.5 Trace-Correlating slog Handler

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

### 9.6 gRPC Auto-Instrumentation

```go
import "go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"

s := grpc.NewServer(
    grpc.StatsHandler(otelgrpc.NewServerHandler()),
)
```

Provides automatic per-RPC spans + metrics (`rpc.server.duration`, `rpc.server.request.size`).

### 9.7 slog Conventions

- **Always pass `ctx`** to `slog.InfoContext` / `slog.ErrorContext` for trace correlation
- Log at `Error` for 5xx, `Warn` for 4xx that may indicate bugs, `Info` for success
- gRPC unary interceptor logs: method, caller identity, duration, gRPC status code

```go
// Example output:
// {"time":"...","level":"INFO","msg":"resource created",
//   "method":"CreateThing","resource_name":"things/abc-123",
//   "latency":"12.3ms","trace_id":"a1b2c3...","span_id":"d4e5f6..."}
```

### 9.8 Application Metrics

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

---

## 10. Testing Patterns

Ensures comprehensive test coverage for all server methods.

### 10.1 Conventions

- Stdlib `testing` package for all tests
- `testify/assert` and `testify/require` for assertions only — **no testify suites**
- `testing/synctest` (stable in Go 1.25+) for concurrent code, timer-based tests, goroutine coordination
- Table-driven tests for all service methods
- `t.Parallel()` where tests are independent
- `testing.Short()` to gate integration tests

### 10.2 Test Categories

Every server method must have tests covering:

**Error Cases:**
- Not found -> verify `codes.NotFound` + `ResourceInfo` details
- Invalid input -> verify `codes.InvalidArgument` + `BadRequest.FieldViolation` details
- Permission denied -> verify `codes.PermissionDenied` + `ErrorInfo` reason
- Conflict / already exists -> verify `codes.AlreadyExists` + `ResourceInfo`
- Etag mismatch -> verify `codes.FailedPrecondition` + `PreconditionFailure`

**Pagination:**
- Empty results -> `next_page_token` is empty, resources slice is empty
- Single page -> all resources returned, no `next_page_token`
- Multi-page -> `next_page_token` present, each page has correct count
- Invalid page token -> `codes.InvalidArgument`
- `page_size` = 0 -> uses default
- `page_size` > max -> clamped to max (1000)

**FieldMask:**
- Partial update -> only specified fields change
- Full update (no mask) -> all mutable fields replaced
- Invalid paths in mask -> `codes.InvalidArgument`
- Immutable field in mask -> `codes.InvalidArgument`

**Auth:**
- Valid JWT with correct claims -> success
- Expired JWT -> `codes.Unauthenticated`, reason `TOKEN_EXPIRED`
- JWT with wrong audience -> `codes.Unauthenticated`, reason `WRONG_AUDIENCE`
- Invalid signature -> `codes.Unauthenticated`, reason `INVALID_SIGNATURE`
- Valid API key -> success
- Revoked API key -> `codes.Unauthenticated`, reason `API_KEY_REVOKED`
- Expired API key -> `codes.Unauthenticated`, reason `API_KEY_EXPIRED`
- No credentials -> `codes.Unauthenticated`, reason `MISSING_CREDENTIALS`

**Protovalidate:**
- Send invalid requests and verify structured violation responses
- Verify that protovalidate interceptor returns `codes.InvalidArgument` with `BadRequest.FieldViolation` details
- Test boundary values for constraints (min_len, max_len, pattern, range)

**LRO Lifecycle:**
- Create operation -> returns non-done operation with metadata
- Poll until done -> operation transitions to done with result
- Cancel in-flight -> operation marked as failed with `CANCELLED`
- Wait with timeout -> returns when done or deadline exceeded

### 10.3 Verifying Error Details

Always verify error detail types and values, not just the gRPC code:

```go
func assertNotFound(t *testing.T, err error, resourceType, resourceName string) {
    t.Helper()
    st, ok := status.FromError(err)
    require.True(t, ok, "expected gRPC status error")
    assert.Equal(t, codes.NotFound, st.Code())

    var found bool
    for _, detail := range st.Details() {
        if ri, ok := detail.(*errdetails.ResourceInfo); ok {
            assert.Equal(t, resourceType, ri.ResourceType)
            assert.Equal(t, resourceName, ri.ResourceName)
            found = true
        }
    }
    assert.True(t, found, "expected ResourceInfo in error details")
}

func assertFieldViolation(t *testing.T, err error, field string) {
    t.Helper()
    st, ok := status.FromError(err)
    require.True(t, ok)
    assert.Equal(t, codes.InvalidArgument, st.Code())

    var found bool
    for _, detail := range st.Details() {
        if br, ok := detail.(*errdetails.BadRequest); ok {
            for _, v := range br.FieldViolations {
                if v.Field == field {
                    found = true
                }
            }
        }
    }
    assert.True(t, found, "expected FieldViolation for %q", field)
}
```

### 10.4 Table-Driven Test Pattern

```go
func TestCreateThing(t *testing.T) {
    t.Parallel()

    tests := []struct {
        name    string
        req     *pb.CreateThingRequest
        wantErr codes.Code
        check   func(t *testing.T, resp *pb.Thing, err error)
    }{
        {
            name: "success",
            req: &pb.CreateThingRequest{
                Parent:  "projects/test-project",
                ThingId: "my-thing",
                Thing:   &pb.Thing{DisplayName: "My Thing"},
            },
            check: func(t *testing.T, resp *pb.Thing, err error) {
                require.NoError(t, err)
                assert.Equal(t, "My Thing", resp.DisplayName)
                assert.NotEmpty(t, resp.Uid)
                assert.NotEmpty(t, resp.Etag)
                assert.NotNil(t, resp.CreateTime)
            },
        },
        {
            name: "missing parent",
            req: &pb.CreateThingRequest{
                Thing: &pb.Thing{DisplayName: "Test"},
            },
            wantErr: codes.InvalidArgument,
        },
        {
            name: "duplicate",
            // ... setup existing thing first ...
            wantErr: codes.AlreadyExists,
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            t.Parallel()
            // ... setup ...
            resp, err := svc.CreateThing(ctx, tt.req)
            if tt.wantErr != codes.OK {
                assertCode(t, err, tt.wantErr)
                return
            }
            if tt.check != nil {
                tt.check(t, resp, err)
            }
        })
    }
}
```

### 10.5 Database Mocking

Use sqlc's generated interface (`emit_interface: true`) for mocking:

```go
// sqlc generates:
type Querier interface {
    GetThing(ctx context.Context, name string) (Thing, error)
    ListThings(ctx context.Context, arg ListThingsParams) ([]Thing, error)
    // ...
}

// In tests, provide a mock implementing Querier
type mockDB struct {
    things map[string]db.Thing
}

func (m *mockDB) GetThing(ctx context.Context, name string) (db.Thing, error) {
    t, ok := m.things[name]
    if !ok {
        return db.Thing{}, pgx.ErrNoRows
    }
    return t, nil
}
```

### 10.6 Integration Tests

Gate behind `testing.Short()`:

```go
func TestIntegration_CreateThing(t *testing.T) {
    if testing.Short() {
        t.Skip("skipping integration test")
    }
    // ... use real database ...
}
```

### 10.7 testing/synctest for Concurrent Code

Use for LRO workers, goroutine coordination, timer-based logic:

```go
func TestWorkerPool_ProcessesOperation(t *testing.T) {
    synctest.Run(func() {
        pool := NewWorkerPool(3)
        op := createTestOperation()

        pool.Submit(op)

        // Advance fake time
        time.Sleep(5 * time.Second)

        // Assert operation completed
        result := pool.GetResult(op.Name)
        assert.True(t, result.Done)
    })
}
```

---

## 11. Network Architecture — Three Ports

Defines port layout and debug HTTP mux for the service.

### 11.1 Port Layout

| Port | Purpose | Exposed To |
|---|---|---|
| `:50051` | gRPC (native gRPC clients, gRPC health service) | Internal services, gRPC clients |
| `:8080` | grpc-gateway REST (JSON transcoding) | External clients, frontends, API consumers |
| `:9090` | Debug HTTP (health, metrics, pprof, Swagger UI) | Internal infra only (K8s probes, Prometheus scraper) |

### 11.2 Rationale

Separate ports give:

- **Independent lifecycle** — drain gRPC while health endpoint still responds
- **Clean observability** — connection metrics per protocol
- **Security boundaries** — debug port never exposed externally
- **K8s compatibility** — correct `appProtocol` hints per port

### 11.3 Debug Mux

Use Go 1.22+ `net/http` enhanced routing for the debug HTTP server on `:9090`:

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

### 11.4 K8s Probe Configuration

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

---

## 12. OpenAPI Spec Generation

Auto-generates Swagger spec from proto annotations.

### 12.1 buf.gen.yaml Setup

Add `protoc-gen-openapiv2` to your generation config:

```yaml
version: v2
managed:
  enabled: true
plugins:
  - remote: buf.build/grpc/go
    out: gen
    opt: paths=source_relative
  - remote: buf.build/protocolbuffers/go
    out: gen
    opt: paths=source_relative
  - remote: buf.build/grpc-ecosystem/gateway
    out: gen
    opt: paths=source_relative
  - remote: buf.build/grpc-ecosystem/openapiv2
    out: gen/openapiv2
    opt:
      - allow_merge=true
      - merge_file_name=api
```

This produces `gen/openapiv2/api.swagger.json` (OpenAPI 2.0) on every `buf generate`.

### 12.2 Service-Level Annotation

Add to your `{service}_service.proto` (once per service):

```protobuf
import "protoc-gen-openapiv2/options/annotations.proto";

option (grpc.gateway.protoc_gen_openapiv2.options.openapiv2_swagger) = {
  info: {
    title: "Thing Service API"       // POPULATE with your service
    version: "1.0"
    description: "Manages Thing resources."  // POPULATE
  }
  schemes: HTTPS
  consumes: "application/json"
  produces: "application/json"
  security_definitions: {
    security: {
      key: "Bearer"
      value: {
        type: TYPE_API_KEY
        in: IN_HEADER
        name: "Authorization"
        description: "JWT Bearer token: 'Bearer {token}'"
      }
    }
    security: {
      key: "ApiKey"
      value: {
        type: TYPE_API_KEY
        in: IN_HEADER
        name: "X-API-Key"
      }
    }
  }
};
```

### 12.3 Per-RPC Annotation

**All RPCs must have `openapiv2_operation` with `summary` and `tags`.**

```protobuf
rpc GetThing(GetThingRequest) returns (Thing) {
  option (google.api.http) = {
    get: "/v1/{name=projects/*/things/*}"
  };
  option (grpc.gateway.protoc_gen_openapiv2.options.openapiv2_operation) = {
    summary: "Get a thing"
    tags: "Things"
  };
}

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
    summary: "Export a thing"
    tags: "Things"
  };
}
```

Only add `summary` and `tags`. Do NOT add per-field `openapiv2_field` descriptions unless overriding something non-obvious. Field names and protovalidate constraints are self-documenting.

### 12.4 Swagger UI

Serve the generated spec on the `:9090` debug port (see [Network Architecture](#11-network-architecture--three-ports)):

```go
mux.Handle("GET /swagger/", http.StripPrefix("/swagger/",
    http.FileServer(http.Dir("gen/openapiv2"))))
```

---

## 13. gRPC Health Service

Primary health mechanism for K8s probes and load balancers.

### 13.1 Primary Health Mechanism

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

### 13.2 What to Check

The background goroutine should verify critical dependencies:

- **PostgreSQL** — `db.Ping(ctx)` on the connection pool
- **External services** — any upstream gRPC/HTTP services this service depends on (if applicable)

Do NOT check non-critical dependencies (caches, optional feature flags). A health check failure should mean "this instance cannot serve requests."

### 13.3 HTTP Health Wrapper

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

### 13.4 Graceful Shutdown

During shutdown, set status to `NOT_SERVING` before draining:

```go
healthcheck.SetServingStatus("", healthpb.HealthCheckResponse_NOT_SERVING)
// Then gracefully stop the gRPC server
grpcServer.GracefulStop()
```

This lets K8s probes detect the instance is draining and stop routing traffic before the server actually stops.

---

## 14. gRPC Reflection — Dev Builds Only

Enables gRPC tooling in development without exposing in production.

### 14.1 Why Gate Behind Build Tags

gRPC reflection exposes your full service schema to any client that connects. This is invaluable for development (grpcurl, grpcui, Postman) but should never be available in production — it's an information disclosure risk and unnecessary attack surface.

### 14.2 Implementation

Two files in `internal/server/`, distinguished by build tags:

**`devservices_dev.go`** — enabled with `-tags dev`:

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

**`devservices_prod.go`** — default (no tag):

```go
//go:build !dev

package server

import "google.golang.org/grpc"

func registerDevServices(s *grpc.Server) {}
```

### 14.3 Usage

Call `registerDevServices(grpcServer)` in your server setup code. The build tag determines which implementation is compiled:

```bash
# Local development / staging — reflection enabled
go build -tags dev ./cmd/server

# Production — reflection disabled (default)
go build ./cmd/server
```

### 14.4 Development Workflow

With reflection enabled, use tools like:

```bash
# List all services
grpcurl -plaintext localhost:50051 list

# Describe a service
grpcurl -plaintext localhost:50051 describe myservice.v1.ThingService

# Call a method
grpcurl -plaintext -d '{"parent": "projects/test"}' \
  localhost:50051 myservice.v1.ThingService/ListThings
```

---

## 15. References

1. [Google AIP](https://aip.dev)
2. [gRPC Go](https://grpc.io/docs/languages/go/)
3. [buf](https://buf.build/docs/)
4. [protovalidate](https://buf.build/bufbuild/protovalidate)
5. [pgx](https://github.com/jackc/pgx)
6. [sqlc](https://sqlc.dev)
7. [OpenTelemetry Go](https://opentelemetry.io/docs/languages/go/)
8. [grpc-gateway](https://grpc-ecosystem.github.io/grpc-gateway/)
9. [golang-jwt](https://github.com/golang-jwt/jwt)
10. [golang-migrate](https://github.com/golang-migrate/migrate)
11. [testify](https://github.com/stretchr/testify)
