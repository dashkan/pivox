-- Invitation policies (one per org that has configured one)
INSERT INTO invitation_policies (org_id, disable_public_email_addresses, allowed_domains, disallowed_domains, update_time) VALUES
    -- Meridian: restrict to corporate domains only
    ('0192a000-0001-7000-8000-000000000001', true, '{"meridian.tv","meridian.example"}', '{}', '2025-04-01 10:00:00+00'),
    -- Pacific Coast: allow specific domains, block public email
    ('0192a000-0001-7000-8000-000000000002', true, '{"pacificcoast.net","pacific.example"}', '{}', '2025-05-15 09:00:00+00'),
    -- Atlantic News: open invitations but block a competitor domain
    ('0192a000-0001-7000-8000-000000000004', false, '{}', '{"competitor-news.example"}', '2025-06-01 08:00:00+00'),
    -- Starlight: corporate domains only
    ('0192a000-0001-7000-8000-00000000000a', true, '{"starlight.io","starlight.example"}', '{}', '2026-01-10 14:00:00+00');
