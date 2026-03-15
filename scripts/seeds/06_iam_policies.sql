-- IAM policies for a few resources
INSERT INTO iam_policies (resource_id, resource_type, policy, update_time) VALUES
    -- Meridian org
    ('0192a000-0001-7000-8000-000000000001', 'organizations', '{"bindings":[{"role":"roles/owner","members":["user:admin@meridian.example","user:cto@meridian.example"]},{"role":"roles/viewer","members":["group:engineering@meridian.example"]}]}', '2025-06-01 10:00:00+00'),
    -- Pacific org
    ('0192a000-0001-7000-8000-000000000002', 'organizations', '{"bindings":[{"role":"roles/owner","members":["user:admin@pacific.example"]},{"role":"roles/editor","members":["user:producer@pacific.example","user:director@pacific.example"]}]}', '2025-07-15 11:00:00+00'),
    -- Meridian corp site project
    ('0192a000-0003-7000-8000-000000310001', 'projects', '{"bindings":[{"role":"roles/owner","members":["user:webmaster@meridian.example"]},{"role":"roles/editor","members":["serviceAccount:deploy@meridian.iam.example"]},{"role":"roles/viewer","members":["group:all@meridian.example"]}]}', '2025-09-01 14:00:00+00'),
    -- Summit org
    ('0192a000-0001-7000-8000-000000000005', 'organizations', '{"bindings":[{"role":"roles/owner","members":["user:ceo@summit.example"]},{"role":"roles/editor","members":["user:cto@summit.example","user:ops@summit.example"]}]}', '2025-10-05 10:00:00+00'),
    -- Starlight org
    ('0192a000-0001-7000-8000-00000000000a', 'organizations', '{"bindings":[{"role":"roles/owner","members":["user:founder@starlight.example"]},{"role":"roles/viewer","members":["group:artists@starlight.example"]}]}', '2026-02-15 13:00:00+00');
