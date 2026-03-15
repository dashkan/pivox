-- API keys (org_id references the organization)
INSERT INTO api_keys (id, org_id, key_id, display_name, key_string, create_time, update_time) VALUES
    -- Meridian
    ('0192a000-0008-7000-8000-000000810001', '0192a000-0001-7000-8000-000000000001', 'web-frontend', 'Web Frontend Key',  'pivox_key_meridian_web_001',     '2025-04-01 09:00:00+00', '2025-09-01 11:00:00+00'),
    ('0192a000-0008-7000-8000-000000810002', '0192a000-0001-7000-8000-000000000001', 'mobile-ios',   'iOS App Key',       'pivox_key_meridian_ios_002',     '2025-04-05 10:00:00+00', '2025-10-01 12:00:00+00'),
    -- Pacific Coast
    ('0192a000-0008-7000-8000-000000820001', '0192a000-0001-7000-8000-000000000002', 'api-gateway',  'API Gateway Key',   'pivox_key_pacific_gw_001',      '2025-05-01 08:00:00+00', '2025-11-01 10:00:00+00'),
    -- Summit Sports
    ('0192a000-0008-7000-8000-000000830001', '0192a000-0001-7000-8000-000000000005', 'public-api',   'Public API Key',    'pivox_key_summit_pub_001',      '2025-06-05 09:00:00+00', '2025-12-01 11:00:00+00'),
    -- Starlight Studios
    ('0192a000-0008-7000-8000-000000840001', '0192a000-0001-7000-8000-00000000000a', 'render-api',   'Render API Key',    'pivox_key_starlight_render_001','2025-10-10 10:00:00+00', '2026-02-20 12:00:00+00');
