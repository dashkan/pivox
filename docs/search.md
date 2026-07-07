# Asset search

Status: **design + partial implementation.** Lexical full-text search is built
(`assets.search_vector` + GIN). The faceted `SearchAssets` surface below is
designed but not yet built. Sections marked **OPEN** were never settled — do not
treat them as decided.

## The core model (and what it deliberately is *not*)

Asset search is **PostgreSQL + pgvector, no Elasticsearch** — but the locked core
is **full-text (`tsvector`) + faceted filtering over detected entities**, **not**
a lexical+semantic hybrid.

The design pivoted early: **semantic/vector search and paste-an-image search are
a deferred stretch goal.** The reasoning: if you don't need "find assets that
look like this image," you don't need vector kNN in the critical path — detect
the people/places/objects/topics at ingest and make them **text + facets**, and
typing "Obama" or "Paris" just works. So the `embedding vector(768)` column
stays **dormant** until the semantic stretch goal.

Consequently, **the ranking-fusion question is OPEN** (see Ranking) — it only
ever existed as a one-line default for the abandoned hybrid design.

## The dual-write (load-bearing)

Every entity the ingest pipeline detects is written **twice**:
1. as a **structured `asset_detection`** row — powers the facet sidebar (chips,
   counts, thumbnails, drill-down); and
2. **denormalized as text** folded into the searchable `tsvector` — so free-text
   search matches detected people/places/objects with no facet UI.

"Type it or click it — the same underlying rows." One GIN scan matches
display name + AI description + transcription + detected terms.

## Faceting = the tag system (not a bespoke catalog)

Facets reuse the existing tag system rather than a separate index or a new
`entities` catalog ("Design B"):

- **`tag_keys` = categories** (PERSON / PLACE / EVENT / OBJECT / COLOR / LANDMARK /
  SCENE / …). **`tag_values` = canonical entities** ("Barack Obama", "Paris"),
  carrying external IDs (Wikidata/IMDb) + aliases. Namespacing, org-scope,
  rename/merge, CRUD, RPCs, etag/audit are already built — reused wholesale.
- **`tag_bindings` stays editorial-only, unchanged.** Its
  `UNIQUE(parent_resource, tag_value_id)` must stay clean (editorial tags like
  `show:nightly-news` can't repeat).
- **New `asset_detections` table** holds AI/RULE **occurrences**. Detections need
  what bindings can't give: **multiplicity** (Obama at 00:12, 04:30, 11:05) and
  **per-occurrence typed attributes** (confidence, `time_range`, bbox). Hence a
  separate table, not overloaded bindings.

**Sidebar mechanics:** `GROUP BY category, entity_id` over detections joined to
the filtered set → each category's values with **counts + a thumbnail**. Chip
selections feed back as `EXISTS` predicates. **Counts recompute on drill-down**
(click Obama → places/topics/dates re-narrow and re-count). **Counts and results
are permission-scoped** — respect `assets.assets.read`; never leak hidden assets.

**Boolean semantics:** OR within a facet, AND across facets.

**Broadcast-specific facet categories** (confirmed needed): technical/format
(4K/HD/SD, HDR/SDR, frame rate, codec, color space), source/provenance
(Getty/AP/Reuters, camera, feed origin), rights/clearance (tracked + filterable,
**no hard blocking**), events/topics (vendor-definable — see below), media type,
capture date, duration, color, objects, language, show/collection. Plus
**badges** (built-in like "QC Verified" + vendor-custom; `badge_definition` +
`asset_badge`; filterable + card-visible).

## The `SearchAssets` RPC

A **dedicated custom method** — `POST …/assets:search` — **not** an extension of
`ListAssets`. `ListAssets` stays the deterministic AIP-160 browse/grid;
`SearchAssets` is the ranked/faceted experience. They coexist. (Rationale: an
AIP-160 filter ranks nothing; the response shapes differ; facet selections want
to be typed, not stringly; AIP puts ranking/aggregation on custom methods.)

- **Request:** free text + typed facet selections (`people[]`, `places[]`,
  `topics[]`, `colors[]`, `date_range`, `media_type`, technical…), with the
  OR-within / AND-across semantics above.
- **Response (one round-trip, results *and* facets):** ranked hits; **matched
  moments** (in-video timecode hits); **match reasons** ("why it matched" —
  highlighted transcript snippet + timecode + which entity chips hit); **facet
  blocks** (category → values → counts → thumbnails, with "show more" past
  top-N); totals.
- **New protos:** `assets/v1/search.proto` (the method), `assets/v1/detection.proto`
  (occurrences + face-cluster RPCs), `assets/v1/topic.proto`. **Extend** the tag
  protos for the vocabulary/gallery. `asset.proto` is barely touched —
  detections are a **sub-resource** (`ListAssetDetections`), not fields on
  `Asset` (same precedent as `GetAssetMetadata`).

> **OPEN — ranked-result pagination.** Ranked results break the id-keyset the
> other List endpoints use; the keyset-vs-offset approach for `SearchAssets` was
> never decided.

## Ranking

- **Shipped:** lexical `ts_rank(search_vector, plainto_tsquery) DESC`.
- **Newsroom default:** recency-weighted relevance, with newest / relevance /
  duration toggles. (The exact recency↔relevance blend was **not** formalized.)

> **OPEN — fusion math (the big one).** For the eventual semantic stretch goal,
> the *proposed* direction is Reciprocal Rank Fusion over `{ts_rank, vector
> cosine}` with facet filters as hard predicates, in a single CTE — but this was
> a one-line recommendation for the abandoned hybrid design. **No weights,
> normalization, RRF `k`, or weighted-sum alternative was ever chosen.** Decide
> before implementing semantic search. Do not cite a decided algorithm — there
> isn't one.

## Moments (search returns time, not just assets)

A 60-minute feed matching "2024 elections" at 00:12:30 via transcript must return
an **in-video timecode hit** deep-linking to that moment. Two layers:
- **Time-coded facts (stored):** detections carry `time_range`; a `transcript_cues`
  table holds timecoded lines.
- **Moments (derived at query time):** matched facts merged into contiguous
  ranges for display.

Rollup rule: a time-coded annotation **counts at the asset level for faceting**
but **expands to moments in results**. **v1 = derived moments only**; persistent
addressable subclips (EDL/chapters) are deferred.

## Events / topics (first-class facet)

A **Topic is a stored search predicate** (reusing the same predicate grammar),
not a new DSL — and topics, smart collections, and saved searches collapse to
**one primitive** (a stored predicate). Two execution modes:
- **Materialized** (topics as countable facets): persist `EVENT` annotations,
  backfill via an LRO on create/edit.
- **Live** (smart collections): evaluate at query time, store nothing.

Detection-qualifier predicates: `confidence > 0.85` (free column compare),
`person X on screen > 30s` (needs face **tracks** + an `asset_entity_stats`
rollup: `total_screen_seconds, appearance_count, first_seen, last_seen,
max_confidence`), `shot outdoors` (a SCENE category via a Places365-style
classifier). Modeled as a typed `EntityCondition{category, entity,
min_confidence?, min_screen_seconds?, min_appearances?}` — **no string DSL**.

**Guardrail:** a rule/search predicate is **validated against the tenant's
enabled capabilities** (can't save `screen_time > 30s` when face-tracking is
off). The per-space capability registry the ingestion workflow reads is the
*same* one the rule/search validator reads — consistent by construction.

**One predicate substrate, three consumers:** search filters, topic/event rules,
and (later) workflow conditions all lower through the one grammar
(`internal/filter`). This reuse is a locked architectural through-line.

## Embeddings (deferred with the semantic stretch goal)

- **Model:** `nomic-embed-text`, 768-dim, Ollama for dev; production provider
  **TBD** behind an **`Embedder`** interface. (An earlier pitch to switch to
  `nomic-embed-vision` for paste-an-image search was declined when image search
  became a stretch goal — the text model stays.)
- **Query embeddings cached** by hashed query text; **do not summarize before
  embedding.**
- **Generated as ingestion *workflow activities*** — "AI-describe → embed →
  pgvector" and "transcribe → index transcript" are activities of the ingestion
  workflow (per-space configurable), not a bespoke pipeline. They run in the
  Worker Process / on-agent per data-residency, never in the Cloud Controller.
- **Face recognition is a separate embedding space** — ArcFace `vector(512)` in a
  `tag_value_image` child table (the people gallery/recognition subsystem), not
  text search.

> **OPEN — re-embedding on model change.** Switching the embedding model means a
> full-gallery-and-detections backfill LRO (not a toggle); every detection stamps
> provider+model+version for invalidation, but the concrete re-embed procedure —
> and a model-version marker column — were never pinned.

## Schema (verified against `internal/db/migrations/000001_init.up.sql`)

**Built:**
- `assets.search_vector tsvector GENERATED ALWAYS AS (…) STORED`, weighted
  **A=`display_name`, B=`ai_description`, C=`transcription`**; `idx_assets_search`
  = **GIN** on it.
- `assets.embedding vector(768) NOT NULL DEFAULT array_fill(0, ARRAY[768])::vector`
  (dormant until semantic search).
- Current query (`internal/db/queries/assets.sql`) is **lexical-only**:
  `WHERE search_vector @@ plainto_tsquery(...) ORDER BY ts_rank(...) DESC`.

**Designed, not yet in the schema:**
- a **`detected_terms` column** dual-written by the pipeline and folded into the
  generated `search_vector` (weight B/C).
- `asset_detections`, `transcript_cues`, `asset_entity_stats`, `badge_definition`
  / `asset_badge`, `tag_value_image` tables.
- a **model-version marker** column.

> **OPEN — vector index.** There is **no ANN index** on `embedding` yet (only the
> GIN on `search_vector`). kNN would seq-scan; an HNSW (`USING hnsw (embedding
> vector_cosine_ops)`) or IVFFlat index is required past a few thousand assets,
> and **HNSW-vs-IVFFlat was never chosen.** Consistent with semantic search being
> deferred.

## Result cards & scoping

Cards show the "why it matched" snippet, rights/status + resolution/HDR + custom
badges, hover-scrub (ANIMATED_PREVIEW) + duration, version/near-duplicate
grouping, and multi-select → batch actions (action set open). All results and
facet counts are **permission-scoped** to `assets.assets.read`.

## Consolidated OPEN items

1. **Fusion math** for hybrid semantic ranking (RRF vs weighted; weights/`k`).
2. **Ranked-result pagination** for `SearchAssets`.
3. **Vector index** type (HNSW vs IVFFlat) — none exists yet.
4. **Re-embedding-on-model-change** procedure + the model-version marker column.
5. **`Embedder` production provider.**
6. The exact **recency↔relevance** blend for the newsroom default sort.

## Relationship to the workflow engine

Search is fed by **ingestion, which is a workflow**: entity detection, AI
description, embedding, and transcription are ingestion **activities**, so
whether an asset gets them is a **per-space, per-workflow** cost/behavior choice
— read from the same capability registry the search/rule validator uses. And the
**one predicate grammar** underpins search filters, topics, and workflow
conditions alike.
