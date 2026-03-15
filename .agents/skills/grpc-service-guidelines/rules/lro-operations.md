---
title: Long-Running Operations — AIP-151/152/153
impact: MEDIUM
impactDescription: enables async operations with proper lifecycle management
tags: LRO, operations, AIP-151, AIP-152, AIP-153, LISTEN/NOTIFY
---

## Long-Running Operations — AIP-151/152/153

> Read this when: implementing operations that take >10s, building the operations service, or wiring up background workers.

### When to Use LROs

Any method that cannot guarantee completion within ~10s MUST return `google.longrunning.Operation` instead of the resource directly.

### AIP Compliance

- **AIP-151** — Method returns `google.longrunning.Operation`. `metadata` carries progress info. `response` carries final result (via `google.protobuf.Any`).
- **AIP-152** — Implement `google.longrunning.Operations` service: `GetOperation`, `ListOperations`, `DeleteOperation`, `CancelOperation`, `WaitOperation`.
- **AIP-153** — Clients poll `GetOperation` with exponential backoff. `done` indicates completion.

### Proto Pattern

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

### Operations Table

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

### Implementation Architecture (`internal/lro/`)

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

### Operation Name Pattern

`operations/{uuid}` — always use this format.

### LRO Metrics

See `observability-tracing-metrics-logging.md` for metric definitions:
- `lro.in_flight` — gauge of currently running operations
- `lro.duration_seconds` — histogram of completion time
- `lro.failure_total` — failure rate by type and reason

### LRO Logging

Log all state transitions:
- `INFO` — "operation created" (name, type)
- `INFO` — "operation progressed" (name, percent, step)
- `INFO` — "operation completed" (name, duration)
- `ERROR` — "operation failed" (name, error code, message)
