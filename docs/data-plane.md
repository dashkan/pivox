# Pivox Data Plane — Architecture & Design

## Overview

The Pivox Data Plane is the live data infrastructure that connects external data feeds to on-air templates. It provides operator control (gating, approval, pause, override), throttling, schema versioning, and high-performance shared memory delivery to the engine.

**The Data Plane is a key differentiator.** When a template uses the Pivox Data Plane, the operator gets full visibility and control over every data field flowing to air — pending changes, approval gates, pause/resume, throttle adjustment, manual override. Templates that bypass the Data Plane (using direct `fetch()`/WebSocket) lose all of this — data flows as a black box with no operator oversight.

**Architecture documents:**
- `docs/data-plane.md` — this document. Data Plane architecture, shared memory, feeds, schemas.
- `docs/engine.md` — playout engine. Rendering, compositing, SDK (including `pivox.feeds` and `pivox.model` APIs).
- `docs/control-plane.md` — control plane. NRCS, operator UI, Data Plane service, hardware automation.
- `docs/architecture.md` — system-level architecture, deployment tiers.

## Data Plane Components

The Data Plane spans two machines — the CP server handles intelligence (routing, gating, throttling), the engine machine handles the last mile (shared memory writes). The CP does **not** run on the engine machine to avoid consuming rendering resources.

```
┌──────────────────────────────────────────────────────────┐
│  CP SERVER (Go)                                           │
│                                                           │
│  ┌──────────────────────────────────────────────────┐    │
│  │  Feed Connectors (pluggable, per data provider)   │    │
│  │  - AP Elections, Opta Sports, Reuters, custom     │    │
│  │  - Normalize provider data → Pivox feed schema    │    │
│  └──────────────────────┬───────────────────────────┘    │
│                          │ normalized data                │
│  ┌──────────────────────┴───────────────────────────┐    │
│  │  Routing / Gating Engine                          │    │
│  │  - Per-field update mode: auto / gated / manual   │    │
│  │  - Operator controls: pause, resume, override     │    │
│  │  - Write-side throttling                          │    │
│  └──────┬───────────────────────────┬───────────────┘    │
│         │                           │                     │
│         ▼                           ▼                     │
│  ┌──────────────┐          ┌────────────────────┐        │
│  │ gRPC Path    │          │ gRPC Feed Stream   │        │
│  │              │          │                    │        │
│  │ UpdateCommand│          │ Stream field       │        │
│  │ → engine     │          │ updates to engine  │        │
│  │ → SDK view   │          │ machine            │        │
│  │   model      │          │                    │        │
│  └──────────────┘          └────────────────────┘        │
│                                                           │
│  ┌──────────────────────────────────────────────────┐    │
│  │  Schema Registry                                  │    │
│  │  - Versioned feed schemas (e.g., pivox.sports.v1) │    │
│  │  - Validation at config/load time (not runtime)   │    │
│  └──────────────────────────────────────────────────┘    │
│                                                           │
│  ┌──────────────────────────────────────────────────┐    │
│  │  Operator UI Controls                             │    │
│  │  - Per-field: auto/gated/manual, pause, override  │    │
│  │  - Per-feed: enable/disable, throttle, health     │    │
│  └──────────────────────────────────────────────────┘    │
└──────────────────────┬───────────────────────────────────┘
                       │ gRPC (commands + feed data stream)
                       │ LAN / network
┌──────────────────────┴───────────────────────────────────┐
│  ENGINE MACHINE (dedicated broadcast hardware)            │
│                                                           │
│  ┌──────────────────────────────────────────────────┐    │
│  │  Shared Memory Writer (Rust — inside supervisor)  │    │
│  │  - Receives feed data stream from CP via gRPC     │    │
│  │  - Writes to /dev/shm/pivox-feeds (lock-free)     │    │
│  │  - ~50MB RAM, negligible CPU                      │    │
│  │  - Part of engine supervisor, not a separate proc │    │
│  └──────────────────────┬───────────────────────────┘    │
│                          │ shared memory (local)          │
│  ┌──────────────────────┴───────────────────────────┐    │
│  │  Channel Processes                                │    │
│  │  - SDK reads shared memory per frame              │    │
│  │  - Fires subscription callbacks at template rate  │    │
│  └──────────────────────────────────────────────────┘    │
└──────────────────────────────────────────────────────────┘
```

**Why the CP doesn't run on the engine machine:** The engine machine is dedicated broadcast hardware running at 60fps with zero margin. Running PostgreSQL, Go services, Redis, and a web server alongside the rendering pipeline risks CPU/memory/disk contention that could cause dropped frames. Only the engine supervisor (Rust, ~20MB) and the shared memory writer (Rust, ~50MB, part of supervisor) run on the engine machine.

**Latency impact:** The gRPC feed stream adds ~0.5-2ms over a facility LAN compared to a co-located write. Still well within the 16.68ms frame budget. For high-frequency feeds, gRPC streaming batches efficiently.

## Two Data Paths

The Data Plane delivers data to the engine via two paths. Templates can use both simultaneously.

| | View Model (`pivox.model`) | Shared Memory Feeds (`pivox.feeds`) |
|---|---|---|
| **Mechanism** | Push — data arrives, SDK patches view model, bindings fire automatically | Subscribe — SDK checks shared memory per frame, fires callback at requested rate |
| **Delivery** | gRPC `UpdateCommand` from CP to engine | Engine reads shared memory directly (sub-microsecond) |
| **Latency** | ~0.3-1ms per update | ~0.001ms per read |
| **Template code** | Declarative bindings in `onLoad()` | Subscribe once in `onLoad()`, receive callbacks |
| **Throttling** | Write-side only (control plane) | Two layers: write-side (operator) + read-side (template) |
| **Operator control** | Full — auto/gated/manual per field, pause, override | Feed-level — enable/disable, set write throttle, pause, override |
| **Best for** | Operator-controlled fields, editorial data, infrequent updates | High-frequency live data, real-time visualizations, tickers, clocks |

### View Model Path (gRPC)

For operator-controlled data with editorial oversight:

```
External Data Feeds
  │
  ▼
Data Plane routing/gating
  │
  ├── AUTO fields ──────► UpdateCommand sent directly to engine
  │
  ├── GATED fields ─────► held in operator UI as PENDING
  │                        operator clicks APPROVE
  │                        → UpdateCommand sent to engine
  │
  └── MANUAL fields ────► operator edits in UI
                           operator clicks UPDATE
                           → UpdateCommand sent to engine
```

Engine receives `UpdateCommand` → patches the SDK view model → bound elements and watchers fire automatically. The engine does not know or care whether the update came from an operator, an automated feed, or an approved gate.

### Shared Memory Path (High-Frequency)

For high-frequency data where gRPC overhead matters:

```
External feed pushes 50 updates/sec
  │
  ▼
Data Plane writes to shared memory at configured rate
  │
  ▼
Engine SDK reads shared memory per frame (sub-microsecond)
  │
  ▼
SDK fires subscription callbacks at template's requested rate
```

## Shared Memory Architecture

### Hierarchical Key-Value with Lock-Free Double Buffer

Each feed is a shared memory region containing **individually keyed and versioned fields**. The writer (shared memory writer in the engine supervisor, receiving data from the CP via gRPC stream) can update a single field without rewriting the entire feed. The reader (engine channel processes) can subscribe to specific fields and detect per-field changes.

**Memory layout — hierarchical: feed → fields, each field double-buffered:**

```
Shared memory region per feed:

┌──────────────────────────────────────────────────────────┐
│  Feed header (128 bytes, cache-line aligned)              │
│                                                           │
│  feed_name: char[48]                                     │
│  feed_version: uint64 (atomic)  — incremented on ANY     │
│                                   field write             │
│  field_count: uint32                                     │
│  max_fields: uint32             — pre-allocated capacity  │
│                                                           │
├──────────────────────────────────────────────────────────┤
│  Field index (fixed-size array, one entry per field)      │
│                                                           │
│  field[0]:                                                │
│    key: char[64]              — "home_score"             │
│    version: uint64 (atomic)   — incremented on THIS      │
│                                 field's write only        │
│    active_buffer: uint8 (atomic) — 0 or 1                │
│    data_size: uint32          — bytes in value payload    │
│    offset_a: uint32           — offset to buffer A        │
│    offset_b: uint32           — offset to buffer B        │
│                                                           │
│  field[1]:                                                │
│    key: "away_score"                                     │
│    version: ...                                           │
│    ...                                                    │
│                                                           │
│  field[N]: ...                                            │
│                                                           │
├──────────────────────────────────────────────────────────┤
│  Value buffers (double-buffered per field)                │
│                                                           │
│  field[0] buffer A: "3"                                  │
│  field[0] buffer B: "2"         ← previous value         │
│  field[1] buffer A: "2"                                  │
│  field[1] buffer B: "2"                                  │
│  field[2] buffer A: "04:21"                              │
│  field[2] buffer B: "04:22"     ← previous value         │
│  ...                                                      │
└──────────────────────────────────────────────────────────┘
```

### Write Path — Single Field Update

```
Example: clock changed from "04:22" to "04:21"

1. Look up field index for key "clock" → field[2]
2. Determine inactive buffer (if active_buffer == 0, write to B; if 1, write to A)
3. Write new value "04:21" to field[2]'s INACTIVE buffer
4. Memory fence (ensure write is visible)
5. Atomically swap field[2].active_buffer (0 → 1 or 1 → 0)
6. Atomically increment field[2].version
7. Atomically increment feed_version (signals "something in this feed changed")

Only field[2] is touched. All other fields remain unchanged.
Their version counters do not increment.
```

### Write Path — Whole Feed Update

When the source pushes a complete data snapshot, all fields are updated in a single pass:

1. For each field: write new value to inactive buffer, swap pointer, increment field version
2. Atomically increment feed_version once at the end

### Read Path — Full Feed

```
1. Read feed_version (atomic)
   — if unchanged since last read, return { changed: false }
2. For each field:
   a. Read field.active_buffer (atomic)
   b. Read value from the ACTIVE buffer
3. Return all field values as JSON object
```

### Read Path — Specific Fields Only

```
Example: template subscribes to ["clock", "powerplay"]

1. Read field[2].version ("clock") and field[9].version ("powerplay")
   — if both unchanged since last read, return { changed: false }
2. Read only the changed fields' active buffers
3. Return only the requested fields
```

### Why This Is Safe

- Each field is independently double-buffered — writer and reader never access the same buffer for the same field simultaneously
- Writer updates one field's inactive buffer, then atomically swaps the pointer for that field only
- Reader always reads complete, consistent values — each field's active buffer is never partially written
- No locks — all coordination via atomic operations
- No blocking — reader never waits for writer, writer never waits for reader
- Cache-line alignment on field headers prevents false sharing between adjacent fields
- Per-field versioning means the reader skips unchanged fields — reads only what's new

### Nested Hierarchies

Fields support dot-notation keys for nested data deeper than one level:

```
Feed: "match"
  ├── field: "home.score"        → 3
  ├── field: "home.shots"        → 28
  ├── field: "home.penalties"    → 4
  ├── field: "away.score"        → 2
  ├── field: "away.shots"        → 31
  ├── field: "clock.elapsed"     → "43:21"
  ├── field: "clock.period"      → "2nd"
  └── field: "clock.stoppage"    → "00:00"
```

Templates subscribe to any level of the hierarchy:

```javascript
// Subscribe to all clock fields
pivox.feeds.subscribe('match', {
  fields: ['clock.*'],           // wildcard — matches clock.elapsed, clock.period, clock.stoppage
  maxUpdatesPerSec: 60,
  onUpdate: (data) => {
    // data = { clock: { elapsed: "43:21", period: "2nd", stoppage: "00:00" } }
  }
});

// Subscribe to specific nested fields
pivox.feeds.subscribe('match', {
  fields: ['home.score', 'away.score'],
  maxUpdatesPerSec: 10,
  onUpdate: (data) => {
    // data = { home: { score: 3 }, away: { score: 2 } }
  }
});

// Read nested field directly
const elapsed = pivox.feeds.read('match', 'clock.elapsed');
// Returns: "43:21"
```

The writer can update at any level — updating `clock.elapsed` only increments that field's version counter. Templates subscribed to `clock.*` get notified. Templates subscribed to `home.*` do not.

### Performance

- Writer only touches the memory region of the changed field — minimal cache line impact
- Reader only parses changed fields — no JSON parsing of unchanged data
- Per-field version counters enable precise change detection for subscriptions to field subsets
- Template subscribing to `["clock"]` only checks one version counter per frame, not the entire feed
- All channel processes on the same machine share the same memory-mapped feeds — write once, read many

**Performance validation (Phase 1):** Test with high write rates (100+ field writes/sec) and verify no impact on engine frame rate. If cache line thrashing between writer and reader cores is observed, write throttling becomes a performance feature rather than just an optimization. Other broadcast systems (Vizrt) have documented negative playback impact from high shared memory write rates — validate whether the per-field double-buffer pattern eliminates this.

## Two-Layer Throttling

```
External feed pushes 50 updates/sec
  │
  ▼
WRITE THROTTLE (Data Plane — operator-controlled)
  │  Config: max_writes_per_sec: 10
  │  Data Plane drops intermediate updates, writes at max 10/sec
  │  Operator can adjust at runtime in UI
  ▼
Shared memory holds latest value per field (updated 10x/sec)
  │
  ▼
READ THROTTLE (SDK — template-controlled)
  │  SDK checks shared memory every frame (~60fps)
  │  But only fires onUpdate at template's maxUpdatesPerSec
  │  e.g., maxUpdatesPerSec: 5 → callback fires 5x/sec
  ▼
Template receives data at its requested rate (5x/sec)
```

**Write throttle** (operator-controlled): How often the Data Plane writes to shared memory. Limits source-side noise. Operator adjusts in UI.

**Read throttle** (template-controlled): How often the SDK delivers updates to the template's callback. Set per subscription via `maxUpdatesPerSec`. Lets the template control its own rendering budget — a complex visualization might only want 5 updates/sec even if the data changes 60x/sec.

### Feed Configuration

```yaml
feeds:
  scores:
    source: "ws://feeds.opta.com/live"
    schema: "pivox.sports.nfl.v1"
    throttle:
      max_writes_per_sec: 10

  ticker:
    source: "ws://feeds.reuters.com/headlines"
    schema: "pivox.financial.ticker.v1"
    throttle:
      max_writes_per_sec: 2

  clock:
    source: "internal"           # generated from timecode
    throttle:
      max_writes_per_sec: 60     # every frame

  telemetry:
    source: "ws://telemetry.internal/f1"
    schema: "pivox.sports.f1.telemetry.v1"
    throttle:
      max_writes_per_sec: 30
      batch: true                # batch multiple data points per write
```

### Operator Controls

**Per feed:**
- Enable / disable a feed (stop writing to shared memory)
- Adjust write throttle rate at runtime
- Pause a feed (freeze last value in shared memory)
- View feed health (connected, last update timestamp, error state)

**Per field (view model path):**
- Auto / gated / manual mode per field
- Pause auto fields (freeze current value on-air)
- Resume paused fields (latest feed value pushed to air)
- Override any field manually (temporarily disconnects from feed)
- Approve gated fields (pushes pending value to air)
- Switch modes at runtime (auto → gated mid-show)

**Operator UI:**

```
┌─ ON AIR: Election Results Board ─────────────────────────┐
│                                                           │
│  Field                Value           Source    Control   │
│  ─────────────────────────────────────────────────────── │
│  race_name            "US President"  Manual   [Edit]    │
│  candidate_a_name     "Smith"         Manual   [Edit]    │
│  candidate_a_votes    1,234,567       Auto ●   [Pause]   │
│  candidate_b_votes    1,122,334       Auto ●   [Pause]   │
│  candidate_a_pct      52.3            Auto ●   [Pause]   │
│  projected_winner     (pending: true) Gated    [Approve] │
│                                                           │
│  ● = live feed connected, updating                       │
│                                                           │
│  [Override All]  [Pause All]  [Resume All]               │
└───────────────────────────────────────────────────────────┘
```

## Feed Schema Versioning

Every feed has a versioned schema (e.g., `pivox.sports.nfl.v1`). Templates declare which schema version they expect. This is a **design-time and configuration-time contract**, not a runtime enforcement layer.

```javascript
// Template declares expected schema
pivox.feeds.subscribe('scores', {
  schema: 'pivox.sports.nfl.v1',
  fields: ['home.score', 'away.score'],
  maxUpdatesPerSec: 10,
  onUpdate: (data) => { ... }
});
```

### Validation — Config Time, Not Runtime

| When | What | Frequency |
|---|---|---|
| Connector startup | Validate source feed structure matches declared schema | Once |
| Template load | Validate template's expected schema matches feed's schema | Once per load |
| Config change | Validate connector output schema matches template expectations | Once per change |
| Runtime (optional) | Spot-check one write per minute, alert on drift | Periodic, never on hot path |
| **Every write** | **No validation — data flows unvalidated to shared memory** | **Never** |

**The engine does not validate feed data.** It reads bytes from shared memory and hands them to JavaScript. If a field is missing, the template gets `undefined` — standard JS behavior. Schema validation on the hot path (10-50K writes/sec during peak) would add CPU overhead for no on-air benefit.

### Schema Compatibility

When a feed schema upgrades (e.g., `v1` → `v2`), connectors can serve multiple schema versions. Templates pinned to `v1` continue working until they're updated. Mismatch between template and feed schema versions produces a warning in the operator UI at load time, not a silent runtime failure.

## Template Manifest — Data Declarations

Templates declare their data requirements in the manifest. The Data Plane uses this to wire up routing:

```json
{
  "name": "election-results-board",
  "version": "2.1",
  "fields": {
    "race_name": {
      "type": "string",
      "default_update_mode": "manual",
      "label": "Race Name"
    },
    "candidate_a_votes": {
      "type": "number",
      "default_update_mode": "auto",
      "data_source": "ap_elections",
      "binding": "results.{race_id}.candidates[0].votes",
      "throttle_max_per_sec": 2
    },
    "projected_winner": {
      "type": "boolean",
      "default_update_mode": "gated",
      "label": "Call Race",
      "gate_reason": "Editorial approval required before calling race on-air"
    }
  },
  "feeds": {
    "ap_elections": {
      "schema": "pivox.elections.v1",
      "fields_used": ["candidates.*", "reporting_pct", "called"]
    }
  }
}
```

The manifest declares `default_update_mode` — the operator can override this at runtime. For example, switching `candidate_a_votes` from `auto` to `gated` mid-show when results become contentious.

## Feed Connectors

Connectors are pluggable — adding a new data source is implementing a Go interface that normalizes the provider's data format to a Pivox feed schema.

| Feed Type | Protocol | Examples |
|---|---|---|
| Sports scores | REST/WebSocket | Opta, Stats Perform, SportRadar |
| Election results | REST/WebSocket | AP Elections, Reuters Decision Desk |
| Financial data | WebSocket/FIX | Reuters, Bloomberg, custom |
| Weather | REST | National Weather Service, AccuWeather |
| Social media | REST/WebSocket | Twitter/X API, custom aggregators |
| Custom | REST/WebSocket/TCP | Any JSON/XML source |

### Connector Interface

```go
type FeedConnector interface {
    // Connect to the data source
    Connect(config ConnectorConfig) error

    // Schema this connector outputs
    Schema() FeedSchema

    // Start receiving data — calls onData for each update
    Start(onData func(field string, value []byte)) error

    // Stop receiving data
    Stop() error

    // Health check
    Health() ConnectorHealth
}
```

Multiple connectors for the same domain (e.g., AP and Reuters for elections) output the same Pivox schema — the template works with either provider.

## Deployment Topology

### Where Each Component Runs

| Component | Runs On | Language | Purpose |
|---|---|---|---|
| Feed connectors | CP server | Go | Connect to external data sources |
| Routing / gating engine | CP server | Go | Auto/gated/manual per field, operator controls |
| Schema registry | CP server | Go | Versioned feed schemas, validation |
| Operator UI controls | CP server (web UI) | Go + React | Per-field monitoring, approval, override |
| Shared memory writer | Engine machine (in supervisor) | Rust | Receive gRPC stream → write to `/dev/shm/` |
| Shared memory reader | Engine machine (in SDK) | Rust + JS | Read `/dev/shm/` → fire subscription callbacks |

### Hybrid Deployment

In hybrid deployments, the CP runs on-prem (separate server) or in the cloud. Feed connectors run on the CP server and connect to data sources directly — no cloud round-trip for the data itself.

```
Cloud CP configures: "connect to AP Elections at ws://feeds.ap.org/..."
  │
  ▼
On-prem CP server connects to feed DIRECTLY
  │
  │ applies routing/gating/throttling
  │
  ▼
Streams field updates to engine machine via gRPC
  │
  ▼
Shared memory writer (in engine supervisor) writes to /dev/shm/
  │
  ▼
Template receives data via pivox.feeds.subscribe()
```

For feeds that need minimum latency (tickers, telemetry), the CP server should be on the facility LAN — not in the cloud. The cloud CP can configure which feeds to connect to, but the actual data flow stays local.

Templates can also subscribe directly to customer-maintained feed endpoints via `fetch()`/WebSocket in CEF, bypassing the Data Plane entirely — but they lose all operator controls (see Data Plane vs Direct Fetch below).

## Redundancy — Multi-Engine Feed Delivery

When running redundant engines (Engine A primary, Engine B standby), the CP streams feed data to both engine machines. The shared memory on both machines should have the same data so a changeover doesn't cause visible glitches.

### Approach: Dual-Send, Not Synchronized Writes

The CP sends each feed update to **both engine machines back-to-back** in the same operation (two gRPC sends microseconds apart). No explicit synchronization protocol between engines.

```
CP Data Plane receives feed update
  │
  ├──gRPC──► Engine A supervisor ──► shared memory A
  │          (~0.5-1ms LAN latency)
  │
  └──gRPC──► Engine B supervisor ──► shared memory B
             (~0.5-1ms LAN latency)

Natural skew: ~0.1-2ms between engines (same LAN switch)
Frame budget: 16.68ms
→ Both engines write within the same frame in almost all cases
```

### Why Full Synchronization Isn't Needed

- **Infrequent updates** (scores, election results): A ~1ms skew means Engine B shows the new value 1ms later. Invisible — the data changes once, both engines have it before the next frame.
- **Clocks/timers**: Not delivered via the Data Plane. `pivox.system.time` is derived from genlock/NTP independently on each engine machine. Already synchronized by timing infrastructure.
- **High-frequency feeds** (tickers): Both engines receive updates within the same frame. Even if a changeover happens mid-update, scrolling tickers mask momentary value differences.

### If Synchronization Is Ever Needed (Not Day One)

If testing during Phase 4 (redundancy) reveals visible issues during changeover, a frame-tagged delivery mechanism can be added:

1. CP tags each feed update with a **target frame number** (the frame this data should first appear on-air)
2. Engine supervisor buffers the update until that frame arrives (synced to genlock)
3. Both engines write to shared memory on the same genlock edge
4. Guarantees pixel-identical output on both engines

This adds ~1 frame of latency (data must arrive at least one frame early) and complexity. Build only if the simpler dual-send approach proves insufficient.

## Data Plane vs Direct Fetch

Templates can bypass the Data Plane entirely using standard browser APIs (`fetch()`, `WebSocket`, `EventSource`). CEF is a full browser — this works. But the template loses all Data Plane benefits:

| Capability | With Pivox Data Plane | Direct fetch/WebSocket |
|---|---|---|
| Operator visibility | Full — sees every field, pending changes, feed health | None — black box |
| Gated approval | Yes — operator approves before data goes to air | No |
| Pause / resume | Yes — per field or per feed | No |
| Manual override | Yes — operator overrides any value | No |
| Throttling | Two layers (write + read) | Template's responsibility |
| Schema versioning | Validated at load time | None |
| Monitoring / alerting | Feed health in dashboard | Invisible to Pivox |
| Offline resilience | Shared memory holds last value | Connection lost = no data |
| Failover | Data Plane handles reconnect | Template handles its own |

**Guidance:** Use the Data Plane for any data the operator needs to monitor or control. Use direct fetch for template-internal concerns that don't affect on-air content (analytics, logging, supplementary CDN assets).

## Capacity Estimates

Shared memory requirements for extreme broadcast scenarios:

| Use Case | Fields | Shared Memory |
|---|---|---|
| Presidential election — all races nationwide (~5,500 races × 200 fields/candidate × 2 candidates) | ~2,200,000 | ~460 MB |
| CNBC — all US equities + global indices + commodities + crypto | ~93,500 | ~16 MB |
| ESPN — all live major sports (peak concurrent) | ~30,000 | ~6 MB |
| **All three combined** | **~2,323,500** | **~482 MB** |

On a production machine with 64-128GB RAM, even the most extreme scenario uses under 500MB — less than 1% of available memory.

The bottleneck for these workloads is write throughput, not memory. At 50,000 field writes/sec (CNBC peak market hours), the per-field double-buffer pattern writes ~8 MB/sec of memory — trivial for modern CPUs.
