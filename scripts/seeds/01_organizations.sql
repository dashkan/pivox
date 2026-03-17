-- 10 broadcast companies
INSERT INTO organizations (id, name, display_name, create_time, update_time) VALUES
    ('0192a000-0001-7000-8000-000000000001', 'meridian-broad',         'Meridian Broadcasting Group',   '2025-03-10 08:00:00+00', '2025-06-15 14:30:00+00'),
    ('0192a000-0001-7000-8000-000000000002', 'pacific-coast-net',      'Pacific Coast Networks',        '2025-03-22 11:15:00+00', '2025-07-01 09:00:00+00'),
    ('0192a000-0001-7000-8000-000000000003', 'heartland-media',        'Heartland Media Corporation',   '2025-04-05 15:45:00+00', '2025-08-20 16:00:00+00'),
    ('0192a000-0001-7000-8000-000000000004', 'atlantic-news-net',      'Atlantic News Network',         '2025-04-18 09:30:00+00', '2025-09-10 10:15:00+00'),
    ('0192a000-0001-7000-8000-000000000005', 'summit-sports',          'Summit Sports Media',           '2025-05-02 13:00:00+00', '2025-10-05 11:45:00+00'),
    ('0192a000-0001-7000-8000-000000000006', 'crescent-ent',           'Crescent Entertainment Group',  '2025-05-20 10:00:00+00', '2025-11-01 08:30:00+00'),
    ('0192a000-0001-7000-8000-000000000007', 'pinnacle-digital',       'Pinnacle Digital Broadcasting', '2025-06-08 07:30:00+00', '2025-12-15 13:00:00+00'),
    ('0192a000-0001-7000-8000-000000000008', 'ironbridge-prod',        'Ironbridge Productions',        '2025-07-14 16:00:00+00', '2026-01-10 15:30:00+00'),
    ('0192a000-0001-7000-8000-000000000009', 'lakeshore-public',       'Lakeshore Public Media',        '2025-08-25 12:00:00+00', '2026-01-28 09:45:00+00'),
    ('0192a000-0001-7000-8000-00000000000a', 'starlight-studios',      'Starlight Studios International','2025-09-30 14:30:00+00', '2026-02-20 17:00:00+00');

-- One soft-deleted org for filter testing
UPDATE organizations SET state = 'DELETE_REQUESTED', delete_time = '2026-02-01 10:00:00+00', deleted_by = 'admin@lakeshore.example' WHERE name = 'lakeshore-public';
