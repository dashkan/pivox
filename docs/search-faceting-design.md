# Search, Facets & Aggregations — design decisions

**Status:** decided (design), not yet built. Captures the decisions from the
List-engine → faceting → search-engine discussion so we build from a spec, not
memory.

**One-liner:** Two tiers sharing one facet contract. Admin resources stay on the
PG `BuildListQuery` `List` RPCs with **terms facets + total_count** (cheap at
admin scale). High-volume operational resources (assets, requests, tag browsing)
get separate OpenSearch-backed `Search` RPCs with the **full** agg set
(date-histogram, range, relevance, multi-select at scale).

---

## 1. Two tiers: `List` vs `Search`

| | **`List*`** | **`Search*`** |
|---|---|---|
| Resources | admin: connectors, secrets, workflows, members, orgs, spaces | operational: assets, requests, tag browsing |
| Backing | Postgres via `filter.BuildListQuery` (already built) | OpenSearch (derived index) |
| Method | AIP-132 standard list, `GET` | AIP-136 custom method, `POST …:search` (rich body) |
| Aggs supported | **terms + total_count only** (live PG) | full: terms, date-histogram, range, metrics |
| Full-text / relevance | No | Yes |
| Why | configure-once, low cardinality; terms facets are cheap in PG | used daily, millions of rows, faceted search is the primary UX |

Rationale: both tiers share the facet contract (§2), but the *supported agg kinds*
and *backing* differ. Admin `List` needs simple **terms** facets (e.g. connectors
by agent, members by role) + a total count — all trivial live PG (`GROUP BY` /
`GROUPING SETS` over the scoped+filtered set at admin cardinality). The rich
stuff — date-histograms, ranges, relevance, multi-select faceting over millions —
is what justifies the separate OpenSearch `Search` tier. Keep the *engine*
distinction; don't put date-hist/range/metrics into `List` speculatively.

`ListAssets` / `ListRequests` still exist for programmatic / export access
(boring pagination through everything). `SearchAssets` is the human UI
(facets + relevance). Different callers, different guarantees.

---

## 2. Facet / aggregation contract (shared proto)

**All facet/agg types live in a shared `pivox.type` package** (mirroring
`google.type`), imported by every `Search*` request/response — they're
cross-resource, not per-service.

### Response

Generic map so adding/removing a facet is a backend change, never a proto change:

```proto
// pivox.type
message FacetBucket { string key = 1; int64 count = 2; string from = 3; string to = 4; }
message FacetResult { repeated FacetBucket buckets = 1; }

// pivox.assets.v1
message SearchAssetsResponse {
  repeated Asset           results         = 1;
  string                   next_page_token = 2;
  int64                    total_count     = 3;   // estimate at scale (see §5)
  bool                     total_is_estimate = 4;
  map<string, FacetResult> facets          = 5;   // key = agg name (defaults to field)
}
```

- **`facets` is a `map`, not typed per-facet fields** — zero proto churn to add a
  facet. Key = the `AggSpec.name` (defaults to the field id used in
  `filter`/`order_by`), so the UI wires "click bucket → add `field=value` filter"
  generically.
- **`repeated FacetBucket`, NOT `map<string, FacetBucket>`** — three reasons:
  (a) buckets are **ordered** (count-desc / chronological bins / range order) and
  proto maps are unordered; (b) only `terms` buckets have a scalar identity —
  date-histogram (timestamp) and range (`from`/`to`) buckets don't, so they can't
  be map keys without lossy label synthesis; (c) a bucket is a message that may
  grow (sub-aggs, `other_count`, is-selected). Its identity is a field (`key` /
  `from` / `to`), not a map key. Mirrors how OpenSearch returns buckets.
- **`FacetResult` wrapper is structural** — proto forbids `map<string, repeated …>`,
  so a one-message wrapper is the minimal legal map→list.
- Metrics (min/max/avg …) go in a separate `map<string,double> metrics` if ever
  needed — they're scalars, not buckets.

### Request — the consumer MUST opt into aggs (like filter/sort/pagination)

**No core AIP covers aggregations.** `filter` (AIP-160) and `order_by` are strings
because they're simple linear expressions; aggs aren't — each has a type + per-kind
params, so a string DSL would just reinvent ES's JSON agg DSL. The precedent is
Google Retail / Discovery Engine Search's **structured `repeated FacetSpec`** (note
its `excluded_filter_keys` == our self-excluding flag). So aggs are a **structured
repeated field**, everything else stays the AIP string DSLs.

```proto
// pivox.type
message AggSpec {
  string field = 1;              // field to aggregate; same id used in filter/order_by
  string name  = 2;              // optional response-map key (defaults to field);
                                 //   lets two aggs target one field (e.g. day + month)
  bool   self_excluding = 3;     // multi-select: drop this facet's own filter (default true)
  oneof kind {
    TermsAgg         terms          = 4;
    DateHistogramAgg date_histogram = 5;
    HistogramAgg     histogram      = 6;
    RangeAgg         range          = 7;
  }
}
message TermsAgg         { int32 size = 1; }                            // top-N
message DateHistogramAgg { string interval = 1; string timezone = 2; } // "1d","1M"
message HistogramAgg     { double interval = 1; }
message RangeAgg         { repeated Range ranges = 1; }
message Range            { string from = 1; string to = 2; }           // number or RFC3339

// pivox.assets.v1
message SearchAssetsRequest {
  string parent     = 1;
  string query      = 2;  // full-text
  string filter     = 3;  // AIP-160, reused from List
  string order_by   = 4;  // reused from List (relevance is the default)
  int32  page_size  = 5;
  string page_token = 6;
  repeated pivox.type.AggSpec aggs = 7;  // opt-in; empty = no aggregation cost
}
```

**Same contract on `List`.** `List*` requests carry the same
`repeated pivox.type.AggSpec aggs` (only the `terms` kind, backed by PG) and
return the same `map<string,FacetResult> facets` + `total_count`. So a facet
renders identically whether it came from a `List` (PG terms) or a `Search`
(OpenSearch) response — the frontend Grid consumes one shape. The tiers differ
only in supported agg kinds + backing, not in the wire contract.

### Agg types (all native to OpenSearch)
terms, date_histogram, numeric histogram, range, and basic metrics
(min/max/avg/sum/percentile). PG equivalents existed (`GROUP BY GROUPING SETS`,
`date_bin`, `width_bucket`) but are moot now that Search is engine-backed.

### Faceting semantics — **multi-select, self-excluding** (default)
Each facet applies every *other* active filter but **not its own**, so a user
who selected `color=blue` still sees `{blue, red, green}` in the Color facet and
can *add* red (multi-select). Facets on other dimensions (e.g. Style) are still
filtered by the active Color filter — that part is universal. Per-`AggSpec` flag
to opt a facet into single-select (fully-filtered) behavior when the UI is radio,
not checkbox. Degrades to "everything filtered" when nothing is selected.

---

## 3. Total count

- **`List` (admin):** cheap exact `COUNT(*)` over base + filters. Fine.
- **`Search` (operational):** **estimate** at scale — exact `COUNT(*)` over a
  million-row filtered set on every request is not viable. Return an estimate /
  capped value + `total_is_estimate`. Keyset gives no free total (neither does
  offset); it's a separate count either way.

---

## 4. Engine decision: **OpenSearch**

Backs the `Search` tier. Apache-2 (frictionless to ship on-prem, no copyleft, no
procurement conversation), **free horizontal sharding**, distributed writes,
mature aggregations/faceting + multi-tenancy. Checks the three requirements:
**no ceiling, shippable, free to scale in every deployment.** Cost accepted:
heavier ops than the lighter engines (JVM, cluster mgmt, memory, tuning).

### Rejected alternatives (so we don't re-litigate)
| Option | Why not |
|---|---|
| **PG-native faceting** (hand-rolled live `GROUPING SETS`) | Real-time multi-select faceting + counts over millions is search-engine territory; single-primary ceiling + write-amplification on the high-ingest primary; and it's hand-rolled facet SQL vs a real search engine. (ParadeDB, below, is the *good* version of "search in PG" — evaluated separately.) |
| **Meilisearch** | Loved the simplicity (Rust, fast, MIT single-node), but **horizontal sharding + replication are Enterprise-only (v1.37+)** — per-deployment commercial licensing to scale on-prem is a disqualifier — and **writes route through a single leader** (indexing bottleneck) even when sharded. |
| **Elasticsearch** | Viable — **AGPL since Aug 2024**, and running it unmodified as a sidecar doesn't infect our code. But AGPL is copyleft, and many enterprise buyers reflexively reject AGPL in a shipped bundle. OpenSearch (Apache-2) removes that conversation entirely. ES is arguably the stronger engine; the license friction is the deciding factor for a shipped on-prem product. |
| **Typesense** | GPL-3 (redistribution concern) + lower proven ceiling than OpenSearch. |
| **ParadeDB (`pg_search`, AGPL-3.0)** | The best "search in PG" by far — real Tantivy BM25 + faceting/aggregations, and a pure Postgres extension where **index updates are in the same transaction as writes**: no sync pipeline, transactionally consistent, one system, on-prem-clean. The catch: **read replicas + HA + multi-node are Enterprise-only**, and *"in a primary-replica topology, BM25 indexes in Community are only available on the primary."* So free Community serves **all** search/facet query load from the single primary (alongside OLTP + synchronous index maintenance) with **no read-offload and no search HA**. For faceted search as the primary UX over millions, that forces Enterprise (per-deployment license to scale — the Meilisearch problem) → OpenSearch's free horizontal + read scaling wins. Reconsider only if a single primary comfortably absorbs combined OLTP + search-query load and no search HA is acceptable. AGPL compliance burden is ours + trivial if unmodified; only a concern for strict "no-AGPL-in-SBOM" buyers. |

The whole architecture is **engine-agnostic** — the `Search`/facet contract is
identical regardless of engine, so this is "pick the backing," not a redesign.

---

## 5. Data flow & sync

- **Postgres remains the source of truth.** OpenSearch is a **derived index**.
- **Sync rides the existing Kafka** (already in the stack for KC event sync):
  change events → Kafka → indexer → OpenSearch. Build once; identical in cloud
  and on-prem (the DRY win).
- Needs: a **reindex/backfill path**, and **eventual-consistency** semantics
  (a just-written asset may lag its index entry briefly — acceptable for search;
  the write itself is committed in PG synchronously).
- **Write-path decoupling** is the reason for CDC-not-synchronous-indexing:
  asset ingestion stays cheap in PG; indexing happens async off Kafka, so index
  maintenance never throttles ingestion.

---

## 6. Multi-tenancy

**Server-mediated.** Client → Pivox `SearchAssets` RPC → OpenSearch. Pivox applies
the `org_id` / `space_id` (+ permitted-spaces) scope filter in its OpenSearch
query — the same base-scope predicate it already builds for PG. Authz stays in
Pivox (the BFF/backend already knows memberships/permissions); OpenSearch stays
internal. No client-facing tenant tokens needed (those are for client-direct
architectures).

---

## 7. Deployment

One engine, one codebase, **cloud + on-prem**. OpenSearch ships as one more
service in the existing container stack (Postgres, Keycloak, agentgateway,
rustfs, Kafka, otel) — incremental, not a new paradigm.

---

## 8. Open questions / before building

1. **Volume trajectory** — per-tenant + aggregate asset/request counts today and
   growth. Drives shard count and validates the "no ceiling" bet. This is the
   one input still missing.
2. **Scale validation on representative hardware**, not a laptop — a laptop
   spike answers correctness, not scale (wrong IO/RAM/concurrency). Validate a
   point on the curve on a provisioned box at target volume; define a reference
   spec + minimum requirements for on-prem.
3. **`SearchAssets` request contract** — full-text query, filter, `AggSpec[]`,
   pagination, sort/relevance, highlighting.
4. **Frontend `Search` consumer** — search box + facet sidebar + relevance
   ordering; distinct from the admin `List` Grid, though facets render from the
   same generic map. Likely a `SearchGrid` variant.
5. **Reindex & schema-evolution** operational story (mapping changes, backfill).
