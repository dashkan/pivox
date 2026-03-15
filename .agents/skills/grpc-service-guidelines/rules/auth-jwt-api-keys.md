---
title: Authentication — JWT + API Keys
impact: MEDIUM
impactDescription: secures service endpoints with dual auth pattern
tags: auth, JWT, API keys, interceptor, CallerInfo
---

## Authentication — JWT + API Keys

> Read this when: implementing the auth interceptor, adding API key support, or configuring JWT validation.

### Dual Auth Pattern

The service supports two authentication methods, resolved at the gRPC interceptor level:

**1. JWT Bearer Tokens (User-facing)**

- Sent via `Authorization: Bearer <token>` header (gRPC metadata)
- Validated using `github.com/golang-jwt/jwt/v5`
- Validate claims: `iss`, `aud`, `exp`, `nbf`
- Extract subject (`sub`) and roles/scopes from claims
- JWKS fetching: cache keys in-memory with TTL, refresh on unknown `kid`

**2. API Keys (Service-to-service)**

- Sent via `x-api-key` gRPC metadata (or `X-API-Key` HTTP header via transcoding)
- Keys stored as SHA-256 hashes in PostgreSQL `api_keys` table
- Scoped permissions, optional expiry, revocation support
- Lookup: hash incoming key -> query by hash -> validate not expired/revoked

### Resolution Order

1. Check `authorization` metadata -> JWT flow
2. Check `x-api-key` metadata -> API key flow
3. Neither -> `Unauthenticated` (code 16) with `ErrorInfo{Reason: "MISSING_CREDENTIALS"}`
4. Both present -> prefer JWT, ignore API key
5. On success -> inject `CallerInfo` into context
6. On failure -> rich error with `ErrorInfo` detailing the reason

### API Keys Table Schema

```sql
CREATE TABLE api_keys (
    uid          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    key_hash     BYTEA UNIQUE NOT NULL,               -- SHA-256 of the raw key
    display_name TEXT NOT NULL DEFAULT '',
    scopes       TEXT[] NOT NULL DEFAULT '{}',          -- e.g., {"things.read", "things.write"}
    create_time  TIMESTAMPTZ NOT NULL DEFAULT now(),
    expire_time  TIMESTAMPTZ,                          -- nullable = never expires
    revoked      BOOLEAN NOT NULL DEFAULT false,
    last_used    TIMESTAMPTZ,
    created_by   TEXT NOT NULL                          -- who provisioned this key
);

CREATE INDEX idx_api_keys_key_hash ON api_keys (key_hash) WHERE NOT revoked;
```

### CallerInfo Context

```go
type CallerInfo struct {
    Subject string   // JWT sub or API key display name
    Method  string   // "jwt" or "api_key"
    Scopes  []string // From JWT claims or API key scopes
}

type contextKey struct{}

func WithCaller(ctx context.Context, ci *CallerInfo) context.Context {
    return context.WithValue(ctx, contextKey{}, ci)
}

func CallerFrom(ctx context.Context) (*CallerInfo, bool) {
    ci, ok := ctx.Value(contextKey{}).(*CallerInfo)
    return ci, ok
}
```

### Auth Metrics

Record in the interceptor (see `observability-tracing-metrics-logging.md` for metric definitions):

- `auth.attempts_total` — by method (jwt/api_key), status (ok/error)
- `auth.failures_total` — by method, reason (expired, revoked, invalid_sig, wrong_aud)

### Configuration

```
JWT_ISSUER=___
JWT_AUDIENCE=___
JWT_SIGNING_ALGORITHM=___  (e.g., RS256, ES256)
JWKS_ENDPOINT=___
```

### Testing Auth

Test cases that MUST be covered:

- Valid JWT with correct claims -> success
- Expired JWT -> `Unauthenticated` with reason `TOKEN_EXPIRED`
- JWT with wrong audience -> `Unauthenticated` with reason `WRONG_AUDIENCE`
- JWT with invalid signature -> `Unauthenticated` with reason `INVALID_SIGNATURE`
- Valid API key -> success
- Revoked API key -> `Unauthenticated` with reason `API_KEY_REVOKED`
- Expired API key -> `Unauthenticated` with reason `API_KEY_EXPIRED`
- No credentials -> `Unauthenticated` with reason `MISSING_CREDENTIALS`
- Both JWT and API key -> JWT is used
