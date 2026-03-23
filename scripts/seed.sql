-- seed.sql
-- Master seed file. Run scripts/cleanup.sql first.
-- Executes per-table seed files in dependency order.

BEGIN;

\i scripts/seeds/01_organizations.sql
\i scripts/seeds/02_custom_domains.sql
\i scripts/seeds/03_invitation_policies.sql
\i scripts/seeds/04_projects.sql
\i scripts/seeds/05_api_keys.sql
\i scripts/seeds/06_iam_policies.sql
\i scripts/seeds/07_tag_keys.sql
\i scripts/seeds/08_tag_values.sql
\i scripts/seeds/09_tag_bindings.sql
\i scripts/seeds/10_storage_gateways.sql
\i scripts/seeds/11_local_corp.sql

COMMIT;
