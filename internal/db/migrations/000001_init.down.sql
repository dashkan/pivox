-- 000001_init.down.sql
-- Drop all tables in reverse dependency order.

-- AI chat
DROP TABLE IF EXISTS ai_artifact_versions;
DROP TABLE IF EXISTS ai_artifacts;
DROP TABLE IF EXISTS ai_messages;
DROP TABLE IF EXISTS ai_conversations;

-- Assets
DROP TABLE IF EXISTS asset_request_line_items;
DROP TABLE IF EXISTS asset_requests;
DROP TABLE IF EXISTS asset_renditions;
DROP TABLE IF EXISTS asset_versions;
DROP TABLE IF EXISTS assets;

-- Auth / IAM / org
DROP TABLE IF EXISTS delegated_auth_sessions;
DROP TABLE IF EXISTS sso_configs;
DROP TABLE IF EXISTS domains;
DROP TABLE IF EXISTS public_email_domains;
DROP TABLE IF EXISTS invitation_policies;
DROP TABLE IF EXISTS invitations;
DROP TABLE IF EXISTS space_members;
DROP TABLE IF EXISTS org_members;
DROP TABLE IF EXISTS role_permissions;
DROP TABLE IF EXISTS roles;
DROP TABLE IF EXISTS permissions;
DROP TABLE IF EXISTS group_members;
DROP TABLE IF EXISTS groups;
DROP TABLE IF EXISTS users;
DROP TABLE IF EXISTS identities;
DROP TABLE IF EXISTS api_keys;
DROP TABLE IF EXISTS dashboards;
DROP TABLE IF EXISTS tag_bindings;
DROP TABLE IF EXISTS tag_values;
DROP TABLE IF EXISTS tag_keys;
DROP TABLE IF EXISTS storage_agent_audit;
DROP TABLE IF EXISTS storage_endpoints;
DROP TABLE IF EXISTS storage_agents;
DROP TABLE IF EXISTS storage_gateways;
DROP TABLE IF EXISTS spaces;
DROP TABLE IF EXISTS organizations;
DROP TABLE IF EXISTS operations;

-- Enum types
DROP TYPE IF EXISTS line_item_state;
DROP TYPE IF EXISTS request_priority;
DROP TYPE IF EXISTS request_state;
DROP TYPE IF EXISTS rendition_type;
DROP TYPE IF EXISTS asset_media_type;
DROP TYPE IF EXISTS asset_state;
DROP TYPE IF EXISTS tag_binding_origin;
DROP TYPE IF EXISTS delegated_auth_session_state;
DROP TYPE IF EXISTS endpoint_state;
DROP TYPE IF EXISTS agent_state;
DROP TYPE IF EXISTS eviction_policy;
DROP TYPE IF EXISTS cert_state;
DROP TYPE IF EXISTS storage_gateway_state;
DROP TYPE IF EXISTS invitation_state;
DROP TYPE IF EXISTS domain_state;
DROP TYPE IF EXISTS principal_kind;
DROP TYPE IF EXISTS resource_state;

-- River schema (CASCADE drops all river_* tables created by River's
-- own migrations).
DROP SCHEMA IF EXISTS river CASCADE;
