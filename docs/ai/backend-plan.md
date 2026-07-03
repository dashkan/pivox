# AI Chat — Go Backend Implementation Plan

Scoped to the Go backend for AI chat (protos, DB, services, streaming, HTTP content endpoint).
Frontend work follows once this is tested and merged.

## Engineering principles — non-negotiable

**No hacky code. Ever.** Applies to every line of this implementation and every PR.

This means:
- No `// TODO: fix properly later`, no `// HACK:`, no temporary workarounds that paper over real problems
- No silent fallbacks, swallowed errors, catch-alls that hide root causes
- No magic constants tuned until tests pass
- No duplicated logic because the clean path seemed hard
- No retry loops that mask races
- No `if platform { ... } else { ... }` branches that hide design flaws
- If the clean solution requires refactoring adjacent code, refactor it (local refactors only — see sweeping-changes rule below)
- If the clean solution requires reverting an earlier commit, revert it (unmerged work only — see sweeping-changes rule below)

**Sweeping changes require user consultation first.** Local refactors proceed freely. Anything that touches files outside the current feature area, changes public interfaces used by other code, modifies merged work, crosses package boundaries, or rewrites more than ~200 lines of existing working code — ask first. Cost of a one-sentence check is low; cost of an unexpected rewrite landing in the inbox is high.

**Complex engineering issues get consulted with Gemini before implementation.** When implementation hits a problem where the obvious path leads somewhere hacky:

1. Stop.
2. Understand the root cause — read code, docs, and context until the actual problem is clear
3. Prompt Gemini for a sounding-board on the clean solution
4. Capture the discussion outcome in this doc or an adjacent design note
5. Implement the clean solution

Examples that qualify for Gemini consultation: cross-language interop edge cases, race conditions, build system issues, API design tradeoffs, performance cliffs, schema migration integrity concerns, library limitations requiring workarounds. Standard CRUD and well-trodden patterns don't.

## TL;DR

- **Package**: `pivox.ai.v1`, Go import path `github.com/dashkan/pivox/internal/pkg/gen/pivox/ai/v1`
- **Resource hierarchy**: `organizations/{organization}/conversations/{conversation}/{messages|artifacts}/...`
- **Single gRPC service**: `AiChat` — AIP-compliant resource methods + one non-AIP `Stream` bidi RPC with lint suppression
- **All REST routes `/v1/` prefixed**, matching Pivox convention
- **HTTP `:content` endpoint**: `GET /v1/{name}:content` for artifact version bytes, registered via `gwMux.HandlePath` on the existing REST gateway mux. Serves inline bytes directly or redirects to the asset system for asset-backed versions
- **HTTP SSE endpoint**: `POST /v1/ai:stream` mirroring Vercel AI SDK UI message stream format, sharing the same gRPC backend as the native bidi `Stream` method
- **Artifact storage split**: inline `bytea` in Postgres for small text artifacts (code, markdown, svg), **`asset_version` pointer** to the existing Pivox asset system for binary/heavy artifacts (image, pdf, video). Artifact system is a thin metadata layer over the asset system for binary content
- **Inline size cap**: 1 MB per inline version (text-only). Asset-backed versions inherit the asset system's own limits
- **Model provider for v1**: Ollama + qwen3-vl via `LanguageModel` interface adapter
- **Tool machinery**: server and client tool dispatch both wired end-to-end. Registries empty in v1; adding a tool is a registration call, not a new phase
- **Conversation history management**: `messages.token_count` column (heuristic `len(text)/4`), SQL window-function truncation query with `model_context_budget` (default 22500). No summarization in v1
- **Migration strategy**: extend `000001_init.up.sql` (pre-release, no new migration file)
- **Testing**: TDD unit + integration (`//go:build dev`) + end-to-end streaming test + HTTP content handler test + SSE handler test

## Repo landing

Scanned the existing Go codebase. The following patterns are established and this plan follows them exactly — no new conventions introduced.

| Concern | Existing pattern | New AiChat location |
|---|---|---|
| Proto files | `api/proto/pivox/<domain>/v1/*.proto` | `api/proto/pivox/ai/v1/*.proto` |
| Generated Go | `internal/pkg/gen/pivox/<domain>/v1` | `internal/pkg/gen/pivox/ai/v1` |
| Service impl | `internal/service/<domain>/` | `internal/service/aichat/` |
| SQL queries | `internal/db/queries/<resource>.sql` | `internal/db/queries/{conversations,messages,artifacts,artifact_versions}.sql` |
| Migration | Single `000001_init.up.sql` — extend for new tables | Append table definitions to existing file |
| Converters | `internal/convert/<domain>.go` | `internal/convert/aichat.go` |
| Resource name parsing | Inline per-service or `internal/resource/` | Inline in `internal/service/aichat/names.go` |
| Error mapping | `internal/apierr/apierr.HandleResourceError` | Reuse |
| Auth | `internal/server/auth_interceptor.go` → `MustPivoxUserID(ctx)` | Reuse via interceptor |
| Validation | `buf/validate` proto annotations, `FieldMaskAwareValidationInterceptor` | Reuse |
| Test helpers | `internal/testutil` (`SetupTestDB`, `SetupGRPCServer`, mocks) | Reuse |
| Integration test guard | `//go:build dev` | Same |
| Main server wiring | `cmd/pivox-cloud/main.go` | Add `AiChat` registration + REST gateway handler + `:content` handler |
| Buf config | `buf.yaml`, `buf.gen.yaml` | No changes needed — `api/proto/pivox/ai/v1/` will be picked up automatically |

## Implementation phases

Each phase is a commit (or set of closely related commits). TDD throughout: tests first, implementation second.

### Phase 0 — Protos (~1 day)

Write the four proto files under `api/proto/pivox/ai/v1/`. Use the existing `pivox.assets.v1` and `pivox.iam.v1` protos as style references (Apache header, `buf/validate`, `google.api.*` annotations, resource patterns, method signatures, HTTP bindings).

**Files:**

1. `api/proto/pivox/ai/v1/ai_chat.proto`
   - Package declaration, imports, `option go_package = "pivox/ai/v1;aiv1"`
   - `service AiChat` with all methods (resource CRUD + `Stream`)
   - `ClientEvent`, `ServerEvent` oneof messages
   - Child messages: `UserMessage`, `TextStart/TextDelta/TextEnd`, `ReasoningStart/Delta/End`, `ToolInputStart/Delta/Available`, `ToolOutputAvailable/Error`, `ToolApprovalRequested/Response`, `ArtifactStart/Delta/End/Error`, `MessageMetadata`, `Done`, `Error` (using `google.rpc.Status`)
   - Mirror Vercel AI SDK's UI message stream shape 1:1 for text/reasoning/tool events
   - Artifact events are first-class oneof cases (not behind `DataPart`)
   - `DataPart` retained for genuinely custom future extensions (engine status, rundown updates)
   - The `Stream` method has the AIP lint suppression comment: `// (-- api-linter: ... aip.dev/not-precedent: bidi streaming custom method for live chat transport. --)`
   - **No HTTP route annotation on `Stream`** — bidi gRPC only. Web clients use the separate `POST /v1/ai:stream` SSE handler (see Phase 7)

2. `api/proto/pivox/ai/v1/conversations.proto`
   - `message Conversation { name, creator, title, description, archived, pinned, message_count, last_message_time, create_time, update_time, etag }`
   - Resource pattern: `organizations/{organization}/conversations/{conversation}`
   - Resource type: `pivox.ai/Conversation`
   - `creator` is `string`, points to `organizations/{organization}/users/{user}` (stored as resource name, not UUID, to keep AIP purity)
   - All HTTP routes `/v1/` prefixed:
     - `GET  /v1/{name=organizations/*/conversations/*}`
     - `GET  /v1/{parent=organizations/*}/conversations`
     - `POST /v1/{parent=organizations/*}/conversations`
     - `PATCH /v1/{conversation.name=organizations/*/conversations/*}`
     - `DELETE /v1/{name=organizations/*/conversations/*}`
   - Request/response messages: `GetConversationRequest`, `ListConversationsRequest/Response`, `CreateConversationRequest`, `UpdateConversationRequest` (with `FieldMask`), `DeleteConversationRequest`
   - Field behavior annotations throughout

3. `api/proto/pivox/ai/v1/messages.proto`
   - `message Message { name, role, parts, create_time }`
   - `role`: enum `{USER, ASSISTANT, SYSTEM, TOOL}`
   - `parts`: repeated `MessagePart` with `oneof` (text, reasoning, tool_call, tool_result, file)
   - Resource pattern: `organizations/{organization}/conversations/{conversation}/messages/{message}`
   - HTTP routes `/v1/` prefixed
   - Request/response: `GetMessageRequest`, `ListMessagesRequest/Response` only (no Create/Update/Delete — produced by the streaming path)

4. `api/proto/pivox/ai/v1/artifacts.proto`
   - `message Artifact { name, type, title, description, create_time, update_time, latest_version }`
   - `message ArtifactVersion` uses `oneof content` for the two storage modes:
     ```proto
     message ArtifactVersion {
       string name = 1 [(google.api.field_behavior) = IDENTIFIER];
       oneof content {
         InlineContent inline = 2;   // small text artifacts (code, markdown, svg)
         string asset_version = 3;   // "organizations/.../assets/.../versions/..."
       }
       google.protobuf.Timestamp create_time = 4;
     }

     message InlineContent {
       bytes data = 1;          // stripped in List responses, returned in Get
       string content_type = 2;
       int64 size_bytes = 3;
     }
     ```
   - Resource patterns:
     - `organizations/{organization}/conversations/{conversation}/artifacts/{artifact}`
     - `organizations/{organization}/conversations/{conversation}/artifacts/{artifact}/versions/{version}`
   - All HTTP routes `/v1/` prefixed
   - Request/response: `GetArtifactRequest`, `ListArtifactsRequest/Response`, `DeleteArtifactRequest` (cascades to versions), `GetArtifactVersionRequest`, `ListArtifactVersionsRequest/Response`, `DeleteArtifactVersionRequest` (auto-deletes parent when last version removed)

**How binary content is fetched**:

1. Client calls `CreateStorageSession` once (existing RPC) → `pivox_session` cookie set on `.pivox.app`
2. Client reads `ArtifactVersion.asset_version` from the proto
3. Client calls `GetAssetVersion(asset_version)` → response includes `storage_url` (new field, see prerequisite below)
4. Client hits `storage_url` directly, session cookie attached automatically by the native HTTP stack
5. Gateway serves the bytes — cached, CDN-proxied, all the existing machinery

Artifact system never touches storage internals. Artifact `:content` handler only serves inline text bytes.

**Prerequisite — asset system change**:

Add `string storage_url = N [(google.api.field_behavior) = OUTPUT_ONLY]` to `pivox.assets.v1.AssetVersion`. Populated server-side by joining `storage_gateways.hostname` + `storage_endpoints.short_name` + `asset_versions.storage_key`. Belongs to the asset system, not the AI work — but this plan depends on it. Landed as a small separate commit before Phase 5.

**Deliverable**: four proto files, reviewable independently before codegen.

### Phase 1 — Proto codegen (~5 min)

Run `buf generate` from the repo root. Expected output:

- `internal/pkg/gen/pivox/ai/v1/*.pb.go` (protoc-gen-go)
- `internal/pkg/gen/pivox/ai/v1/*_grpc.pb.go` (protoc-gen-go-grpc)
- `internal/pkg/gen/pivox/ai/v1/*.pb.gw.go` (protoc-gen-grpc-gateway)
- `api/openapi/pivox/ai/v1/*.swagger.json` (protoc-gen-openapiv2)

No new plugins or config needed — `buf.gen.yaml` already targets `api/proto/pivox/`. The `proto-gen` skill in the repo handles this.

### Phase 2 — DB schema (~0.5 day)

Extend `internal/db/migrations/000001_init.up.sql` with new tables. Follow existing field ordering convention (id → FKs → identity → domain → state → etag → audit → timestamps).

**New tables:**

```sql
-- ============================================================================
-- AI chat — conversations
-- ============================================================================
CREATE TABLE conversations (
    id              UUID PRIMARY KEY DEFAULT uuidv7(),
    -- relationships
    org_id          UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    creator_uid     TEXT NOT NULL,  -- Pivox identity id (Keycloak `sub`) of conversation owner
    -- identity
    name            TEXT NOT NULL,  -- stable ID used in resource name
    -- domain
    title           TEXT NOT NULL DEFAULT '',
    description     TEXT NOT NULL DEFAULT '',
    archived        BOOLEAN NOT NULL DEFAULT FALSE,
    pinned          BOOLEAN NOT NULL DEFAULT FALSE,
    message_count   INTEGER NOT NULL DEFAULT 0,
    last_message_time TIMESTAMPTZ,
    -- etag/revision
    etag            TEXT NOT NULL DEFAULT md5(now()::text),
    revision        INTEGER NOT NULL DEFAULT 1,
    -- audit
    created_by      TEXT NOT NULL DEFAULT '',
    updated_by      TEXT NOT NULL DEFAULT '',
    -- timestamps
    create_time     TIMESTAMPTZ NOT NULL DEFAULT now(),
    update_time     TIMESTAMPTZ NOT NULL DEFAULT now(),
    delete_time     TIMESTAMPTZ,
    -- constraints
    UNIQUE(org_id, name)
);
CREATE INDEX idx_conversations_org ON conversations (org_id, create_time DESC) WHERE delete_time IS NULL;
CREATE INDEX idx_conversations_creator ON conversations (org_id, creator_uid, create_time DESC) WHERE delete_time IS NULL;
CREATE INDEX idx_conversations_archived ON conversations (org_id, creator_uid) WHERE archived = FALSE AND delete_time IS NULL;

-- ============================================================================
-- AI chat — messages
-- ============================================================================
CREATE TABLE messages (
    id              UUID PRIMARY KEY DEFAULT uuidv7(),
    -- relationships
    conversation_id UUID NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
    -- identity
    name            TEXT NOT NULL,
    -- domain
    role            TEXT NOT NULL,  -- "user" | "assistant" | "system" | "tool"
    parts           JSONB NOT NULL DEFAULT '[]',  -- serialized repeated MessagePart
    -- ordering
    sequence        BIGINT NOT NULL,  -- monotonic within conversation
    -- token budget tracking — heuristic (len(text)/4), not exact per-model tokenization
    token_count     INTEGER NOT NULL DEFAULT 0,
    -- timestamps
    create_time     TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- constraints
    UNIQUE(conversation_id, name),
    UNIQUE(conversation_id, sequence)
);
CREATE INDEX idx_messages_conversation ON messages (conversation_id, sequence);

-- ============================================================================
-- AI chat — artifacts
-- ============================================================================
CREATE TABLE artifacts (
    id              UUID PRIMARY KEY DEFAULT uuidv7(),
    -- relationships
    conversation_id UUID NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
    -- identity
    name            TEXT NOT NULL,
    -- domain
    type            TEXT NOT NULL,   -- "code" | "markdown" | "svg" | "image" | ...
    title           TEXT NOT NULL DEFAULT '',
    description     TEXT NOT NULL DEFAULT '',
    latest_version_id UUID REFERENCES artifact_versions(id) DEFERRABLE INITIALLY DEFERRED,
    -- timestamps
    create_time     TIMESTAMPTZ NOT NULL DEFAULT now(),
    update_time     TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- constraints
    UNIQUE(conversation_id, name)
);
CREATE INDEX idx_artifacts_conversation ON artifacts (conversation_id, create_time DESC);

-- ============================================================================
-- AI chat — artifact_versions
-- Either inline_* columns are set, or asset_version_name is set. Enforced by
-- CHECK constraint. No duplication — binary content lives in the asset system.
-- ============================================================================
CREATE TABLE artifact_versions (
    id                   UUID PRIMARY KEY DEFAULT uuidv7(),
    -- relationships
    artifact_id          UUID NOT NULL REFERENCES artifacts(id) ON DELETE CASCADE,
    -- identity
    name                 TEXT NOT NULL,  -- e.g. "v1", "v2"
    -- domain — inline mode (small text artifacts: code, markdown, svg)
    inline_data          BYTEA,
    inline_content_type  TEXT,
    inline_size_bytes    BIGINT CHECK (inline_size_bytes IS NULL OR inline_size_bytes <= 1048576),  -- 1 MB cap for inline text
    -- domain — asset mode (binary artifacts: image, pdf, video)
    asset_version_name   TEXT,  -- "organizations/.../assets/.../versions/..." pointer
    -- ordering
    sequence             INTEGER NOT NULL,  -- v1 = 1, v2 = 2, ...
    -- timestamps
    create_time          TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- constraints
    UNIQUE(artifact_id, name),
    UNIQUE(artifact_id, sequence),
    CHECK (
        (inline_data IS NOT NULL AND inline_content_type IS NOT NULL AND inline_size_bytes IS NOT NULL AND asset_version_name IS NULL)
        OR
        (inline_data IS NULL AND inline_content_type IS NULL AND inline_size_bytes IS NULL AND asset_version_name IS NOT NULL)
    )
);
CREATE INDEX idx_artifact_versions_artifact ON artifact_versions (artifact_id, sequence DESC);
CREATE INDEX idx_artifact_versions_asset ON artifact_versions (asset_version_name) WHERE asset_version_name IS NOT NULL;
```

**Notes:**

- `parts` stored as `JSONB` so we can write proto `MessagePart` oneof values serialized to JSON (via `protojson`). Read path unmarshals back to proto. Alternative is a shredded table (`message_parts`) but JSONB is simpler for v1 and Postgres GIN indexes make filtering viable if needed.
- `latest_version_id` is a deferred FK so we can write an artifact + its first version in the same transaction.
- Cascade deletes: deleting a conversation cascades to messages and artifacts; deleting an artifact cascades to versions; deleting an organization cascades to all conversations. Matches the resource hierarchy.
- `asset_version_name` is a plain string pointer, not a real FK — the asset system is its own table tree and we don't want cross-schema FK coupling. If an upstream asset version is deleted, the artifact version is **orphaned**: fetching its content returns 404 and the client renders a "content unavailable" placeholder. Acceptable for v1; revisit if it becomes a real UX problem.
- The `idx_artifact_versions_asset` partial index lets the asset system (or a cleanup job) find all artifact versions pointing to a given asset version — useful for cascade cleanup if we ever want to enforce consistency.
- No `creator` FK to a `users` table — using the raw Pivox identity id (the Keycloak `sub`; same as existing patterns in `organizations`, `projects`).
- **1 MB inline size cap**: enforced via CHECK. Asset-backed versions have no cap here — they use whatever the asset system enforces.
- Regenerate the migration bundle via Pivox's existing migration regen process (confirm with `make migrate` or equivalent — will verify during implementation).

### Phase 3 — sqlc queries (~0.5 day)

Add query files to `internal/db/queries/`:

**`conversations.sql`**
- `CreateConversation :one`
- `GetConversationByName :one` — by `(org_id, name)`
- `GetConversationByID :one`
- `ListConversationsByCreator :many` — with `LIMIT $2 OFFSET $3`, filtered by `archived`
- `CountConversationsByCreator :one`
- `UpdateConversation :one` — uses `sqlc.narg()` for optional fields (title, description, archived, pinned), bumps revision + etag + update_time
- `DeleteConversation :exec` — soft delete (sets `delete_time`)
- `IncrementConversationMessageCount :exec` — bumps `message_count` and `last_message_time`, called from streaming handler

**`messages.sql`**
- `CreateMessage :one` — called only by streaming handler. Params include `token_count` computed by the handler via `len(text)/4` heuristic before insert
- `GetMessageByName :one`
- `ListMessagesByConversation :many` — ordered by `sequence`, paginated
- `ListMessagesForModelBudget :many` — returns the newest messages that fit within a token budget, ordered chronologically. Uses a window function for the running total:
  ```sql
  -- name: ListMessagesForModelBudget :many
  SELECT id, conversation_id, name, role, parts, sequence, token_count, create_time
  FROM (
      SELECT *,
             SUM(token_count) OVER (ORDER BY sequence DESC) AS running_tokens
      FROM messages
      WHERE conversation_id = $1
  ) windowed
  WHERE running_tokens <= $2
  ORDER BY sequence ASC;
  ```
- `CountMessagesByConversation :one`
- `SumTokensByConversation :one` — `SELECT COALESCE(SUM(token_count), 0) FROM messages WHERE conversation_id = $1`. For analytics / UI "conversation size" display
- `GetNextSequenceForConversation :one` — `SELECT COALESCE(MAX(sequence), 0) + 1 FROM messages WHERE conversation_id = $1` (wrapped in a transaction with the insert)

**`artifacts.sql`**
- `CreateArtifact :one`
- `GetArtifactByName :one`
- `ListArtifactsByConversation :many`
- `CountArtifactsByConversation :one`
- `DeleteArtifact :exec` — hard delete, cascades to versions
- `UpdateArtifactLatestVersion :exec` — called when a new version is committed

**`artifact_versions.sql`**
- `CreateInlineArtifactVersion :one` — increments sequence via subquery, writes `inline_*` columns
- `CreateAssetArtifactVersion :one` — increments sequence via subquery, writes `asset_version_name`
- `GetArtifactVersionByName :one` — metadata-shaped row (all columns, client decides which mode it's in). Used by `GetArtifactVersion` resource method to build the proto. `inline_data` is always returned here; for large inline payloads this is fine because the cap is 1 MB and individual version reads are rare (the hot path is `:content`)
- `GetArtifactVersionForContent :one` — returns `(inline_data, inline_content_type, inline_size_bytes, asset_version_name, artifact_id)` for the HTTP `:content` handler. Same shape as `GetArtifactVersionByName` but semantically scoped to the content-serving flow
- `ListArtifactVersionsByArtifact :many` — **returns only metadata columns**, strips `inline_data` to avoid loading potentially-MB of bytes per row. sqlc supports column-level projections via explicit column selection
- `CountArtifactVersionsByArtifact :one`
- `DeleteArtifactVersion :exec`
- `IsOnlyArtifactVersion :one` — `SELECT COUNT(*) = 1 FROM artifact_versions WHERE artifact_id = $1` — used by the DeleteArtifactVersion handler to know when to cascade up

Follow existing query-file conventions: sqlc annotations, RETURNING on mutations, `sqlc.narg()` for optional update fields.

### Phase 4 — sqlc gen (~5 min)

Run `sqlc generate` from `internal/db/`. Outputs new types and query functions into `internal/db/generated/`:
- Row types: `Conversation`, `Message`, `Artifact`, `ArtifactVersion`
- Params types: one per query
- `Querier` interface extended with new methods

No schema changes needed to `sqlc.yaml` — the new files are picked up automatically from `queries/`.

### Phase 5 — Go service implementation (~2–3 days)

New package `internal/service/aichat/` with files split by concern:

```
internal/service/aichat/
├── server.go                   AiChat gRPC server struct, constructor, unimplemented embed
├── names.go                    Resource name parsing + building helpers
├── conversations.go            Conversation CRUD methods
├── messages.go                 Message Get/List methods
├── artifacts.go                Artifact Get/List/Delete methods
├── artifact_versions.go        ArtifactVersion Get/List/Delete methods (inline + asset modes)
├── stream.go                   AiChat.Stream bidi RPC handler
├── stream_events.go            ServerEvent / ClientEvent dispatch helpers
├── content_handler.go          HTTP handler for :content endpoint (inline bytes + asset redirect)
├── sse_handler.go              HTTP SSE handler for /v1/ai:stream (Vercel AI SDK format)
├── sse_translate.go            ServerEvent proto ↔ Vercel SSE event translation
├── tools/
│   ├── registry.go             Server tool registry (empty for v1)
│   └── tool.go                 Tool interface + ToolDefinition
├── model/
│   ├── language_model.go       LanguageModel interface + Message / ToolDefinition types
│   ├── ollama.go               OllamaAdapter (qwen3-vl via ollama Go client)
│   └── ollama_test.go
├── conversations_test.go       TDD unit tests (mocked queries)
├── messages_test.go
├── artifacts_test.go
├── artifact_versions_test.go   Covers inline mode, asset mode, CHECK constraint enforcement
├── stream_test.go              Streaming handler unit tests (mocked model + queries)
├── content_handler_test.go     HTTP handler unit tests (inline + asset redirect paths)
├── sse_handler_test.go         SSE handler unit tests (translation, flush, error paths)
├── sse_translate_test.go       ServerEvent → SSE translation table tests
└── server_integration_test.go  //go:build dev — real DB, real gRPC, real HTTP (content + SSE)
```

**Server struct**:

```go
type Server struct {
    aiv1.UnimplementedAiChatServer
    pool     db.DBTX
    queries  db.Querier
    auth     authn.Service  // for org membership checks
    model    model.LanguageModel
    tools    *tools.Registry
    logger   *slog.Logger
}
```

**Resource name parser** (`names.go`): inline helpers mirroring `assets` service pattern.

```go
func parseConversationName(name string) (org, conv string, err error)
func parseConversationParent(parent string) (org string, err error)
func parseMessageName(name string) (org, conv, msg string, err error)
func parseArtifactName(name string) (org, conv, art string, err error)
func parseArtifactVersionName(name string) (org, conv, art, ver string, err error)
func buildConversationName(org, conv string) string
func buildMessageName(org, conv, msg string) string
func buildArtifactName(org, conv, art string) string
func buildArtifactVersionName(org, conv, art, ver string) string
```

**Error handling**: reuse `apierr.HandleResourceError(err, "Conversation", name)` from existing services.

**Auth**: get `identityID := server.MustPivoxUserID(ctx)` (the caller's Pivox `identities.id` UUID — for Keycloak tokens the `sub` claim IS the identity id, verified by `internal/oidc`; no provider-specific custom claim needed), look up org membership via existing `iam.Helper`, reject unauthorized requests with `PermissionDenied`. For List operations, filter by `creator_id = identityID` unless the user has an org-admin role.

**Converters** (`internal/convert/aichat.go`):

```go
func ConversationToProto(row db.Conversation, orgName string) *aiv1.Conversation
func MessageToProto(row db.Message, convName string) (*aiv1.Message, error)  // unmarshals JSONB parts
func ArtifactToProto(row db.Artifact, convName string) *aiv1.Artifact
func ArtifactVersionToProto(row db.ArtifactVersion, artName string) *aiv1.ArtifactVersion
```

### Phase 6 — Stream RPC handler + model adapter (~2 days)

**Conversation history management for the model call** — token-budget truncation (not summarization) for v1:

1. When the stream handler needs to build the model prompt, it calls `queries.ListMessagesForModelBudget(ctx, convID, budget)` where `budget` comes from config (`ai_chat.model_context_budget`, default `22500` for qwen3-vl/Ollama on a 32k-context build: `(32000 - 2000 reserved for response) × 0.75 safety = 22500`)
2. The window-function query returns the newest messages that fit, in chronological order, directly from SQL — no Go-side walking
3. Handler prepends the system prompt and passes the resulting `[]Message` to the `LanguageModel` adapter

**Token counting** uses a simple character-based heuristic at message write time:

```go
// internal/service/aichat/tokens.go
func estimateTokens(text string) int {
    if len(text) == 0 { return 0 }
    return (len(text) + 3) / 4  // len/4 rounded up — rough ASCII-English approximation
}
```

No tokenizer dependency. Approximation drift (typically 10–25% off per-model) is absorbed by the 25% safety buffer baked into the budget math. Upgrade to a real per-provider tokenizer when billing or tighter accuracy becomes a need — not a v1 concern.

**Summarization (rolling window with LLM-generated summaries)** is a documented follow-up, not v1. Flagged in the risks section.


**`LanguageModel` interface**:

```go
package model

type LanguageModel interface {
    Stream(ctx context.Context, req StreamRequest) (StreamReader, error)
}

type StreamRequest struct {
    Messages     []Message         // conversation history
    Tools        []ToolDefinition  // server + client tools available to the model
    SystemPrompt string
    Temperature  float32
}

type Message struct {
    Role    string           // "user" | "assistant" | "system" | "tool"
    Parts   []MessagePart
}

type MessagePart struct {
    Type       string           // "text" | "tool_call" | "tool_result" | "image"
    Text       string
    ToolCall   *ToolCall
    ToolResult *ToolResult
    ImageURL   string
}

type ToolDefinition struct {
    Name        string
    Description string
    InputSchema []byte  // JSON Schema
    ServerSide  bool    // true if executed by Go, false if forwarded to client
}

type StreamReader interface {
    Next(ctx context.Context) (ModelEvent, error)  // returns io.EOF when done
    Close() error
}

type ModelEvent struct {
    Kind      string  // "text_delta" | "tool_call_start" | "tool_call_delta" | "tool_call_complete" | "finish" | "error"
    Text      string
    ToolCall  *ToolCall
    Error     error
}
```

**OllamaAdapter**: implements `LanguageModel` using `github.com/ollama/ollama/api` client.

- Translates `StreamRequest` → Ollama `ChatRequest` (with tool definitions mapped to Ollama's tool format)
- Consumes Ollama's streaming `ChatResponse` via `Chat(ctx, req, func)`
- Emits `ModelEvent`s on a channel wrapped in the `StreamReader` interface
- Translates Ollama `ToolCall` output → `ModelEvent{Kind: "tool_call_complete"}`
- Handles Ollama errors → `ModelEvent{Kind: "error"}` with `google.rpc.Status`-compatible details

**Stream handler** (`stream.go`):

```go
func (s *Server) Stream(stream aiv1.AiChat_StreamServer) error {
    ctx := stream.Context()
    identityID := server.MustPivoxUserID(ctx)
    
    // 1. Wait for first ClientEvent — must be UserMessage with conversation name
    firstMsg, err := stream.Recv()
    if err != nil { return err }
    userMsg := firstMsg.GetMessage()
    if userMsg == nil { return status.Error(codes.InvalidArgument, "first event must be UserMessage") }
    
    // 2. Load conversation, verify ownership
    conv, err := s.loadConversation(ctx, userMsg.ConversationName, uid)
    if err != nil { return err }
    
    // 3. Load conversation history (LIMIT 100 most recent or paginate)
    history, err := s.loadMessageHistory(ctx, conv.ID)
    if err != nil { return err }
    
    // 4. Persist the new user message
    userDbMsg, err := s.saveUserMessage(ctx, conv.ID, userMsg)
    if err != nil { return err }
    
    // 5. Emit TextStart for assistant response
    assistantSeq := userDbMsg.Sequence + 1
    s.emitTextStart(stream, assistantSeq)
    
    // 6. Call the model with the full history + new message
    modelReq := model.StreamRequest{
        Messages: buildModelMessages(history, userMsg),
        Tools:    s.tools.ToDefinitions(),  // empty for v1
        SystemPrompt: s.defaultSystemPrompt(conv),
    }
    reader, err := s.model.Stream(ctx, modelReq)
    if err != nil { return s.emitError(stream, err) }
    defer reader.Close()
    
    // 7. Pump model events → ServerEvents, persist as we go
    var assistantText strings.Builder
    for {
        evt, err := reader.Next(ctx)
        if err == io.EOF { break }
        if err != nil { return s.emitError(stream, err) }
        
        switch evt.Kind {
        case "text_delta":
            assistantText.WriteString(evt.Text)
            s.emitTextDelta(stream, evt.Text)
        case "tool_call_complete":
            // Server-side: execute the tool, send result back to model
            // Client-side: emit ToolInputAvailable, wait for ClientEvent.ToolOutput
            s.handleToolCall(ctx, stream, reader, evt.ToolCall)
        case "finish":
            break
        }
    }
    
    // 8. Emit TextEnd, persist assistant message, emit Done
    s.emitTextEnd(stream)
    if _, err := s.saveAssistantMessage(ctx, conv.ID, assistantSeq, assistantText.String()); err != nil {
        s.logger.Error("failed to persist assistant message", "error", err)
    }
    s.emitDone(stream)
    
    return nil
}
```

**Simplifications for v1**:
- No artifact generation yet (artifact framework exists in protos + DB but the model doesn't emit artifacts until specific tools that produce them are registered)
- No reasoning events (qwen3-vl doesn't do extended thinking like Claude)
- Server and client tool machinery are both wired end-to-end. Registries are empty in v1, so no tools actually execute — but both paths work on day one. Adding a tool in a follow-up is just a registration call, not a new architectural phase
- Cancellation via `r.Context().Done()` / `stream.Context().Done()` on either gRPC or SSE surface — no explicit cancel event needed

**Tool call dispatch — both server and client tools, stateless per turn**:

Server tools (executed in Go during the same stream):
1. Model emits `tool_call_complete` for a server-registered tool
2. Server tool registry looks up the handler, executes it
3. Result is appended to the in-flight model context
4. Same model call continues emitting text_deltas after receiving the tool result
5. All in one model call, one stream, one turn

Client tools (executed by the caller — native app or web, via the next turn):
1. Model emits `tool_call_complete` for a tool that's NOT in the server registry
2. Server persists the `tool_call` as a message part (standard conversation history), streams the `ToolInputAvailable` event to the client
3. Model turn ends, stream closes cleanly
4. Client executes the tool locally
5. Client POSTs a new request (gRPC `ClientEvent.ToolOutput` OR SSE `POST /v1/ai:stream` with `tool_output` event body)
6. Server validates the `tool_call_id` matches an un-resolved tool call in the conversation, appends the `tool_result` message, starts a **fresh model call** with the updated history
7. New stream returns the continuation

The client tool path is **stateless per turn** — no in-flight model state to preserve. Every continuation is a new model call with the accumulated conversation history. Same code path that handles a user message continuation, just triggered by a different `ClientEvent` / request body shape. Matches Vercel AI SDK's approach to client tools with `addToolOutput` + `sendAutomaticallyWhen`.

### Phase 7 — HTTP content endpoint + SSE stream endpoint (~1 day)

Two HTTP handlers registered on the same `gwMux` as all other REST gateway routes. One HTTP port, one mux, one auth middleware.

#### 7a. `:content` handler — inline artifact bytes only

**Path**: `GET /v1/organizations/{org}/conversations/{conv}/artifacts/{art}/versions/{ver}:content`

**Behavior**: serves bytes for inline artifact versions only.

- **Inline mode** (row has `inline_data`): serves bytes directly from the row with `Content-Type: inline_content_type`, ETag, and `Cache-Control: public, max-age=31536000, immutable`
- **Asset mode** (row has `asset_version_name`): **not served by this handler**. Returns `404 — use GetAssetVersion`. Clients that see an asset-backed artifact version call `GetAssetVersion` on the asset system, read `storage_url` from the response, and fetch directly from the gateway with their session cookie. The artifact system stays uncoupled from storage internals

**`content_handler.go`** — inline-only, no asset system coupling:

```go
type ContentHandler struct {
    queries db.Querier
    auth    authn.Service
    logger  *slog.Logger
}

func (h *ContentHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
    identity, err := h.authenticate(r)
    if err != nil {
        http.Error(w, "unauthorized", http.StatusUnauthorized)
        return
    }

    name, ok := parseContentPath(r.URL.Path)
    if !ok {
        http.NotFound(w, r)
        return
    }

    row, err := h.queries.GetArtifactVersionForContent(r.Context(), name)
    if err != nil {
        http.NotFound(w, r)
        return
    }

    if !h.canAccess(r.Context(), identity.UID, row) {
        http.Error(w, "forbidden", http.StatusForbidden)
        return
    }

    // Asset-backed versions are fetched via the asset system directly.
    // Client reads asset_version from the ArtifactVersion proto, calls
    // GetAssetVersion, and uses the storage_url field. This handler only
    // serves inline content.
    if !row.InlineData.Valid {
        http.Error(w, "artifact content is asset-backed; fetch via GetAssetVersion", http.StatusNotFound)
        return
    }

    etag := `"` + name + `"`
    if r.Header.Get("If-None-Match") == etag {
        w.WriteHeader(http.StatusNotModified)
        return
    }
    w.Header().Set("Content-Type", row.InlineContentType.String)
    w.Header().Set("Content-Length", strconv.FormatInt(row.InlineSizeBytes.Int64, 10))
    w.Header().Set("ETag", etag)
    w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
    if _, err := w.Write(row.InlineData.Bytes); err != nil {
        h.logger.Warn("failed to write artifact content", "name", name, "error", err)
    }
}
```

Wiring:

```go
contentHandler := aichat.NewContentHandler(queries, authSvc, logger)
if err := gwMux.HandlePath(
    "GET",
    "/v1/organizations/{org}/conversations/{conv}/artifacts/{art}/versions/{ver}:content",
    func(w http.ResponseWriter, r *http.Request, _ map[string]string) {
        contentHandler.ServeHTTP(w, r)
    },
); err != nil {
    return fmt.Errorf("register artifact content handler: %w", err)
}
```

#### 7b. SSE stream handler — `POST /v1/ai:stream`

Mirrors Vercel AI SDK UI message stream format so any future web client can use `@ai-sdk/react`'s `useChat` directly. Native clients continue to use the bidi gRPC `AiChat.Stream` — this is the web adapter.

**Request**: `POST /v1/ai:stream`, JSON body:
```json
{
  "conversation_name": "organizations/acme/conversations/abc",
  "message": { "role": "user", "parts": [{ "type": "text", "text": "..." }] },
  "model": "qwen3-vl"
}
```

**Response**: `text/event-stream` with Vercel AI SDK UI message stream events:
```
data: {"type":"text-start","id":"msg-1"}

data: {"type":"text-delta","id":"msg-1","delta":"Hello"}

data: {"type":"text-delta","id":"msg-1","delta":" there"}

data: {"type":"text-end","id":"msg-1"}

data: {"type":"data-artifact-start","data":{"id":"art-1","type":"code","title":"example.py"}}

data: {"type":"data-artifact-end","data":{"id":"art-1","payload":{"inline":{"size_bytes":420,"content_type":"text/x-python"}}}}

data: {"type":"finish"}
```

Artifact events ride as `data-*` custom parts in SSE (Vercel's documented extension point), translated from the first-class proto `ArtifactStart/Delta/End/Error` messages internally. The SSE surface follows Vercel's shape; the gRPC surface uses the typed proto. Two formats, one set of backend events.

**Implementation** — self-dial to the local gRPC server:

```go
type SSEHandler struct {
    grpcClient aiv1.AiChatClient  // dialed to localhost gRPC server
    auth       authn.Service
    logger     *slog.Logger
}

func (h *SSEHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
    // 1. Auth
    identity, err := h.authenticate(r)
    if err != nil {
        http.Error(w, "unauthorized", http.StatusUnauthorized)
        return
    }
    
    // 2. Decode JSON body → conversation_name + user message
    var req sseStreamRequest
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        http.Error(w, "bad request", http.StatusBadRequest)
        return
    }
    
    // 3. SSE headers + flush
    w.Header().Set("Content-Type", "text/event-stream")
    w.Header().Set("Cache-Control", "no-cache")
    w.Header().Set("Connection", "keep-alive")
    flusher := w.(http.Flusher)
    
    // 4. Open gRPC bidi stream to local server, forwarding the auth token
    ctx := metadata.AppendToOutgoingContext(r.Context(), "authorization", r.Header.Get("Authorization"))
    stream, err := h.grpcClient.Stream(ctx)
    if err != nil {
        sseError(w, flusher, err)
        return
    }
    
    // 5. Send user message as the first ClientEvent
    if err := stream.Send(&aiv1.ClientEvent{...}); err != nil {
        sseError(w, flusher, err)
        return
    }
    
    // 6. Pump ServerEvents → SSE events
    for {
        ev, err := stream.Recv()
        if err == io.EOF { break }
        if err != nil {
            sseError(w, flusher, err)
            return
        }
        sseLine := translateToSSE(ev)  // ServerEvent proto → Vercel SSE format
        fmt.Fprintf(w, "data: %s\n\n", sseLine)
        flusher.Flush()
    }
}
```

Wiring:

```go
// Dial local gRPC server with shared dial options
aiChatClient := aiv1.NewAiChatClient(grpcConn)
sseHandler := aichat.NewSSEHandler(aiChatClient, authSvc, logger)
if err := gwMux.HandlePath(
    "POST",
    "/v1/ai:stream",
    func(w http.ResponseWriter, r *http.Request, _ map[string]string) {
        sseHandler.ServeHTTP(w, r)
    },
); err != nil {
    return fmt.Errorf("register SSE stream handler: %w", err)
}
```

Both handlers reuse the same auth service, same DB pool, same logger, same mux. The SSE handler self-dials to localhost so the translation logic stays in one place (the gRPC `AiChat.Stream` method is the canonical implementation; the SSE handler is a thin protocol adapter).

### Phase 8 — Main server wiring (~0.5 day)

Extend `cmd/pivox-cloud/main.go`:

1. Import `aiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/ai/v1"` and `"github.com/dashkan/pivox/internal/service/aichat"`
2. Read new config flags:
   - `--ollama-url` (default `http://localhost:11434`)
   - `--ollama-model` (default `qwen3-vl`)
   - `--model-context-budget` (default `22500` — token budget for conversation history sent to the model)
3. Construct model adapter: `llm, err := aichat.NewOllamaModel(cfg.OllamaURL, cfg.OllamaModel)`
4. Construct tool registry: `toolRegistry := aichat.NewToolRegistry()` (empty for v1)
5. Construct server: `aiServer := aichat.NewServer(pool, queries, authSvc, llm, toolRegistry, logger)`
6. Register gRPC: `aiv1.RegisterAiChatServer(grpcServer, aiServer)`
7. Register REST gateway: add `aiv1.RegisterAiChatHandlerFromEndpoint` to the registration loop (handles all AIP resource methods via `/v1/...` routes)
8. Register `:content` HTTP handler on `gwMux.HandlePath("GET", "/v1/...:content", ...)` — see Phase 7a
9. Dial a local gRPC client for the SSE handler: `aiChatClient := aiv1.NewAiChatClient(grpcConn)` (same `grpcConn` used by REST gateway)
10. Register SSE handler on `gwMux.HandlePath("POST", "/v1/ai:stream", ...)` — see Phase 7b
11. Update config type (`internal/config/config.go`) with `AIChatConfig { OllamaURL, OllamaModel, ModelContextBudget }` fields

### Phase 9 — Tests (~2 days, interleaved with other phases as TDD)

**Unit tests** (written first, per TDD):

- `conversations_test.go`: each resource method against mocked `db.Querier`. Parse errors, auth errors, org-mismatch, pagination, update field mask, delete cascade.
- `messages_test.go`: Get/List against mocked querier. No Create/Update/Delete — those happen through Stream only.
- `artifacts_test.go`: Get/List/Delete. Delete cascade assertion.
- `artifact_versions_test.go`: Get/List/Delete in both storage modes. Inline mode: create, read, verify `inline_data`/`inline_content_type`/`inline_size_bytes` round-trip. Asset mode: create with `asset_version_name`, verify only asset pointer is set. CHECK constraint: attempt to write both modes at once → rejected. Delete-last-version cascades to parent Artifact. Inline size cap (1 MB) enforced.
- `stream_test.go`: mocked `LanguageModel` + mocked querier. Scenarios:
  - Happy path: user message in → text deltas → done → message persisted
  - Model error mid-stream → ErrorEvent → stream ends with status
  - Empty conversation name → InvalidArgument
  - Unauthorized user → PermissionDenied
  - Tool call complete (server tool) → tool dispatch (even if registry is empty, dispatch path is exercised)
- `content_handler_test.go`: mocked querier + mocked auth service. Scenarios:
  - Inline happy path: valid token, valid path, inline row → 200 with bytes + `inline_content_type` headers
  - Asset happy path: valid token, valid path, asset-backed row → 302 redirect to asset system `:content` URL
  - Missing auth → 401
  - Invalid path → 404
  - Non-existent version → 404
  - Cross-org access → 403
  - If-None-Match matches (inline only) → 304
- `sse_handler_test.go`: mocked gRPC `AiChatClient` + mocked auth. Scenarios:
  - Auth failure → 401
  - Malformed JSON body → 400
  - Happy path: upstream gRPC stream yields TextStart/Delta/End/Finish → handler writes matching `data: {...}` SSE frames → flusher called
  - Upstream error → handler writes `data: {"type":"error",...}` and closes
  - Client disconnect mid-stream → handler exits cleanly, upstream gRPC stream cancelled via context
- `sse_translate_test.go`: table-driven translation tests for every `ServerEvent` oneof case → Vercel SSE event shape. Fixture-driven to catch any drift in the translation map.
- `model/ollama_test.go`: mocked HTTP client testing the translation between Ollama's streaming format and `ModelEvent`s
- `names_test.go`: parse + build round-trips for all resource patterns, invalid-input error cases

**Integration test** (`server_integration_test.go`, `//go:build dev`):
- Real Postgres via `testutil.SetupTestDB`
- Real in-process gRPC server via `testutil.SetupGRPCServer`
- Real HTTP content handler + SSE handler on a local listener
- Mock `LanguageModel` (we don't want to depend on a running Ollama in CI)
- End-to-end flow: create org + user + conversation → open gRPC bidi stream → send user message → receive text deltas → verify message persisted → call `ListMessages` → verify order → create inline artifact version → fetch via HTTP `:content` → verify bytes match → create asset-backed artifact version → fetch via HTTP `:content` → verify 302 redirect to asset system
- SSE flow: POST to `/v1/ai:stream` with conversation + user message → parse SSE response → verify same events arrive as gRPC stream would deliver
- Verify cascade deletes: delete conversation → messages + artifacts + versions gone
- Verify auth: cross-user access returns `PermissionDenied` / 403
- Verify size cap: attempt to create a 2 MB inline artifact version → rejected by CHECK constraint
- Verify CHECK constraint: attempt to create a version with both inline AND asset fields set → rejected

**End-to-end Ollama smoke test** (manual or gated):
- Separate test file that requires `OLLAMA_URL` env var set and a real running Ollama with `qwen3-vl` pulled
- Single test that sends "say hello" and asserts a non-empty response
- Not in CI, run manually before merging

Follow existing test file conventions: use `testify/require`, table-driven tests, `testutil.SetupTestDB` for integration, mocks for unit tests.

### Phase 10 — Config and docs (~0.5 day)

- **Config**: extend `internal/config/config.go` with `AIChatConfig { OllamaURL, OllamaModel, ModelContextBudget, ArtifactMaxBytes }`, wire flags in main.go.
- **Makefile**: no new targets needed — existing `make build`, `make test`, `make proto-gen` cover it.
- **Docs**: a short note in `docs/` describing the new service, the proto contract, and how to run against a local Ollama for development. Not a full design doc — the existing `docs/ai/plan.md` already covers the architecture.

## Risks and open questions

1. **Ollama tool calling format with qwen3-vl** — need to verify qwen3-vl's tool call output format matches Ollama's standard `tool_calls` schema. If not, the OllamaAdapter's translation layer handles quirks. Spike during Phase 6.
2. **Message history token budget** — v1 uses SQL window-function truncation with a `len(text)/4` heuristic stored in `messages.token_count`. Budget default `22500` (75% of a 32k Ollama context window, minus ~2k reserved for response). Drops oldest messages when over budget. No summarization — follow-up when users hit long-conversation pain.
3. **JSONB `parts` column trade-off** — pros: simple, flexible, schema-light. Cons: no foreign-key integrity for tool calls that reference artifacts. For v1, accept the trade-off. Follow-up: consider shredded `message_parts` table when specific query patterns demand it.
4. **`deferred FK` on artifacts.latest_version_id** — requires `DEFERRABLE INITIALLY DEFERRED` to allow inserting both in one transaction. Test explicitly.
5. **Orphaned artifact versions when upstream asset version is deleted** — `asset_version_name` is a plain string pointer, not a real FK. If the asset system deletes a version that an artifact version points to, the artifact version is orphaned: `:content` returns 404, client renders "content unavailable" placeholder. Acceptable for v1. Follow-up: consider a real FK or a cleanup job that walks the `idx_artifact_versions_asset` partial index and nulls out orphans when asset deletion is observed.
6. **Asset system content URL shape** — the `:content` handler's redirect to the asset system needs the exact URL pattern. Confirm during Phase 7a: read `internal/service/assets/` or `internal/service/requests/` to find the existing asset content serving route (if any) and mirror it. If the asset system doesn't yet serve content over HTTP, either add that capability first or document the gap and temporarily inline asset bytes through the artifact `:content` handler.
7. **SSE handler self-dial** — the SSE handler opens a gRPC client to the local gRPC server to reuse `AiChat.Stream`. Requires the gRPC server to be up before the SSE handler starts accepting requests, and proper context cancellation so the upstream stream tears down when the SSE client disconnects. Test the disconnect path explicitly.
8. **Streaming write backpressure** — if the Swift/WinUI client is slow to consume, the gRPC bidi stream builds up a buffer. For v1 we don't handle this explicitly — grpc-go handles basic flow control. Follow-up: explicit buffer management if we see backpressure issues.
9. **Orphaned streams on disconnect** — if the client disconnects mid-stream, we still need to persist the partial assistant message (or mark it errored). Stream handler's cleanup path catches this via `ctx.Err()`. Applies to both the gRPC `Stream` method and the SSE handler.
10. **Auth interceptor only sets UID** — org membership is not in the context. Every resource method does a DB lookup to resolve `uid → (org, member)`. This is the existing Pivox pattern; AiChat doesn't introduce anything new here.

## Open items to resolve before starting

None that block writing the protos and schema. Items to confirm during implementation:

- **Exact Ollama Go client version to pin** (check what's compatible with qwen3-vl tool calling)
- **Whether `DeleteConversation` is soft delete or hard delete** — existing Pivox pattern is soft delete (`delete_time`). Proposed plan uses soft delete to match. Confirm during review.
- **Client tool dispatch** — wired end-to-end in v1 (server persists the tool call, closes the stream, accepts a continuation with `tool_output`). Empty registries mean no real tools run, but the loop is exercised by a test harness tool.

## Execution order

When you give go:

1. **Scan** is done. Skip.
2. **Write the four proto files** (Phase 0)
3. **Run proto codegen** (Phase 1)
4. **Extend migration + add sqlc queries** (Phase 2 + 3)
5. **Run sqlc gen** (Phase 4)
6. **Write unit tests for resource methods, then implement** (Phase 5, TDD)
7. **Write unit tests for streaming, then implement stream handler + model adapter** (Phase 6, TDD)
8. **Write unit tests for content handler, then implement** (Phase 7, TDD)
9. **Wire into main.go** (Phase 8)
10. **Integration tests** (Phase 9 final pass)
11. **Manual Ollama smoke test** against local instance
12. **Run full `make test`**, resolve any failures
13. **Ready for review**

Estimated total: **1–2 days** of focused work. The plan is specific enough that implementation is mechanical — patterns match existing Pivox services, proto shapes are fixed, DB schema is drafted, tests are scoped. The only real unknown is Ollama qwen3-vl tool-call formatting, which is a small spike inside Phase 6 rather than a risk against the whole timeline.

## Out of scope for this work stream

These are tracked separately in `docs/ai/plan.md` and happen after the Go BE is shipped:

- SwiftUI / WinUI client implementation (AIElements library)
- Shared C++ core bridging between native client and `AiChat` Stream
- Actual client-tool implementations (server-side machinery and dispatch loop are in v1; the specific tools like `open_view`, `edit_asset`, etc. come with the macOS AIElements work)
- Additional model providers (Anthropic, OpenAI, Gateway)
- Artifact renderers (code/markdown/svg/pdf/etc. UI components)
- Chat UI components (Conversation, Message, PromptInput, etc.)
- The full 40+ ai-elements component library

This plan covers only what's needed for the Go backend to serve AiChat requests end to end, so frontend work can integrate against a real server.
