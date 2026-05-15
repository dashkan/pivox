-- seed.sql
-- Master seed file. Run scripts/cleanup.sql first.
-- Executes per-table seed files in dependency order.

BEGIN;

\i scripts/seeds/01_organizations.sql
\i scripts/seeds/02_acme_sso.sql
\i scripts/seeds/03_invitation_policies.sql
\i scripts/seeds/04_spaces.sql
\i scripts/seeds/05_api_keys.sql
\i scripts/seeds/07_tag_keys.sql
\i scripts/seeds/08_tag_values.sql
\i scripts/seeds/09_tag_bindings.sql
\i scripts/seeds/10_storage_gateways.sql
\i scripts/seeds/11_local_corp.sql
\i scripts/seeds/12_dev_org_roles.sql
\i scripts/seeds/13_assets.sql
\i scripts/seeds/14_asset_versions.sql
\i scripts/seeds/15_dev_user_membership.sql

COMMIT;
