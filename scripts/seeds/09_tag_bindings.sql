-- Tag bindings (bind tags to projects)
INSERT INTO tag_bindings (id, parent_resource, tag_value_id, create_time, update_time) VALUES
    -- Meridian corp site -> production
    ('0192a000-0006-7000-8000-000000610001', '//pivox.api/organizations/meridian-broad/projects/corp-site', '0192a000-0005-7000-8000-000000510001', '2025-04-01 08:00:00+00', '2025-04-01 08:00:00+00'),
    -- Meridian corp site -> news cost center
    ('0192a000-0006-7000-8000-000000610002', '//pivox.api/organizations/meridian-broad/projects/corp-site', '0192a000-0005-7000-8000-000000520001', '2025-04-05 10:00:00+00', '2025-04-05 10:00:00+00'),
    -- Pacific hub -> west-coast
    ('0192a000-0006-7000-8000-000000620001', '//pivox.api/organizations/pacific-coast-net/projects/pacific-hub', '0192a000-0005-7000-8000-000000530001', '2025-05-01 09:00:00+00', '2025-05-01 09:00:00+00'),
    -- Summit main site -> live event
    ('0192a000-0006-7000-8000-000000630001', '//pivox.api/organizations/summit-sports/projects/main-site', '0192a000-0005-7000-8000-000000540001', '2025-06-10 08:00:00+00', '2025-06-10 08:00:00+00'),
    -- Starlight showcase -> production stage
    ('0192a000-0006-7000-8000-000000640001', '//pivox.api/organizations/starlight-studios/projects/showcase', '0192a000-0005-7000-8000-000000550002', '2025-10-20 10:00:00+00', '2025-10-20 10:00:00+00');
