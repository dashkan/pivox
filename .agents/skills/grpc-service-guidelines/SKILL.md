---
name: grpc-service-guidelines
description:
  Production-grade gRPC service development in Go following Google AIP standards.
  Use when building gRPC services, writing proto definitions, implementing server
  methods, setting up database schemas with sqlc, adding authentication (JWT/API keys),
  implementing long-running operations, configuring observability (tracing, metrics,
  logging), or writing tests for gRPC services. Triggers on Go backend tasks involving
  protobuf, gRPC, PostgreSQL, buf, protovalidate, or Google API design patterns.
license: MIT
metadata:
  version: "1.0.0"
---

# gRPC Service Guidelines

Production-grade gRPC service patterns in Go at Google L5+ standards. Covers
idiomatic Go, clean architecture, comprehensive error handling, observability,
and strict adherence to Google AIP (API Improvement Proposals).

## When to Apply

Reference these guidelines when:

- Scaffolding a new gRPC service in Go
- Writing or reviewing proto definitions with buf
- Implementing gRPC server methods
- Setting up PostgreSQL schemas and sqlc queries
- Adding JWT or API key authentication
- Implementing long-running operations (AIP-151/152/153)
- Configuring tracing, metrics, and structured logging
- Writing tests for server methods, pagination, auth, or LROs

## Rule Categories by Priority

| Priority | Category              | Impact | Rule File                              |
| -------- | --------------------- | ------ | -------------------------------------- |
| 1        | Go Coding Standards   | HIGH   | `go-coding-standards`                  |
| 2        | Project Structure     | HIGH   | `project-structure`                    |
| 3        | AIP Compliance        | HIGH   | `aip-compliance`                       |
| 4        | Proto & Validation    | HIGH   | `proto-definitions-and-validation`     |
| 5        | Error Handling        | HIGH   | `error-rich-grpc-details`              |
| 6        | Database              | HIGH   | `database-postgresql-sqlc`             |
| 7        | Authentication        | MEDIUM | `auth-jwt-api-keys`                    |
| 8        | Long-Running Ops      | MEDIUM | `lro-operations`                       |
| 9        | Observability         | MEDIUM | `observability-tracing-metrics-logging` |
| 10       | Testing               | MEDIUM | `testing-patterns`                     |
| 11       | Network Architecture  | MEDIUM | `network-architecture`                 |
| 12       | OpenAPI Generation    | MEDIUM | `openapi-generation`                   |
| 13       | gRPC Health Service   | MEDIUM | `grpc-health-service`                  |
| 14       | gRPC Reflection       | LOW    | `grpc-reflection-dev-only`             |

## Quick Reference

### 1. Go Coding Standards (HIGH)

- `go-coding-standards` — Stdlib-first philosophy, approved dependencies,
  Go 1.25/1.26 features, general coding conventions

### 2. Project Structure (HIGH)

- `project-structure` — Directory layout, where things go, adding new resources

### 3. AIP Compliance (HIGH)

- `aip-compliance` — Google API Improvement Proposals reference catalog,
  standard field behavior mapping, method implementation checklist

### 4. Proto & Validation (HIGH)

- `proto-definitions-and-validation` — Proto file conventions, protovalidate
  annotations, AIP validation patterns, domain-specific validation examples

### 5. Error Handling (HIGH)

- `error-rich-grpc-details` — Rich gRPC error details via `internal/apierr`,
  error detail types, gRPC code mapping, testing error details

### 6. Database (HIGH)

- `database-postgresql-sqlc` — Schema conventions, pgx native driver, sqlc
  config, CRUD query patterns, pagination, transactions, etag validation

### 7. Authentication (MEDIUM)

- `auth-jwt-api-keys` — Dual auth pattern (JWT + API keys), resolution order,
  CallerInfo context, auth metrics

### 8. Long-Running Operations (MEDIUM)

- `lro-operations` — AIP-151/152/153 compliance, operations table, manager/worker/reaper
  architecture, WaitOperation via LISTEN/NOTIFY

### 9. Observability (MEDIUM)

- `observability-tracing-metrics-logging` — Three-signal architecture (OTel tracing,
  Prometheus metrics, slog logging), trace correlation, application metrics with purpose

### 10. Testing (MEDIUM)

- `testing-patterns` — Table-driven tests, error detail verification, protovalidate
  testing, pagination/FieldMask/auth/LRO test coverage, database mocking, synctest

### 11. Network Architecture (MEDIUM)

- `network-architecture` — Three-port design, debug HTTP mux setup, Swagger UI
  serving, K8s probe configuration

### 12. OpenAPI Generation (MEDIUM)

- `openapi-generation` — buf.gen.yaml config, service-level and per-RPC annotations,
  Swagger UI serving

### 13. gRPC Health Service (MEDIUM)

- `grpc-health-service` — Primary health mechanism, background dependency checking,
  HTTP health wrapper, graceful shutdown

### 14. gRPC Reflection (LOW)

- `grpc-reflection-dev-only` — Build-tag gated reflection, dev vs prod files,
  grpcurl/grpcui workflow

## How to Use

Read individual rule files for detailed explanations and code examples:

```
rules/proto-definitions-and-validation.md
rules/error-rich-grpc-details.md
rules/database-postgresql-sqlc.md
```

Each rule file contains:

- Context for when to read it
- Conventions and patterns with code examples
- Configuration snippets
- Testing guidance where applicable

## Full Compiled Document

For the complete guide with all rules expanded: `AGENTS.md`
