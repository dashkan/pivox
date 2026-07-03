# Audit-field Consistency Snapshot — 2026-04-30

**Status**: report only. Drives [#4 — audit-fields sweep to UUID FK](https://github.com/dashkan/pivox/issues/4).
**Generated against**: dev DB (`postgresql://localhost:5432/pivox`).
**Identity rows**: 2 (`ashkan.daie@gmail.com`, `ashkan@acme.com`).

> **Update (2026): Firebase has since been removed — auth is
> Keycloak-only.** This is a dated snapshot; the `firebase_identities`
> table and `*_firebase_identity_id` columns referenced below have since
> been renamed/reshaped (the table is now `identities`, keyed on the
> Keycloak `sub` UUID). Read this as a historical record of the DB state
> on 2026-04-30, not the current schema. See `AGENTS.md` for the current
> auth model.

## TL;DR

The dev DB is in clean shape for the migration. **No firebase_uid strings, no
mixed UUID/legacy formats, no orphaned UUID references.** The only anomaly is
the seed scripts writing email-shaped strings (`admin@<orgname>.example`) into
some `*_by` columns; these are not real users and need to be cleared by
updating the seeds + drop+reseed.

## What got inspected

50 audit columns across 26 tables, plus the 5 already-UUID user-ref columns.
Each value classified into:

- `blank` — empty string (default)
- `uuid_ok` — valid UUID that resolves in `firebase_identities.id`
- `uuid_orf` — valid UUID that does NOT resolve (orphan)
- `fbuid_ok` — non-UUID, non-email string that matches `firebase_identities.firebase_uid`
- `fbuid_orf` — non-UUID, non-email string that does NOT match any `firebase_uid`
- `email` — value contains `@` (definitely not a uid or uuid)

## Findings

### TEXT audit columns (the migration target)

Per-column counts (only non-empty rows shown):

| column | rows | blank | uuid_ok | email | other |
|---|---:|---:|---:|---:|---:|
| `org_members.created_by` | 1 | 0 | 1 | 0 | 0 |
| `organizations.deleted_by` | 12 | 11 | 0 | 1 | 0 |
| `spaces.deleted_by` | 13 | 12 | 0 | 1 | 0 |
| `storage_endpoints.created_by` | 5 | 0 | 0 | 5 | 0 |
| `storage_gateways.created_by` | 6 | 0 | 0 | 6 | 0 |
| `tag_keys.created_by` | 7 | 5 | 0 | 2 | 0 |
| `tag_values.created_by` | 15 | 12 | 0 | 3 | 0 |
| *(all other 43 columns)* | varies | 100% blank | 0 | 0 | 0 |

**Email-shaped values** (17 rows total, all from demo seeds):

```
organizations.deleted_by         → admin@lakeshore.example
spaces.deleted_by                → admin@meridian.example
storage_endpoints.created_by     → admin@{heartland,local,meridian,...}.example
storage_gateways.created_by      → admin@{heartland,local,meridian,...}.example
tag_keys.created_by              → admin@local.example
tag_values.created_by            → admin@local.example
```

These were written by `scripts/seeds/01_organizations.sql`,
`scripts/seeds/04_spaces.sql`, `scripts/seeds/10_storage_gateways.sql`,
`scripts/seeds/11_local_corp.sql`, `07_tag_keys.sql`, `08_tag_values.sql`.
They were always synthetic and never matched any real Firebase identity —
the seed authors used a placeholder string that happens to look like an
email address.

**No firebase_uid-shaped strings detected anywhere.** All non-blank values
are either real UUIDs (1 row in `org_members.created_by`) or seed-placeholder
emails (17 rows). This is much cleaner than expected for a Phase 1–7
codebase; previous DB resets have flushed any historical mess.

### UUID user-ref columns (already correct shape)

| column | rows | non-null | resolves | orphans |
|---|---:|---:|---:|---:|
| `ai_conversations.creator_id` | 0 | 0 | 0 | 0 |
| `group_members.user_id` | 0 | 0 | 0 | 0 |
| `org_members.principal_id` | 1 | 1 | 1 | 0 |
| `space_members.principal_id` | 0 | 0 | 0 | 0 |
| `organizations.created_by_firebase_identity_id` | 12 | 1 | 1 | 0 |

All non-null values resolve. Zero orphans. Good.

### Cross-column consistency

`ai_conversations` (0 rows): no disagreement possible.

`organizations` (12 rows):
- 11 rows: both `created_by` (TEXT) and `created_by_firebase_identity_id` (UUID) blank/null — seed orgs created without a founder
- 1 row: UUID set, TEXT blank — the `acme` org from `dev_acme_sso.sql`'s pass-2 seed
- 0 rows: TEXT set without UUID, or values that disagree

So nothing has dual-written then drifted. The historical concern ("does the
TEXT column point at a different person than the UUID column?") doesn't
apply to current data.

## Migration implications

1. **Schema migration is safe to run as planned** (issue #4):
   - Drop `created_by_firebase_identity_id` from organizations; keep its 1
     real value as the new `created_by` UUID.
   - Drop `creator_id` from ai_conversations (zero rows; nothing to preserve).
   - Convert all TEXT `*_by` columns to `UUID NULL REFERENCES firebase_identities(id)`.
2. **Seeds need updating** before the drop+reseed (or as part of the same
   commit):
   - `01_organizations.sql`, `04_spaces.sql`, `07_tag_keys.sql`,
     `08_tag_values.sql`, `10_storage_gateways.sql`, `11_local_corp.sql`
     stop writing the `admin@…example` placeholder. Either omit `created_by`
     entirely (column default is now NULL) or write a real
     `firebase_identities.id` if a system / bot identity is meaningful for
     that resource.
3. **The 1 row with `org_members.created_by` as a valid UUID** survives the
   migration unchanged — it's already the right shape, just the column type
   changes from TEXT to UUID.
4. **No backfill query needed** for the migration. Pre-prod scope means we
   drop+recreate the DB anyway, but if a backup is around: `UPDATE … SET
   <col> = NULL WHERE <col> ~ '@'` would clear the seed placeholders without
   touching anything else.

## Anomalies that don't need action

- `tag_keys.created_by` has rows with `admin@local.example` shape — flagged
  above; same seed-cleanup fix applies.
- `org_members.updated_by` has 1 row, blank — that's correct (the binding
  was created, not updated).
- All `ai_*` tables (chat conversations / messages / artifacts) are empty,
  which means we don't get to validate the Phase 7 path-vs-row enforcement
  empirically here. Functional tests cover that surface; this audit is just
  about static column state.

## Recommended next steps

1. Land seed cleanup in the same commit as the schema migration. Single
   diff, no in-between state where the seed and schema disagree.
2. After migration: re-run this same classification script as a
   regression check. Target: 0 emails, 0 orphans, 0 fbuid strings, every
   non-blank value resolves.
3. Wire a CI check (issue #4 acceptance) that fails if any `*_by` column
   is `TEXT` rather than `UUID FK`.

## Reproducibility

The shell loop that produced the per-column classification is in the
session's bash history; the core query template is:

```sql
SELECT count(*),
       count(*) FILTER (WHERE "<col>" = ''),
       count(*) FILTER (WHERE "<col>" ~ '^[0-9a-f]{8}-[0-9a-f]{4}-' AND EXISTS(SELECT 1 FROM firebase_identities fi WHERE fi.id::text = "<col>")),
       count(*) FILTER (WHERE "<col>" ~ '^[0-9a-f]{8}-[0-9a-f]{4}-' AND NOT EXISTS(SELECT 1 FROM firebase_identities fi WHERE fi.id::text = "<col>")),
       count(*) FILTER (WHERE "<col>" !~ '^[0-9a-f]{8}-' AND "<col>" != '' AND "<col>" !~ '@' AND EXISTS(SELECT 1 FROM firebase_identities fi WHERE fi.firebase_uid = "<col>")),
       count(*) FILTER (WHERE "<col>" !~ '^[0-9a-f]{8}-' AND "<col>" != '' AND "<col>" !~ '@' AND NOT EXISTS(SELECT 1 FROM firebase_identities fi WHERE fi.firebase_uid = "<col>")),
       count(*) FILTER (WHERE "<col>" ~ '@')
  FROM <table>;
```

Driven by an `information_schema.columns` query for every TEXT column named
`created_by` / `updated_by` / `deleted_by`. Easy to re-run after future
migrations as a regression check.
