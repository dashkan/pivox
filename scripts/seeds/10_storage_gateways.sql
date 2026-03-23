-- Storage gateways (1-2 per org, representing on-prem locations)
-- Uses rustfs defaults: rustfsadmin/rustfsadmin on localhost:9000
INSERT INTO storage_gateways (id, org_id, name, display_name, ip_addresses, registration_token, hostname, state, cache_max_size_gb, cache_eviction, cache_ttl_hours, created_by, create_time, update_time) VALUES
    -- Meridian Broadcasting: 2 gateways (HQ + West Coast facility)
    ('0192a000-0010-7000-8000-000000100001', '0192a000-0001-7000-8000-000000000001', 'meridian-hq',       'Meridian HQ',          '{127.0.0.1}', 'dev-token-meridian-hq',       'meridian-hq.storage.pivox.app',       'ACTIVE', 500, 'LRU', 0, 'admin@meridian.example',    '2025-06-01 08:00:00+00', '2025-06-01 08:00:00+00'),
    ('0192a000-0010-7000-8000-000000100002', '0192a000-0001-7000-8000-000000000001', 'meridian-west',     'Meridian West Coast',  '{127.0.0.1}', 'dev-token-meridian-west',     'meridian-west.storage.pivox.app',     'PROVISIONING', 250, 'LRU', 0, 'admin@meridian.example', '2025-07-15 10:00:00+00', '2025-07-15 10:00:00+00'),
    -- Pacific Coast Networks: 1 gateway
    ('0192a000-0010-7000-8000-000000100003', '0192a000-0001-7000-8000-000000000002', 'pacific-main',      'Pacific Main Facility', '{127.0.0.1}', 'dev-token-pacific-main',     'pacific-main.storage.pivox.app',      'ACTIVE', 1000, 'LRU', 0, 'admin@pacific.example',    '2025-06-20 09:00:00+00', '2025-06-20 09:00:00+00'),
    -- Heartland Media: 1 gateway
    ('0192a000-0010-7000-8000-000000100004', '0192a000-0001-7000-8000-000000000003', 'heartland-dc',      'Heartland Data Center', '{127.0.0.1}', 'dev-token-heartland-dc',     'heartland-dc.storage.pivox.app',      'ACTIVE', 500, 'LFU', 24, 'admin@heartland.example',  '2025-07-01 08:00:00+00', '2025-07-01 08:00:00+00'),
    -- Summit Sports: 1 gateway (offline for testing)
    ('0192a000-0010-7000-8000-000000100005', '0192a000-0001-7000-8000-000000000005', 'summit-studio',     'Summit Studio',         '{127.0.0.1}', 'dev-token-summit-studio',    'summit-studio.storage.pivox.app',     'OFFLINE', 500, 'LRU', 0, 'admin@summit.example',     '2025-08-01 12:00:00+00', '2025-08-01 12:00:00+00');

-- Agents (connected servers for active gateways)
INSERT INTO storage_agents (id, gateway_id, ip_address, hostname, version, state, cache_used_gb, join_time, last_seen_time) VALUES
    -- Meridian HQ: 2 agents (load balanced)
    ('0192a000-0011-7000-8000-000000110001', '0192a000-0010-7000-8000-000000100001', '127.0.0.1', 'meridian-gw-01', '1.0.0-alpha.1', 'CONNECTED', 123, '2025-06-01 08:05:00+00', '2026-03-22 12:00:00+00'),
    ('0192a000-0011-7000-8000-000000110002', '0192a000-0010-7000-8000-000000100001', '127.0.0.2', 'meridian-gw-02', '1.0.0-alpha.1', 'CONNECTED', 98,  '2025-06-01 08:10:00+00', '2026-03-22 12:00:00+00'),
    -- Pacific Main: 1 agent
    ('0192a000-0011-7000-8000-000000110003', '0192a000-0010-7000-8000-000000100003', '127.0.0.1', 'pacific-gw-01',  '1.0.0-alpha.1', 'CONNECTED', 456, '2025-06-20 09:05:00+00', '2026-03-22 12:00:00+00'),
    -- Heartland DC: 1 agent (draining for maintenance)
    ('0192a000-0011-7000-8000-000000110004', '0192a000-0010-7000-8000-000000100004', '127.0.0.1', 'heartland-gw-01','1.0.0-alpha.1', 'DRAINING',  200, '2025-07-01 08:05:00+00', '2026-03-22 11:55:00+00');

-- Endpoints (S3-compatible backends, all pointing to local rustfs for dev)
-- Configuration stored as JSONB with S3Configuration shape
INSERT INTO storage_endpoints (id, gateway_id, name, display_name, configuration, annotations, created_by, create_time, update_time) VALUES
    -- Meridian HQ: 2 endpoints (primary + archive)
    ('0192a000-0012-7000-8000-000000120001', '0192a000-0010-7000-8000-000000100001', 'meridian-hq-west',  'West Coast Storage',  '{"type":"s3","endpoint_uri":"http://localhost:9000","bucket":"meridian-hq-west","region":"","access_key":{"access_key_id":"rustfsadmin","secret_access_key":"rustfsadmin"}}', '{}', 'admin@meridian.example', '2025-06-01 09:00:00+00', '2025-06-01 09:00:00+00'),
    ('0192a000-0012-7000-8000-000000120002', '0192a000-0010-7000-8000-000000100001', 'meridian-hq-east',  'East Coast Storage',  '{"type":"s3","endpoint_uri":"http://localhost:9000","bucket":"meridian-hq-east","region":"","access_key":{"access_key_id":"rustfsadmin","secret_access_key":"rustfsadmin"}}', '{}', 'admin@meridian.example', '2025-06-01 09:30:00+00', '2025-06-01 09:30:00+00'),
    -- Pacific Main: 1 endpoint
    ('0192a000-0012-7000-8000-000000120003', '0192a000-0010-7000-8000-000000100003', 'pacific-main-primary',  'Primary Storage',  '{"type":"s3","endpoint_uri":"http://localhost:9000","bucket":"pacific-main-primary","region":"","access_key":{"access_key_id":"rustfsadmin","secret_access_key":"rustfsadmin"}}', '{}', 'admin@pacific.example',  '2025-06-20 10:00:00+00', '2025-06-20 10:00:00+00'),
    -- Heartland DC: 1 endpoint
    ('0192a000-0012-7000-8000-000000120004', '0192a000-0010-7000-8000-000000100004', 'heartland-dc-primary',  'Primary Storage',  '{"type":"s3","endpoint_uri":"http://localhost:9000","bucket":"heartland-dc-primary","region":"","access_key":{"access_key_id":"rustfsadmin","secret_access_key":"rustfsadmin"}}', '{}', 'admin@heartland.example', '2025-07-01 09:00:00+00', '2025-07-01 09:00:00+00');
