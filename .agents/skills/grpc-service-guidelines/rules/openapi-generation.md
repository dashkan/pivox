---
title: OpenAPI Spec Generation
impact: MEDIUM
impactDescription: auto-generates Swagger spec from proto annotations
tags: OpenAPI, Swagger, protoc-gen-openapiv2, buf, documentation
---

## OpenAPI Spec Generation

> Read this when: configuring buf for OpenAPI output, adding API documentation annotations, or setting up Swagger UI.

### buf.gen.yaml Setup

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

### Service-Level Annotation (Once Per Service)

Add to your `{service}_service.proto`:

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

### Per-RPC Annotation (Keep Concise)

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

### Swagger UI

Serve the generated spec on the `:9090` debug port (see `network-architecture.md`):

```go
mux.Handle("GET /swagger/", http.StripPrefix("/swagger/",
    http.FileServer(http.Dir("gen/openapiv2"))))
```
