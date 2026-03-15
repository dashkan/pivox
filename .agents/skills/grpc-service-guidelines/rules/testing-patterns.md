---
title: Testing Patterns
impact: MEDIUM
impactDescription: ensures comprehensive test coverage for all server methods
tags: testing, table-driven, error-details, synctest, mocking
---

## Testing Patterns

> Read this when: writing tests for server methods, error details, auth, pagination, or LRO lifecycle.

### Conventions

- Stdlib `testing` package for all tests
- `testify/assert` and `testify/require` for assertions only — **no testify suites**
- `testing/synctest` (stable in Go 1.25+) for concurrent code, timer-based tests, goroutine coordination
- Table-driven tests for all service methods
- `t.Parallel()` where tests are independent
- `testing.Short()` to gate integration tests

### Test Categories

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

### Verifying Error Details

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

### Table-Driven Test Pattern

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

### Database Mocking

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

### Integration Tests

Gate behind `testing.Short()`:

```go
func TestIntegration_CreateThing(t *testing.T) {
    if testing.Short() {
        t.Skip("skipping integration test")
    }
    // ... use real database ...
}
```

### testing/synctest for Concurrent Code

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
