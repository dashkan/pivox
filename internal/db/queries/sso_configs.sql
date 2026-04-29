-- GetSsoConfigByOrgID looks up the SSO config row for an org, if
-- one exists. UNIQUE(org_id) ensures at most one row. Used by
-- DeleteDomain to enforce the "last verified domain on an enabled
-- SSO config" precondition: the handler refuses to delete a
-- VERIFIED domain when removing it would leave an enabled SSO
-- config without any verified domain.
-- name: GetSsoConfigByOrgID :one
SELECT * FROM sso_configs WHERE org_id = $1;
