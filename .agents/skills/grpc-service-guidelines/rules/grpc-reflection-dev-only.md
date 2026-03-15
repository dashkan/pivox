---
title: gRPC Reflection — Dev Builds Only
impact: LOW
impactDescription: enables gRPC tooling in development without exposing in production
tags: reflection, build-tags, dev, grpcurl, grpcui
---

## gRPC Reflection — Dev Builds Only

> Read this when: setting up development tooling, configuring build tags, or enabling grpcurl/grpcui access.

### Why Gate Behind Build Tags

gRPC reflection exposes your full service schema to any client that connects. This is invaluable for development (grpcurl, grpcui, Postman) but should never be available in production — it's an information disclosure risk and unnecessary attack surface.

### Implementation

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

### Usage

Call `registerDevServices(grpcServer)` in your server setup code. The build tag determines which implementation is compiled:

```bash
# Local development / staging — reflection enabled
go build -tags dev ./cmd/server

# Production — reflection disabled (default)
go build ./cmd/server
```

### Development Workflow

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
