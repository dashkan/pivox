---
title: Rich gRPC Error Details
impact: HIGH
impactDescription: ensures machine-readable errors for all API consumers
tags: errors, gRPC, status, errdetails, apierr
---

## Rich gRPC Error Details

> Read this when: implementing server methods, writing error responses, or testing error behavior.

### Core Rule

**Every** RPC error MUST use Google's richer error model. **Never** return a bare `status.Errorf(codes.X, "message")`. Always attach structured error details via `internal/apierr` package helpers.

### Error Detail Types

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

### `internal/apierr` Package

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

### Error Rules

1. **Never return bare status errors.** Always use `internal/apierr` helpers.
2. **Always include `ResourceInfo`** on resource-specific errors (NotFound, AlreadyExists, PermissionDenied).
3. **Always include `ErrorInfo`** with a machine-readable `Reason` string (e.g., `"RESOURCE_NOT_FOUND"`, `"ETAG_MISMATCH"`, `"API_KEY_REVOKED"`). This is what clients switch on.
4. **Always include `BadRequest.FieldViolation`** for every individual field that fails business-logic validation — not one error for the whole request. (Protovalidate handles schema validation automatically via interceptor.)
5. **Always include `PreconditionFailure`** for etag mismatches, state violations, or conditional failures.
6. **Include `RetryInfo`** when the client should retry (rate limits, transient failures).
7. **Never include `DebugInfo`** in production. Gate behind build tag or env flag.
8. **Error details serialize through grpc-gateway** into the HTTP `details` array automatically.

### gRPC Code Mapping

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

### Testing Error Details

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
