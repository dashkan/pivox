-- GetSsoConfigByOrgID looks up the SSO config row for an org, if
-- one exists. UNIQUE(org_id) ensures at most one row. Used by
-- GetSsoConfig to surface the current config to the caller.
-- name: GetSsoConfigByOrgID :one
SELECT * FROM sso_configs WHERE org_id = $1;

-- GetSsoConfigByOrgIDForUpdate is the locking variant used by
-- DeleteDomain inside its transaction to serialize the
-- "last verified domain on an enabled SSO config" precondition
-- against concurrent verified-domain deletes. Without the row
-- lock, two concurrent DeleteDomain calls against sibling
-- verified domains can both observe count >= 2 (acceptable),
-- both delete, and leave the org with zero verified domains
-- under enabled SSO. Locking the SSO config row FOR UPDATE
-- forces concurrent transactions to queue on the same row,
-- so the second tx's count sees the post-first-commit state
-- and refuses with FAILED_PRECONDITION. Returns no rows when
-- the org has no SSO config — DeleteDomain treats that as "no
-- precondition to enforce" and skips the count entirely.
-- name: GetSsoConfigByOrgIDForUpdate :one
SELECT * FROM sso_configs WHERE org_id = $1 FOR UPDATE;

-- UpsertSsoConfig is the create-or-update for the per-org SsoConfig
-- singleton. ON CONFLICT (org_id) DO UPDATE — UNIQUE(org_id)
-- ensures at most one row per org, so the upsert is unambiguous.
-- This persists the Pivox-side SSO metadata; Keycloak owns the
-- upstream provider brokering.
--
-- client_secret_ciphertext is the KMS-envelope-encrypted secret.
-- The COALESCE+NULLIF on UPDATE collapses both Go nil (binds SQL
-- NULL) and Go empty []byte (binds SQL ''::bytea) to "preserve the
-- existing ciphertext"; only a non-empty new value overwrites. There
-- is intentionally no way to clear the secret via this query — a
-- cleared secret would render an enabled SsoConfig non-functional, so
-- callers that want to disable SSO flip `enabled=false` instead.
-- name: UpsertSsoConfig :one
INSERT INTO sso_configs (
    org_id, display_name, enabled,
    oidc_config, saml_config, client_secret_ciphertext,
    created_by, updated_by
) VALUES ($1, $2, $3, $4, $5, $6, $7, $7)
ON CONFLICT (org_id) DO UPDATE SET
    display_name             = EXCLUDED.display_name,
    enabled                  = EXCLUDED.enabled,
    oidc_config              = EXCLUDED.oidc_config,
    saml_config              = EXCLUDED.saml_config,
    -- Preserve existing ciphertext unless the caller supplied a
    -- non-empty new value. See header comment for the NULL-vs-empty
    -- handling and why "clear secret" isn't an option here.
    client_secret_ciphertext = COALESCE(NULLIF(EXCLUDED.client_secret_ciphertext, ''::bytea), sso_configs.client_secret_ciphertext),
    updated_by               = EXCLUDED.updated_by,
    update_time              = now(),
    revision                 = sso_configs.revision + 1,
    etag                     = md5(now()::text || sso_configs.revision::text)
RETURNING *;
