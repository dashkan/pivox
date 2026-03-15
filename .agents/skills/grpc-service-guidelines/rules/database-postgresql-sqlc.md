---
title: Database — PostgreSQL, sqlc, Migrations
impact: HIGH
impactDescription: ensures correct schema design and type-safe queries
tags: postgresql, pgx, sqlc, migrations, pagination
---

## Database — PostgreSQL, sqlc, Migrations

> Read this when: writing SQL queries, creating migrations, implementing pagination, or working with sqlc.

### Schema Conventions

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

### pgx Native Driver

Use `pgx/v5` natively (not through `database/sql` stdlib interface). Reasons:
- `LISTEN/NOTIFY` for `WaitOperation` (LRO)
- `pgtype` for proper UUID, timestamptz, jsonb handling
- Connection pool stats for observability metrics
- Better performance (no `database/sql` abstraction overhead)

### sqlc Configuration

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

### sqlc Query Patterns

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

### Pagination

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

### Transactions

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

### Etag Validation

Before updating, check the etag if the client provided one:

```go
if req.GetThing().GetEtag() != "" && req.GetThing().GetEtag() != existing.Etag {
    return nil, apierr.EtagMismatch(existing.Name, req.GetThing().GetEtag(), existing.Etag)
}
```
