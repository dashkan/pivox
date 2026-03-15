-- cleanup.sql
-- Truncate all seed data in reverse dependency order.
-- Does NOT drop tables or schema — just removes data.

BEGIN;

-- Reverse of seed order: cross-cutting → project-level → org-level → orgs
TRUNCATE operations CASCADE;
TRUNCATE tag_bindings CASCADE;
TRUNCATE tag_values CASCADE;
TRUNCATE tag_keys CASCADE;
TRUNCATE iam_policies CASCADE;
TRUNCATE api_keys CASCADE;
TRUNCATE project_members CASCADE;
TRUNCATE projects CASCADE;
TRUNCATE invitation_policies CASCADE;
TRUNCATE custom_domains CASCADE;
TRUNCATE role_members CASCADE;
TRUNCATE role_permissions CASCADE;
TRUNCATE roles CASCADE;
TRUNCATE group_members CASCADE;
TRUNCATE groups CASCADE;
TRUNCATE invitations CASCADE;
TRUNCATE organizations CASCADE;

COMMIT;
