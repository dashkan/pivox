-- Meridian Broadcasting assets — Phase 6b smoke fixture.
--
-- 12 deterministic asset rows under Meridian's spaces, covering
-- the icon-mapping variety the renderer dispatches on. The split
-- is intentional:
--
--   * 10 rows in `corp-site` (active space) — Library renders
--     these via Phase 5's QueryDashboardData.
--   * 2 rows in `internal-tools` (soft-deleted space, see
--     04_spaces.sql:23) — verify ListAssetsByOrg's
--     `WHERE spaces.delete_time IS NULL` filter actually fires.
--     DB count says 12; Library renders 10; the delta proves
--     the soft-delete filter is wired right.
--
-- All assets bind to endpoint `meridian-hq-west` (id from
-- 10_storage_gateways.sql). State is `'ACTIVE'` (the proto's
-- "fully ingested and available" state per asset.proto:212);
-- the asset_state enum has no `'READY'` member.
--
-- thumbnail_url is deliberately NOT seeded — Phase 6b ships
-- with `IconConfig.source_field = "thumbnail_url"` empty so the
-- IconConfigResolver chain falls through to `iconField` (the
-- numeric Icon enum value synthesized server-side from
-- media_type/content_type). Phase 6c wires the storage-gateway
-- URL composition; until then, every row renders an SF Symbol
-- via the IconSymbol.swift map shipped in Phase 6a.
--
-- checksum_sha256 left empty — Phase 6b's renderer doesn't read
-- it and the column allows ''. When 6c seeds rustfs object keys,
-- checksums become load-bearing for the gateway URL contract.

INSERT INTO assets (
    id, space_id, endpoint_id, name, display_name, filename,
    media_type, content_type, size_bytes, state,
    create_time, update_time
) VALUES
    -- corp-site (active) — 10 rows covering all 7 iconForAsset
    -- code paths: IMAGE/VIDEO/AUDIO/DOCUMENT/GRAPHIC enum cases,
    -- content-type prefix fallback, final ICON_DOCUMENT fallback.

    -- IMAGE×2 → ICON_PHOTO via media_type enum.
    ('0192a000-0030-7000-8000-310001000001', '0192a000-0003-7000-8000-000000310001', '0192a000-0012-7000-8000-000000120001',
     'logo-final', 'Logo Final', 'Logo Final.png',
     'IMAGE', 'image/png', 245678, 'ACTIVE',
     '2026-04-12 09:00:00+00', '2026-04-12 09:00:00+00'),
    ('0192a000-0030-7000-8000-310001000002', '0192a000-0003-7000-8000-000000310001', '0192a000-0012-7000-8000-000000120001',
     'team-photo', 'Team Photo', 'Team Photo.jpeg',
     'IMAGE', 'image/jpeg', 1842300, 'ACTIVE',
     '2026-04-11 14:30:00+00', '2026-04-11 14:30:00+00'),

    -- VIDEO×2 → ICON_VIDEO via media_type enum.
    ('0192a000-0030-7000-8000-310001000003', '0192a000-0003-7000-8000-000000310001', '0192a000-0012-7000-8000-000000120001',
     'hero-reel', 'Hero Reel', 'Hero Reel.mp4',
     'VIDEO', 'video/mp4', 124500000, 'ACTIVE',
     '2026-04-10 11:15:00+00', '2026-04-10 11:15:00+00'),
    ('0192a000-0030-7000-8000-310001000004', '0192a000-0003-7000-8000-000000310001', '0192a000-0012-7000-8000-000000120001',
     'promo-spot', 'Promo Spot', 'Promo Spot.mov',
     'VIDEO', 'video/quicktime', 89300000, 'ACTIVE',
     '2026-04-09 16:45:00+00', '2026-04-09 16:45:00+00'),

    -- AUDIO×2 → ICON_AUDIO via media_type enum.
    ('0192a000-0030-7000-8000-310001000005', '0192a000-0003-7000-8000-000000310001', '0192a000-0012-7000-8000-000000120001',
     'soundbed', 'Soundbed', 'Soundbed.mp3',
     'AUDIO', 'audio/mpeg', 4120000, 'ACTIVE',
     '2026-04-08 10:00:00+00', '2026-04-08 10:00:00+00'),
    ('0192a000-0030-7000-8000-310001000006', '0192a000-0003-7000-8000-000000310001', '0192a000-0012-7000-8000-000000120001',
     'interview', 'Interview', 'Interview.wav',
     'AUDIO', 'audio/wav', 38400000, 'ACTIVE',
     '2026-04-07 13:20:00+00', '2026-04-07 13:20:00+00'),

    -- DOCUMENT×1 → ICON_DOCUMENT via media_type enum.
    ('0192a000-0030-7000-8000-310001000007', '0192a000-0003-7000-8000-000000310001', '0192a000-0012-7000-8000-000000120001',
     'press-brief', 'Press Brief', 'Press Brief.pdf',
     'DOCUMENT', 'application/pdf', 1200000, 'ACTIVE',
     '2026-04-06 08:30:00+00', '2026-04-06 08:30:00+00'),

    -- GRAPHIC×1 → ICON_PHOTO via media_type enum (GRAPHIC and IMAGE
    -- both map to PHOTO per IconSymbol.swift's case .graphic).
    ('0192a000-0030-7000-8000-310001000008', '0192a000-0003-7000-8000-000000310001', '0192a000-0012-7000-8000-000000120001',
     'logo-vector', 'Logo Vector', 'Logo Vector.svg',
     'GRAPHIC', 'image/svg+xml', 28400, 'ACTIVE',
     '2026-04-05 12:00:00+00', '2026-04-05 12:00:00+00'),

    -- NULL media_type + image/webp → ICON_PHOTO via content_type
    -- prefix fallback (iconForAsset's switch on
    -- contentType.starts(image/)).
    ('0192a000-0030-7000-8000-310001000009', '0192a000-0003-7000-8000-000000310001', '0192a000-0012-7000-8000-000000120001',
     'banner-fallback', 'Banner', 'Banner.webp',
     NULL, 'image/webp', 156800, 'ACTIVE',
     '2026-04-04 09:45:00+00', '2026-04-04 09:45:00+00'),

    -- NULL media_type + "" → ICON_DOCUMENT via final fallback
    -- (every other branch of iconForAsset's switch falls through).
    ('0192a000-0030-7000-8000-31000100000a', '0192a000-0003-7000-8000-000000310001', '0192a000-0012-7000-8000-000000120001',
     'legacy-doc', 'Legacy Document', 'legacy-bundle.bin',
     NULL, '', 89500, 'ACTIVE',
     '2026-04-03 15:00:00+00', '2026-04-03 15:00:00+00'),

    -- internal-tools (soft-deleted in 04_spaces.sql:23) — 2 rows
    -- verify ListAssetsByOrg's WHERE spaces.delete_time IS NULL
    -- filter actually fires. Library renders 10, not 12; the
    -- delta proves the filter is wired right.
    ('0192a000-0030-7000-8000-31000200000b', '0192a000-0003-7000-8000-000000310002', '0192a000-0012-7000-8000-000000120001',
     'cover-image', 'Cover Image', 'Cover Image.heic',
     'IMAGE', 'image/heic', 3200000, 'ACTIVE',
     '2026-04-02 10:30:00+00', '2026-04-02 10:30:00+00'),
    ('0192a000-0030-7000-8000-31000200000c', '0192a000-0003-7000-8000-000000310002', '0192a000-0012-7000-8000-000000120001',
     'notes', 'Engineering Notes', 'Notes.txt',
     'DOCUMENT', 'text/plain', 12400, 'ACTIVE',
     '2026-04-01 16:00:00+00', '2026-04-01 16:00:00+00');
