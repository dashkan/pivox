---
title: Proto Definitions & Validation
impact: HIGH
impactDescription: foundation for API contract and request validation
tags: protobuf, buf, protovalidate, AIP, OpenAPI, resource-oriented
---

## Proto Definitions & Validation

> Read this when: writing proto definitions, adding validation rules, designing
> new resources, adding RPCs, or configuring buf. All examples are drawn from
> the Pivox codebase and represent the canonical patterns.

### Proto File Conventions

- `syntax = "proto3"` — do NOT use editions yet (ecosystem not ready, especially grpc-gateway)
- Only specify `option go_package`; use buf managed mode for other languages
- Proto package naming: `pivox.{domain}.v1` (e.g. `pivox.api.v1`, `pivox.iam.v1`)
- Go package option: `option go_package = "pivox/{domain}/v1;{domain}v1";`
- `google.api.field_behavior` annotations on **every** field — no exceptions (AIP-203)
- `google.api.http` annotations on every RPC for JSON transcoding (AIP-127)
- `google.api.resource` annotations on every resource message
- `google.api.resource_reference` on every field that references another resource
- `buf.validate` annotations on every request message field

### buf.yaml Configuration

```yaml
version: v2
lint:
  use:
    - STANDARD
  except:
    - IMPORT_USED
    - RPC_REQUEST_RESPONSE_UNIQUE
    - RPC_RESPONSE_STANDARD_NAME
    - SERVICE_SUFFIX
modules:
  - path: api/proto
    name: buf.build/pivox/googleapis
```

### ID Model — No `uid` Field

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

### Resource Message Pattern

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

### Standard Field Behavior

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

### Protovalidate Integration

All request messages MUST have `buf.validate` annotations on every field.
Validation runs as a gRPC interceptor before the handler.

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

### Division of Labor

| Responsibility | Owner | Examples |
|---|---|---|
| Declarative schema constraints | Protovalidate (proto annotations) | Field format, length, required, patterns |
| Business logic errors | `internal/apierr` package | Etag mismatch, wrong state, not found, already exists |

### Request Validation Patterns

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

### Standard Method Patterns

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

### Service Patterns

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

### Custom Method Patterns

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

### Singleton Sub-Resource Pattern

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

### Multi-Pattern Resources

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

### API Linter Disable Patterns

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

### Enum Patterns

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

### Domain-Specific Validation Examples

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

### Verification Workflow

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

### Proto Coding Standards Summary

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
