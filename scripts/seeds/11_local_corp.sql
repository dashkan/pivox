-- Local Corp: simple org for local dev testing
-- One org, two projects, a few tags, one gateway, one agent, one endpoint

-- Organization
INSERT INTO organizations (id, name, display_name, create_time, update_time) VALUES
    ('0192a000-0001-7000-8000-00000000000b', 'local-corp', 'Local Corp', '2026-01-01 08:00:00+00', '2026-01-01 08:00:00+00');

-- Projects
INSERT INTO projects (id, org_id, name, display_name, labels, create_time, update_time) VALUES
    ('0192a000-0003-7000-8000-0000003b0001', '0192a000-0001-7000-8000-00000000000b', 'news',   'News Production',   '{"env":"dev"}', '2026-01-02 08:00:00+00', '2026-01-02 08:00:00+00'),
    ('0192a000-0003-7000-8000-0000003b0002', '0192a000-0001-7000-8000-00000000000b', 'sports', 'Sports Production', '{"env":"dev"}', '2026-01-02 09:00:00+00', '2026-01-02 09:00:00+00');

-- Tag keys
INSERT INTO tag_keys (id, org_id, short_name, namespaced_name, description, created_by, create_time, update_time) VALUES
    ('0192a000-0004-7000-8000-0000004b0001', '0192a000-0001-7000-8000-00000000000b', 'env',      '0192a000-0001-7000-8000-00000000000b/env',      'Environment',  'admin@local.example', '2026-01-03 08:00:00+00', '2026-01-03 08:00:00+00'),
    ('0192a000-0004-7000-8000-0000004b0002', '0192a000-0001-7000-8000-00000000000b', 'priority', '0192a000-0001-7000-8000-00000000000b/priority', 'Priority level','admin@local.example', '2026-01-03 08:00:00+00', '2026-01-03 08:00:00+00');

-- Tag values
INSERT INTO tag_values (id, tag_key_id, short_name, namespaced_name, description, created_by, create_time, update_time) VALUES
    ('0192a000-0005-7000-8000-0000005b0001', '0192a000-0004-7000-8000-0000004b0001', 'dev',  '0192a000-0001-7000-8000-00000000000b/env/dev',  'Development', 'admin@local.example', '2026-01-03 09:00:00+00', '2026-01-03 09:00:00+00'),
    ('0192a000-0005-7000-8000-0000005b0002', '0192a000-0004-7000-8000-0000004b0001', 'prod', '0192a000-0001-7000-8000-00000000000b/env/prod', 'Production',  'admin@local.example', '2026-01-03 09:00:00+00', '2026-01-03 09:00:00+00'),
    ('0192a000-0005-7000-8000-0000005b0003', '0192a000-0004-7000-8000-0000004b0002', 'high', '0192a000-0001-7000-8000-00000000000b/priority/high', 'High priority', 'admin@local.example', '2026-01-03 09:00:00+00', '2026-01-03 09:00:00+00');

-- Storage gateway
INSERT INTO storage_gateways (id, org_id, name, display_name, ip_addresses, registration_token, hostname, state, created_by, create_time, update_time) VALUES
    ('0192a000-0010-7000-8000-000000100010', '0192a000-0001-7000-8000-00000000000b', 'local', 'Local Dev Gateway', '{127.0.0.1}', 'dev-token-local', 'local.storage.pivox.app', 'ACTIVE', 'admin@local.example', '2026-01-04 08:00:00+00', '2026-01-04 08:00:00+00');

-- Agent
INSERT INTO storage_agents (id, gateway_id, ip_address, hostname, version, state, cache_used_gb, join_time, last_seen_time) VALUES
    ('0192a000-0011-7000-8000-000000110010', '0192a000-0010-7000-8000-000000100010', '127.0.0.1', 'localhost', '1.0.0-alpha.1', 'CONNECTED', 0, '2026-01-04 08:05:00+00', '2026-03-23 12:00:00+00');

-- Endpoint (rustfs on localhost, cache enabled)
INSERT INTO storage_endpoints (id, gateway_id, name, display_name, configuration, cache_enabled, cache_max_size_gb, cache_eviction, cache_ttl_hours, annotations, created_by, create_time, update_time) VALUES
    ('0192a000-0012-7000-8000-000000120010', '0192a000-0010-7000-8000-000000100010', 'primary', 'Primary Storage', '{"type":"s3","endpoint_uri":"http://localhost:9000","bucket":"pivox-dev","region":"","access_key":{"access_key_id":"rustfsadmin","secret_access_key":"rustfsadmin"}}', true, 100, 'LRU', 0, '{}', 'admin@local.example', '2026-01-04 09:00:00+00', '2026-01-04 09:00:00+00');
