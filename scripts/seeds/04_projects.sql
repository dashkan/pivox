-- Projects (1-2 per org, all org-parented)
INSERT INTO projects (id, org_id, name, display_name, labels, create_time, update_time) VALUES
    -- Meridian Broadcasting
    ('0192a000-0003-7000-8000-000000310001', '0192a000-0001-7000-8000-000000000001', 'corp-site',     'Corporate Website',    '{"env":"production","team":"web"}',    '2025-03-15 08:00:00+00', '2025-07-01 10:00:00+00'),
    ('0192a000-0003-7000-8000-000000310002', '0192a000-0001-7000-8000-000000000001', 'internal-tools', 'Internal Tools',       '{"env":"production","team":"ops"}',    '2025-03-18 10:00:00+00', '2025-08-15 12:00:00+00'),
    -- Pacific Coast Networks
    ('0192a000-0003-7000-8000-000000320001', '0192a000-0001-7000-8000-000000000002', 'pacific-hub',   'Pacific Hub Platform', '{"env":"production","region":"west"}', '2025-04-10 07:00:00+00', '2025-08-01 09:00:00+00'),
    -- Heartland Media
    ('0192a000-0003-7000-8000-000000330001', '0192a000-0001-7000-8000-000000000003', 'portal',        'Heartland Portal',    '{"env":"production"}',                 '2025-04-25 07:30:00+00', '2025-09-01 08:00:00+00'),
    -- Atlantic News Network
    ('0192a000-0003-7000-8000-000000340001', '0192a000-0001-7000-8000-000000000004', 'wire-service',  'Wire Service',        '{"env":"production"}',                 '2025-05-15 06:30:00+00', '2025-10-20 08:00:00+00'),
    -- Summit Sports
    ('0192a000-0003-7000-8000-000000350001', '0192a000-0001-7000-8000-000000000005', 'main-site',     'Summit Main Site',    '{"env":"production"}',                 '2025-06-01 08:00:00+00', '2025-11-01 10:00:00+00'),
    -- Crescent Entertainment
    ('0192a000-0003-7000-8000-000000360001', '0192a000-0001-7000-8000-000000000006', 'crescent-main', 'Crescent Main',       '{"env":"production"}',                 '2025-06-25 10:00:00+00', '2026-01-10 12:00:00+00'),
    -- Pinnacle Digital
    ('0192a000-0003-7000-8000-000000370001', '0192a000-0001-7000-8000-000000000007', 'platform',      'Pinnacle Platform',   '{"env":"production"}',                 '2025-07-20 08:00:00+00', '2026-01-20 10:00:00+00'),
    -- Ironbridge Productions
    ('0192a000-0003-7000-8000-000000380001', '0192a000-0001-7000-8000-000000000008', 'portal',        'Ironbridge Portal',   '{"env":"production"}',                 '2025-08-15 08:00:00+00', '2026-01-30 10:00:00+00'),
    -- Lakeshore Public
    ('0192a000-0003-7000-8000-000000390001', '0192a000-0001-7000-8000-000000000009', 'public-site',   'Public Site',         '{"env":"production"}',                 '2025-09-10 08:00:00+00', '2026-02-01 09:00:00+00'),
    -- Starlight Studios
    ('0192a000-0003-7000-8000-0000003a0001', '0192a000-0001-7000-8000-00000000000a', 'showcase',      'Starlight Showcase',   '{"env":"production"}',                 '2025-10-05 08:00:00+00', '2026-02-20 10:00:00+00');

-- Soft-delete one project for filter testing
UPDATE projects SET state = 'DELETE_REQUESTED', delete_time = '2026-02-01 15:00:00+00', deleted_by = 'admin@meridian.example' WHERE id = '0192a000-0003-7000-8000-000000310002';
