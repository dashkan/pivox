-- Custom domains (3 orgs have custom domains)
INSERT INTO custom_domains (id, org_id, domain, state, dns_records, create_time, update_time, verify_time) VALUES
    -- Meridian: active custom domain
    ('0192a000-000b-7000-8000-000000b10001', '0192a000-0001-7000-8000-000000000001', 'pivox.meridian.tv',
     'ACTIVE',
     '[{"type":"CNAME","name":"pivox","value":"custom.pivox.dev."},{"type":"TXT","name":"_pivox-verify","value":"pivox-verify=meridian-abc123"}]',
     '2025-04-01 08:00:00+00', '2025-05-01 10:00:00+00', '2025-04-15 12:00:00+00'),
    -- Pacific Coast: active custom domain
    ('0192a000-000b-7000-8000-000000b20001', '0192a000-0001-7000-8000-000000000002', 'dashboard.pacificcoast.net',
     'ACTIVE',
     '[{"type":"CNAME","name":"dashboard","value":"custom.pivox.dev."},{"type":"TXT","name":"_pivox-verify","value":"pivox-verify=pacific-def456"}]',
     '2025-05-10 09:00:00+00', '2025-06-15 11:00:00+00', '2025-05-25 14:00:00+00'),
    -- Starlight: pending verification
    ('0192a000-000b-7000-8000-000000b30001', '0192a000-0001-7000-8000-00000000000a', 'studio.starlight.io',
     'PENDING',
     '[{"type":"CNAME","name":"studio","value":"custom.pivox.dev."},{"type":"TXT","name":"_pivox-verify","value":"pivox-verify=starlight-ghi789"}]',
     '2026-02-20 15:00:00+00', '2026-02-20 15:00:00+00', NULL);
