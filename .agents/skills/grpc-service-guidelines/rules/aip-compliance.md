---
title: AIP Compliance Reference
impact: HIGH
impactDescription: ensures all API design follows Google API Improvement Proposals
tags: AIP, Google, API design, resource-oriented, standards
---

## AIP Compliance Reference

> Read this when: designing a new resource, implementing a standard method, or making any API design decision. Reference AIPs by number in code comments and PR descriptions.

### Resource Design

| AIP | Topic | Key Requirement |
|---|---|---|
| AIP-121 | Resource-oriented design | APIs are modeled as resources with standard methods |
| AIP-122 | Resource names | Full resource name as `name` field (e.g., `projects/{project}/things/{thing}`) |
| AIP-123 | Resource types | Type string format: `{service}.googleapis.com/{ResourceType}` |
| AIP-148 | Standard fields | `create_time`, `update_time`, `delete_time`, `etag` on every resource. No `uid` — `name` is the sole external ID. |
| AIP-154 | Etags | Optimistic concurrency — etag regenerated on every update, checked before writes |
| AIP-164 | Soft delete | `delete_time` set on delete, `Undelete` method to restore, `expire_time` for permanent cleanup |

### Standard Methods

| AIP | Method | HTTP | Key Details |
|---|---|---|---|
| AIP-131 | Get | `GET /v1/{name=resources/*}` | Return single resource by name |
| AIP-132 | List | `GET /v1/{parent=...}/resources` | Pagination via `page_size`/`page_token`, `next_page_token` |
| AIP-133 | Create | `POST /v1/{parent=...}/resources` | Optional `resource_id` for client-assigned names |
| AIP-134 | Update | `PATCH /v1/{resource.name=...}` | `update_mask` (FieldMask) — only update specified fields |
| AIP-135 | Delete | `DELETE /v1/{name=resources/*}` | Soft delete by default, optional `force` for hard delete |
| AIP-127 | HTTP transcoding | — | `google.api.http` annotations on every RPC |

### Long-Running Operations

| AIP | Topic | Key Requirement |
|---|---|---|
| AIP-151 | LRO definition | Method returns `google.longrunning.Operation` with `metadata` and `response` types |
| AIP-152 | Operations service | Implement `GetOperation`, `ListOperations`, `DeleteOperation`, `CancelOperation`, `WaitOperation` |
| AIP-153 | Polling lifecycle | Clients poll `GetOperation` with exponential backoff; `done` field indicates completion |

### Query Features

| AIP | Topic | Key Requirement |
|---|---|---|
| AIP-140 | FieldMasks | Partial updates — only modify fields in the mask |
| AIP-158 | Pagination | Cursor-based with opaque, base64-encoded page tokens (never raw offsets) |
| AIP-160 | Filtering | CEL-like filter expressions on List methods |
| AIP-161 | Ordering | `order_by` field on List methods |

### Field Behavior & Annotations

| AIP | Topic | Key Requirement |
|---|---|---|
| AIP-203 | Field behavior | `google.api.field_behavior` annotation on **every** field — REQUIRED, OUTPUT_ONLY, IMMUTABLE, OPTIONAL. No exceptions. |

### Standard Field Behavior Mapping

| Field | Behavior | Notes |
|---|---|---|
| `name` | IDENTIFIER | Sole external ID, server-assigned from `parent` + `resource_id` |
| `display_name` | OPTIONAL | Mutable, human-readable |
| `create_time` | OUTPUT_ONLY, IMMUTABLE | Server-assigned at creation |
| `update_time` | OUTPUT_ONLY | Updated by server on every write |
| `delete_time` | OUTPUT_ONLY | Set on soft delete, cleared on undelete |
| `etag` | OUTPUT_ONLY | Regenerated on every update |

### Implementing a Method Checklist

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
