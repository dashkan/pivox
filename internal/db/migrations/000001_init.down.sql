-- 000001_init.down.sql
-- Drop all tables in reverse dependency order.

DROP TABLE IF EXISTS auth_token_codes;
DROP TABLE IF EXISTS public_email_domains;
DROP TABLE IF EXISTS invitation_policies;
DROP TABLE IF EXISTS invitations;
DROP TABLE IF EXISTS role_members;
DROP TABLE IF EXISTS role_permissions;
DROP TABLE IF EXISTS roles;
DROP TABLE IF EXISTS permissions;
DROP TABLE IF EXISTS group_members;
DROP TABLE IF EXISTS groups;
DROP TABLE IF EXISTS project_members;
-- Drop deferred FK before dropping users/organizations
ALTER TABLE IF EXISTS organizations DROP CONSTRAINT IF EXISTS fk_organizations_owner;
DROP TABLE IF EXISTS users;
DROP TABLE IF EXISTS accounts;
DROP TABLE IF EXISTS iam_policies;
DROP TABLE IF EXISTS api_keys;
DROP TABLE IF EXISTS tag_bindings;
DROP TABLE IF EXISTS tag_values;
DROP TABLE IF EXISTS tag_keys;
DROP TABLE IF EXISTS storage_endpoints;
DROP TABLE IF EXISTS storage_agents;
DROP TABLE IF EXISTS storage_gateways;
DROP TABLE IF EXISTS projects;
DROP TABLE IF EXISTS custom_domains;
DROP TABLE IF EXISTS organizations;
DROP TABLE IF EXISTS operations;

DROP TYPE IF EXISTS credential_state;
DROP TYPE IF EXISTS endpoint_state;
DROP TYPE IF EXISTS endpoint_engine;
DROP TYPE IF EXISTS agent_state;
DROP TYPE IF EXISTS eviction_policy;
DROP TYPE IF EXISTS cert_state;
DROP TYPE IF EXISTS storage_gateway_state;
DROP TYPE IF EXISTS invitation_state;
DROP TYPE IF EXISTS project_member_type;
DROP TYPE IF EXISTS project_role;
DROP TYPE IF EXISTS role_member_type;
DROP TYPE IF EXISTS custom_domain_state;
DROP TYPE IF EXISTS resource_state;
