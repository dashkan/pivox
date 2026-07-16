-- Sample workflow for the Local Corp dev org.
--
-- Gives a freshly-wiped dev DB something to render on the read-only
-- workflow canvas (/workflows is otherwise empty on first boot). One
-- connector, one workflow, one promoted version whose definition
-- exercises every node type the canvas knows how to draw.
--
-- Attaches to `local-corp` (0192a000-…-00000000000b), the simple dev
-- org seeded in 11_local_corp.sql. Org-scoped (space_id NULL).
--
-- Shapes are NOT arbitrary — they must match what the Go read path
-- (internal/convert) unmarshals:
--   - connectors.config       protojson of Connector{config: http}  → {"http": {...}}
--                             (convert.ConnectorToProto)
--   - workflow_versions.definition
--                             protojson of a scratch WorkflowVersion carrying
--                             parameters + trigger + root (+ errorSequence)
--                             (convert.WorkflowVersionToProto / service marshalDefinition)
-- Both blobs were generated from the proto types via protojson, so field
-- names are camelCase and enums are their string form.
--
-- NOTE: the `errorSequence` read/write round-trip is wired.
-- internal/convert.WorkflowVersionToProto lifts errorSequence off the
-- definition (workflows.go:94), and the service write path
-- (marshalDefinition) persists it. The errorSequence key below therefore
-- decodes and round-trips through the production read/write path today. The
-- runtime executor honors it too (the Worker Process's RunWorkflowWorker feeds
-- it to engine.Interpreter.Run).

-- CEL note: values inside the definition are CEL expressions. A string
-- literal is a quoted string INSIDE the CEL, hence the doubled quotes
-- (e.g. "\"us-east\"" is the CEL string literal "us-east"); bare
-- identifiers like vars.region / 1 > 0 are CEL references/operators.

-- 1) Connector: an http connector with a base URL.
INSERT INTO connectors (id, org_id, space_id, slug, display_name, description, config, create_time, update_time) VALUES
    ('0192a000-0060-7000-8000-00000000b001',
     '0192a000-0001-7000-8000-00000000000b',
     NULL,
     'sample-api',
     'Sample Inventory API',
     'Demo http connector backing the nightly-ingest sample workflow.',
     '{"http":{"baseUrl":"https://api.example.com/v1","headers":{"Accept":"\"application/json\""}}}',
     '2026-01-05 08:00:00+00', '2026-01-05 08:00:00+00');

-- 2) Workflow container (version pointer set after the version exists —
--    workflows.version → workflow_versions(id) is a non-deferred circular FK,
--    so the row must be workflow-first).
INSERT INTO workflows (id, org_id, space_id, slug, display_name, description, enabled, origin, create_time, update_time) VALUES
    ('0192a000-0061-7000-8000-00000000b001',
     '0192a000-0001-7000-8000-00000000000b',
     NULL,
     'nightly-ingest',
     'Nightly Inventory Ingest',
     'Sample workflow demonstrating every node type: http, set, condition, parallel, try/catch, run_workflow, and end.',
     true,
     'OWNED',
     '2026-01-05 08:05:00+00', '2026-01-05 08:05:00+00');

-- 3) Promoted version. definition exercises: set, http (via the connector),
--    condition (branch + otherwise), parallel (2 lanes), try (body + catch
--    with a fail), run_workflow (self-reference), end, plus an errorSequence.
--    Step ids are unique across root + errorSequence.
INSERT INTO workflow_versions (id, workflow_id, version_number, note, definition, create_time) VALUES
    ('0192a000-0062-7000-8000-00000000b001',
     '0192a000-0061-7000-8000-00000000b001',
     1,
     'Initial sample definition.',
     '{"parameters":[{"key":"region","type":"PARAM_STRING","description":"Target inventory region.","defaultValue":"us-east"},{"key":"dry_run","type":"PARAM_BOOL","description":"When true, skip destructive writes.","defaultValue":true}],"trigger":{"schedule":{}},"root":{"steps":[{"id":"init","activity":{"set":{"assignments":{"region":"\"us-east\""}}}},{"id":"fetch","activity":{"http":{"connector":"organizations/local-corp/connectors/sample-api","method":"GET","path":"\"/inventory\"","query":{"region":"vars.region"},"headers":{"X-Request-Source":"\"pivox\""},"retry":{"maxAttempts":3,"initialBackoff":"0.100s","maxBackoff":"5s","backoffMultiplier":2},"successStatus":[404],"retryableStatus":[429]}}},{"id":"route","condition":{"branches":[{"when":"1 > 0","then":{"steps":[{"id":"route_live","activity":{"set":{"assignments":{"mode":"\"live\""}}}}]}}],"otherwise":{"steps":[{"id":"route_idle","activity":{"set":{"assignments":{"mode":"\"idle\""}}}}]}}},{"id":"fanout","parallel":{"branches":[{"steps":[{"id":"fan_left","activity":{"set":{"assignments":{"left":"\"a\""}}}}]},{"steps":[{"id":"fan_right","activity":{"set":{"assignments":{"right":"\"b\""}}}}]}]}},{"id":"guard","try":{"body":{"steps":[{"id":"guard_probe","activity":{"set":{"assignments":{"probe":"vars.region"}}}}]},"catch":{"steps":[{"id":"guard_fail","activity":{"fail":{"message":"inventory probe failed"}}}]}}},{"id":"compose","activity":{"runWorkflow":{"workflow":"organizations/local-corp/workflows/nightly-ingest","parameters":{"region":"vars.region"}}}},{"id":"done","activity":{"end":{}}}]},"errorSequence":{"steps":[{"id":"err_note","activity":{"set":{"assignments":{"note":"\"workflow failed\""}}}},{"id":"err_notify","activity":{"http":{"connector":"organizations/local-corp/connectors/sample-api","method":"POST","path":"\"/alerts\"","body":"vars.region"}}}]}}',
     '2026-01-05 08:10:00+00');

-- 4) Promote the version (set the live pointer now that the row exists).
UPDATE workflows
   SET version = '0192a000-0062-7000-8000-00000000b001'
 WHERE id = '0192a000-0061-7000-8000-00000000b001';
