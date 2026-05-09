-- Asset versions — Phase 6c smoke fixture.
--
-- One v1 row per asset in 13_assets.sql. Establishes the
-- "every active asset has at least one version" baseline the Phase
-- 6c synthesizer relies on (LEFT JOIN LATERAL to the latest
-- asset_versions row per asset; without these rows, version_number
-- would come back NULL and the URL composer would fall through to
-- the iconField path even for IMAGE rows).
--
-- storage_key follows the locked Phase 6c layout, per
-- docs/assets.md:430-435 + the {org-slug}/{space-slug}/ prefix
-- requirement from #83:
--
--   {org-slug}/{space-slug}/assets/{asset.id}/v1/original.{ext}
--
-- The extension is derived from the asset's content_type at seed-write
-- time (image/png → png, video/quicktime → mov, etc.). When ingestion
-- + the rendition pipeline ship, a single InitiateUpload handler will
-- compose the same shape from the asset row's content_type — these
-- seed values are deterministic stand-ins for that flow.
--
-- mime_type mirrors the asset's content_type to keep the two columns
-- in sync (the asset-versions row's mime_type is the authoritative
-- value for the URL composer; the seed-shape test
-- TestSeed_AssetVersions_MatchAssetContentType verifies the parity
-- so a future edit to one file without the other surfaces as a test
-- failure rather than a silent dev-Library breakage).
--
-- created_by intentionally NULL — same reason as 13_assets.sql.

INSERT INTO asset_versions (
    asset_id, version_number, mime_type, storage_key, size_bytes, create_time
) VALUES
    -- corp-site (active) — 10 v1 rows.

    -- IMAGE×2.
    ('0192a000-0030-7000-8000-310001000001', 1, 'image/png',  'meridian-broad/corp-site/assets/0192a000-0030-7000-8000-310001000001/v1/original.png', 245678,  '2026-04-12 09:00:00+00'),
    ('0192a000-0030-7000-8000-310001000002', 1, 'image/jpeg', 'meridian-broad/corp-site/assets/0192a000-0030-7000-8000-310001000002/v1/original.jpg', 1842300, '2026-04-11 14:30:00+00'),

    -- VIDEO×2.
    ('0192a000-0030-7000-8000-310001000003', 1, 'video/mp4',       'meridian-broad/corp-site/assets/0192a000-0030-7000-8000-310001000003/v1/original.mp4', 124500000, '2026-04-10 11:15:00+00'),
    ('0192a000-0030-7000-8000-310001000004', 1, 'video/quicktime', 'meridian-broad/corp-site/assets/0192a000-0030-7000-8000-310001000004/v1/original.mov', 89300000,  '2026-04-09 16:45:00+00'),

    -- AUDIO×2.
    ('0192a000-0030-7000-8000-310001000005', 1, 'audio/mpeg', 'meridian-broad/corp-site/assets/0192a000-0030-7000-8000-310001000005/v1/original.mp3', 4120000,  '2026-04-08 10:00:00+00'),
    ('0192a000-0030-7000-8000-310001000006', 1, 'audio/wav',  'meridian-broad/corp-site/assets/0192a000-0030-7000-8000-310001000006/v1/original.wav', 38400000, '2026-04-07 13:20:00+00'),

    -- DOCUMENT×1.
    ('0192a000-0030-7000-8000-310001000007', 1, 'application/pdf', 'meridian-broad/corp-site/assets/0192a000-0030-7000-8000-310001000007/v1/original.pdf', 1200000, '2026-04-06 08:30:00+00'),

    -- GRAPHIC×1 (svg).
    ('0192a000-0030-7000-8000-310001000008', 1, 'image/svg+xml', 'meridian-broad/corp-site/assets/0192a000-0030-7000-8000-310001000008/v1/original.svg', 28400, '2026-04-05 12:00:00+00'),

    -- NULL media_type + image/webp (content-type prefix fallback path).
    ('0192a000-0030-7000-8000-310001000009', 1, 'image/webp', 'meridian-broad/corp-site/assets/0192a000-0030-7000-8000-310001000009/v1/original.webp', 156800, '2026-04-04 09:45:00+00'),

    -- NULL media_type + empty content_type (terminal fallback). Extension
    -- comes from the original filename ("legacy-bundle.bin") since
    -- content-type-derived extension is empty.
    ('0192a000-0030-7000-8000-31000100000a', 1, '', 'meridian-broad/corp-site/assets/0192a000-0030-7000-8000-31000100000a/v1/original.bin', 89500, '2026-04-03 15:00:00+00'),

    -- internal-tools (soft-deleted in 04_spaces.sql:26) — 2 v1 rows.
    -- These never reach the synthesizer because ListAssetsByOrg's
    -- `WHERE spaces.delete_time IS NULL` filter strips them; the v1
    -- rows exist purely to keep the "every asset has a version"
    -- invariant honest across the whole 13_assets.sql set.
    ('0192a000-0030-7000-8000-31000200000b', 1, 'image/heic', 'meridian-broad/internal-tools/assets/0192a000-0030-7000-8000-31000200000b/v1/original.heic', 3200000, '2026-04-02 10:30:00+00'),
    ('0192a000-0030-7000-8000-31000200000c', 1, 'text/plain', 'meridian-broad/internal-tools/assets/0192a000-0030-7000-8000-31000200000c/v1/original.txt',   12400,   '2026-04-01 16:00:00+00');
