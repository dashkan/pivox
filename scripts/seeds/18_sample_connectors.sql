-- Sample connectors for local-corp.
--
-- Seeds a varied set of HTTP connectors so the connectors list UI has
-- real data to exercise: name filtering, sort (name + updated), the
-- org/space scope filter, and the org-level rollup. 22 org-direct
-- connectors plus 6 space-scoped ones (3 in `news`, 3 in `sports`) —
-- the two spaces seeded for local-corp in 11_local_corp.sql.
--
-- Needs local-corp (11_local_corp.sql) for the org + its spaces. The
-- org is resolved by slug at runtime; if local-corp is absent the
-- CROSS JOIN yields no rows and the insert is a silent no-op (matches
-- the resilience of the other dev seeds).
--
-- Timestamps are FIXED (not now()-relative) so a reseed is
-- deterministic: create_time fans out by day and update_time by a
-- minute offset, giving stable variety for the two sort columns.
--
-- Idempotent via ON CONFLICT (org_id, space_id, slug) DO NOTHING —
-- the connectors uniqueness constraint (NULLS NOT DISTINCT) treats the
-- org-direct rows (space_id NULL) as a single scope, so re-running is a
-- no-op rather than a constraint error.
--
-- Runs inside the outer transaction from scripts/seed.sql — no inner
-- BEGIN/COMMIT.

INSERT INTO connectors (org_id, space_id, slug, display_name, description, config, create_time, update_time)
SELECT
    org.id,
    sp.id,  -- NULL for org-direct rows (space_name NULL → no LEFT JOIN match)
    d.slug,
    d.name,
    d.name || ' integration',
    ('{"http":{"baseUrl":"https://api.' || d.slug || '.example.com","headers":{}}}')::jsonb,
    TIMESTAMPTZ '2026-01-05 08:00:00+00' + (d.ord || ' days')::interval,
    TIMESTAMPTZ '2026-06-15 09:00:00+00' - (d.ord * 41 || ' minutes')::interval
FROM (VALUES
    -- Org-direct connectors.
    ( 1, NULL::text, 'Stripe Payments',     'stripe-payments'),
    ( 2, NULL,       'GitHub Webhooks',      'github-webhooks'),
    ( 3, NULL,       'Slack Notifications',  'slack-notifications'),
    ( 4, NULL,       'Twilio SMS',           'twilio-sms'),
    ( 5, NULL,       'SendGrid Email',       'sendgrid-email'),
    ( 6, NULL,       'AWS S3',               'aws-s3'),
    ( 7, NULL,       'Datadog Metrics',      'datadog-metrics'),
    ( 8, NULL,       'PagerDuty Alerts',     'pagerduty-alerts'),
    ( 9, NULL,       'Salesforce CRM',       'salesforce-crm'),
    (10, NULL,       'HubSpot Marketing',    'hubspot-marketing'),
    (11, NULL,       'Zendesk Support',      'zendesk-support'),
    (12, NULL,       'Jira Issues',          'jira-issues'),
    (13, NULL,       'Notion Sync',          'notion-sync'),
    (14, NULL,       'Airtable Base',        'airtable-base'),
    (15, NULL,       'Shopify Orders',       'shopify-orders'),
    (16, NULL,       'Segment Events',       'segment-events'),
    (17, NULL,       'Mixpanel Analytics',   'mixpanel-analytics'),
    (18, NULL,       'Cloudflare DNS',       'cloudflare-dns'),
    (19, NULL,       'Okta SSO',             'okta-sso'),
    (20, NULL,       'Vault Secrets',        'vault-secrets'),
    (21, NULL,       'Elastic Search',       'elastic-search'),
    (22, NULL,       'Snowflake Warehouse',  'snowflake-warehouse'),
    -- Space-scoped connectors (news).
    (23, 'news',     'Newsroom CMS',         'newsroom-cms'),
    (24, 'news',     'AP Wire',              'ap-wire'),
    (25, 'news',     'Reuters Feed',         'reuters-feed'),
    -- Space-scoped connectors (sports).
    (26, 'sports',   'Live Scores',          'live-scores'),
    (27, 'sports',   'Stats Provider',       'stats-provider'),
    (28, 'sports',   'Ticketing',            'ticketing')
) AS d(ord, space_name, name, slug)
CROSS JOIN (SELECT id FROM organizations WHERE name = 'local-corp') org
LEFT JOIN spaces sp ON sp.org_id = org.id AND sp.name = d.space_name
ON CONFLICT (org_id, space_id, slug) DO NOTHING;
