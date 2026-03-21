	# Pivox Control Plane — Architecture & Design

## Overview

The Pivox control plane is a Go application that manages all broadcast operations above the rendering engine. It is the brain of the system — handling NRCS/rundown management, asset management, operator UI, data binding, hardware automation, redundancy coordination, and all external integrations.

**The control plane is a single Go codebase that runs in two modes:**
- **Cloud mode** — source of truth for configuration, user management, asset storage. Serves the web UI.
- **Local mode** — runs on-prem alongside the engine. Syncs with cloud, operates independently during outages. Manages local engine(s), local storage, and data feed relays.

Both modes use the same binary with different configuration. See `docs/architecture.md` for deployment tiers (Cloud, Hybrid, On-Prem).

The control plane communicates with the playout engine via gRPC over TCP (facility LAN — the standard deployment, CP and engine on separate machines) or Unix domain sockets (single-machine deployments only). See `docs/engine.md` for the engine architecture.

**Day-one scope:**
- NRCS with rundown management
- Template registry and versioning
- Asset management with nearline/cloud storage
- Asset cache manager with look-ahead preloading
- Data Plane (live feeds, auto/gated/manual routing, shared memory, schema versioning)
- Operator web UI (browser + Electron)
- Broadcast hardware automation
- Redundancy coordination
- Compliance recording management and real-time indexing
- Timer/auto-advance service
- MOS gateway (legacy NRCS integration)
- VDCP gateway (automation integration)
- Monitoring and alerting

## Technology Stack

| Component | Technology | Notes |
|---|---|---|
| Language | Go | All control plane services |
| API | gRPC + REST (dual) | gRPC for engine + internal services, REST for operator UI + external integrations |
| Database | PostgreSQL | Rundowns, templates, assets, configuration, audit trail |
| Search | Elasticsearch or Meilisearch | Semantic search for recorded content, asset discovery |
| Cache | Redis | Session state, real-time status, pub/sub for UI updates |
| Message bus | NATS or Redis Streams | Internal event distribution between services |
| Frontend | React + TypeScript | Operator UI, template editor, media browser |
| Desktop | Electron | Desktop app wrapping the web UI with native OS integration |
| Object storage | S3-compatible (rustfs on-prem, AWS S3, GCS) | Asset storage, compliance recording archive. Each org configures 1+ storage locations. |

## System Architecture

```
┌─────────────────────────────────────────────────────────────────────┐
│  PIVOX CONTROL PLANE (Go)                                            │
│                                                                      │
│  ┌──────────────────────────────────────────────────────────────┐   │
│  │                      API Gateway                              │   │
│  │  REST (operator UI, external integrations)                    │   │
│  │  gRPC (engine communication, internal services)               │   │
│  │  WebSocket (real-time UI updates, preview streams)            │   │
│  └──┬───────────────────────────────────────────────────────────┘   │
│     │                                                                │
│  ┌──┼──────────────────────────────────────────────────────────┐    │
│  │  │              Core Services                                │    │
│  │  │                                                            │    │
│  │  ├── Playout Controller (state machine, command routing)      │    │
│  │  ├── NRCS / Rundown Manager                                   │    │
│  │  ├── Template Registry (versioning, approval workflow)        │    │
│  │  ├── Asset Manager (MAM, nearline, cloud)                     │    │
│  │  ├── Asset Cache Manager (look-ahead preload to engine)       │    │
│  │  ├── Data Plane (feed routing, gating, throttling, shared mem) │    │
│  │  ├── Timer Service (auto-advance, frame-accurate)             │    │
│  │  ├── Redundancy Coordinator (dual-write, failover)            │    │
│  │  ├── Recording Manager (compliance, ingest, indexing)         │    │
│  │  └── Monitoring Service (health, metrics, alerting)           │    │
│  └──────────────────────────────────────────────────────────────┘    │
│                                                                      │
│  ┌──────────────────────────────────────────────────────────────┐   │
│  │              Integration Gateways                             │   │
│  │                                                                │   │
│  │  ├── MOS Gateway (NRCS integration — legacy, to be replaced)  │   │
│  │  ├── VDCP Gateway (automation integration)                    │   │
│  │  ├── TSL UMD Gateway (tally from vision mixers)               │   │
│  │  ├── Hardware Automation Gateway (routers, mixers, etc.)      │   │
│  │  └── Data Feed Connectors (AP, Opta, custom APIs)             │   │
│  └──────────────────────────────────────────────────────────────┘   │
│                                                                      │
│  ┌──────────────────────────────────────────────────────────────┐   │
│  │              Operator UI (React + Electron)                   │   │
│  │                                                                │   │
│  │  ├── Rundown editor                                           │   │
│  │  ├── Channel monitoring (MJPEG preview + status)              │   │
│  │  ├── Template browser + data entry                            │   │
│  │  ├── Media browser + clip management                          │   │
│  │  ├── Transition selector                                      │   │
│  │  ├── Live data monitor (per-field controls)                   │   │
│  │  ├── Hardware status dashboard                                │   │
│  │  └── Template editor (WYSIWYG — future)                      │   │
│  └──────────────────────────────────────────────────────────────┘   │
├──────────────────────────────────────────────────────────────────────┤
│  gRPC over UDS / TCP                                                 │
├──────────────────────────────────────────────────────────────────────┤
│  PLAYOUT ENGINE (Rust + C++) — see docs/engine.md                    │
├──────────────────────────────────────────────────────────────────────┤
│  BROADCAST FACILITY HARDWARE                                         │
│  Routers, mixers, multiviewers, audio desks, record servers          │
└──────────────────────────────────────────────────────────────────────┘
```

## Core Services

### Playout Controller

The central state machine that coordinates everything. It translates operator actions and automation triggers into engine commands.

**Responsibilities:**
- Maintains the on-air state for every channel and layer (what's playing, what's cued)
- Translates high-level operator actions into low-level engine commands (e.g., "play audio item" → VideoLoadCommand + LoadCommand + PlayCommand across multiple layers)
- Enforces channel modes (on-air, preview, edit, debug)
- Routes commands to redundant engines (dual-write)
- Validates commands against plugin capabilities (don't send seek to a CEF plugin)
- Tracks foreground/background slot state per layer
- Manages transition defaults and overrides

**The bundling pattern:** The playout controller is where high-level operator actions become multiple engine commands. Examples:

| Operator Action | Engine Commands Generated |
|---|---|
| "Play lower third" | LoadCommand (BG) → PlayCommand (BG→FG with transition) |
| "Play audio with visualizer" | VideoLoadCommand (audio on layer 0) + LoadCommand (visualizer on layer 1, with audio_layer=0) + LoadCommand (lower-third on layer 2) + PlayCommand on all |
| "Play graphics package" | Multiple LoadCommands across layers + coordinated PlayCommands |
| "Update score" | UpdateCommand with data patch to view model |
| "Next page" | NextCommand to the target layer |

### NRCS / Rundown Manager

Manages the editorial workflow — rundowns, stories, and graphic items.

**Data model:**

```
Show
  └── Rundown
       ├── Segment ("Opening", "Headlines", "Interview")
       │    ├── Item (graphic, video, audio, still)
       │    │    ├── template reference
       │    │    ├── data fields (view model initial values)
       │    │    ├── transition settings (type, duration, direction)
       │    │    ├── channel + layer assignment
       │    │    ├── duration / auto-advance timer
       │    │    ├── data binding config (which fields are auto/gated/manual)
       │    │    └── CC required flag
       │    ├── Item ...
       │    └── Item ...
       ├── Segment ...
       └── Segment ...
```

**Features:**
- Create, edit, reorder rundown items
- Assign templates, data, transitions, channel/layer targets
- Import/export rundowns
- Rundown templates (reusable show structures)
- Multi-user collaboration (multiple operators editing the same rundown)
- Version history and undo
- Lock items that are on-air or cued

**MOS integration:** For facilities using external NRCS (ENPS, iNEWS, Octopus), the MOS Gateway syncs rundowns bidirectionally. Pivox's native NRCS is the preferred path — MOS is legacy support.

### Template Registry

Manages template lifecycle — upload, version, approve, deploy.

**Features:**
- Upload templates (HTML/JS/CSS bundles)
- Version control (semantic versioning, rollback)
- Approval workflow (designer → reviewer → approved for air)
- Template manifest validation (check SDK compliance, field declarations)
- Dependency tracking (which assets does this template reference?)
- Template categories and tagging
- Preview generation (thumbnail for browsing)

**Template states:**

```
Draft → In Review → Approved → Published → Deprecated
                 ↓
              Rejected (with feedback)
```

Only **Published** templates are available on production engines. Staging engines can load **Approved** or **Published**.

### Asset Manager

Manages all media assets — templates, video clips, audio files, images, fonts.

**Storage tiers:**

```
Hot (local SSD on engine machine)
  ↕ Asset Cache Manager handles movement
Nearline (NAS/SAN on facility network)
  ↕ Async transfer
Cold (cloud storage — S3/GCS/Azure Blob)
```

**Features:**
- Upload, organize, tag, search assets
- Folder/collection hierarchy
- Metadata management (technical metadata auto-extracted, editorial metadata user-entered)
- Format detection and validation
- Thumbnail and proxy generation
- Usage tracking (which templates/rundowns reference this asset?)
- Retention policies per tier
- Bulk operations (import, export, migrate)

### Asset Cache Manager

Ensures assets are on the engine machine's local SSD before they're needed. Described in detail in `docs/engine.md` — Asset Preloading and Caching section.

**Key behaviors:**
- Watches rundown state — knows what items are coming up
- Look-ahead window (configurable, e.g., 10 items)
- Pulls from nearline/cloud to local SSD
- LRU eviction with disk budget
- Pins assets that are in the active rundown
- Reports cache readiness to playout controller
- Adapts fetch priority based on: time to air, asset size, network bandwidth

### Data Plane Service

The Data Plane is Pivox's live data infrastructure — connecting external feeds to on-air templates with operator control, gating, throttling, schema versioning, and high-performance shared memory delivery.

See `docs/data-plane.md` for the full Data Plane architecture. Summary of control plane responsibilities:

- Connect to external data sources via pluggable connectors
- Normalize provider-specific data into versioned Pivox schemas
- Route data based on update mode per field (auto/gated/manual)
- Write to shared memory for high-frequency feeds (engine reads directly)
- Send UpdateCommand via gRPC for view model updates
- Operator controls: pause, resume, override, approve, throttle per field
- Maintain last-known-good values for failover
- In hybrid deployments, runs on the local control plane (data flows locally, no cloud hop)

### Timer Service

Frame-accurate automatic rundown advance. Uses the engine's frame counter (from genlock) as the authoritative clock, not wall-clock time.

**How it works:**
1. Timer service subscribes to engine's `WatchStatus` gRPC stream
2. Counts `frames_rendered` from `ChannelStatus`
3. At the target frame count: sends PlayCommand to playout controller
4. Playout controller executes the advance

**Timer modes:**

| Mode | Behavior |
|---|---|
| Fixed interval | Advance every N seconds (converted to frame count at channel frame rate) |
| Per-item duration | Each rundown item has its own duration |
| Timecode-triggered | Advance at specific SMPTE timecodes |
| Manual with countdown | Show countdown in operator UI, auto-advance at zero (operator can override/hold) |

**Operator controls:**
- Start/stop auto-advance per rundown
- Hold current item (pause timer without stopping the show)
- Skip to next item (advance immediately)
- Adjust remaining time on current item
- Override per-item duration before it airs

### Redundancy Coordinator

Manages hot-standby state replication between Engine A and Engine B. Described in detail in `docs/engine.md` — Redundancy section.

**Responsibilities:**
- Dual-write every command to both engines with ordering guarantees
- Monitor health of both engines via status streams
- Detect failure (missed health checks, frame drops, AJA card errors)
- Signal changeover switch (GPI or protocol) on failure
- Coordinate failback (manual — operator decides when to switch back to primary)
- State reconciliation after failover (ensure standby is in sync before promoting)

### Recording Manager

Manages compliance recording, asset ingest, and real-time content indexing. The engine's recording adapter writes to local SSD — the recording manager handles everything after that.

**Responsibilities:**
- Start/stop recording per channel (automatic when channel goes on-air)
- Segment recordings (per rundown item, per time interval, or continuous)
- Ingest recordings into Asset Manager as new assets
- Generate thumbnails and low-res proxy
- Real-time indexing during capture:
  - Template name and data fields on-air at each timecode
  - Rundown item metadata
  - AI-based content analysis (scene detection, OCR, speech-to-text) — optional, resource-dependent
- Transfer completed recordings to nearline/cloud storage
- Retention policy enforcement
- Search interface: "show me every time we displayed the election board"

### Monitoring Service

Health monitoring, metrics collection, and alerting for the entire Pivox system.

**What it monitors:**

| Source | Metrics |
|---|---|
| Engine channels | Frames rendered, frames dropped, layer states, genlock lock, GPI state |
| Engine plugins | Plugin state (IDLE/LOADING/READY/PLAYING/ERROR), restart count |
| AJA cards | Signal present, output format, genlock locked, temperature |
| Asset cache | Cache hit/miss rate, disk usage, pending downloads |
| Data feeds | Connection status, update rate, last received timestamp |
| Redundancy | Engine A/B sync state, last command replicated, divergence detected |
| Recording | Recording active, disk space remaining, ingest queue depth |
| Hardware (via automation gateway) | Router state, mixer tally, multiviewer status |

**Alerting:**
- Frame drop rate exceeds threshold → alert
- Engine process crash → alert + auto-restart
- Genlock lost → critical alert
- Data feed disconnected → warning
- Cache disk >80% full → warning
- Redundancy engines diverged → critical alert
- AJA card error → critical alert

Integration with standard monitoring infrastructure: Prometheus metrics endpoint, Grafana dashboards, alerting via PagerDuty/Slack/email.

## Integration Gateways

### MOS Gateway (Legacy NRCS Integration)

MOS (Media Object Server) protocol for integration with external newsroom systems (ENPS, iNEWS, Octopus).

- Receives rundown updates from NRCS
- Syncs items bidirectionally
- Translates MOS commands to Pivox rundown operations
- Legacy support — Pivox's native NRCS is the preferred path

**Strategic goal:** Define a modern Pivox NRCS integration protocol (gRPC/protobuf, real-time, bidirectional streaming) to replace MOS. MOS is XML-over-TCP from the early 2000s — brittle, slow, and poorly defined for real-time use cases. The new protocol would be published as an open spec for NRCS vendors to implement.

### VDCP Gateway (Automation Integration)

VDCP (Video Disk Control Protocol) for integration with broadcast automation systems.

- Receives play/stop/cue commands from automation
- Reports clip status back to automation
- Serial (RS-422) or TCP transport
- Used primarily in automated playout environments (e.g., commercial breaks, overnight programming)

### TSL UMD Gateway (Tally)

Receives tally signals from vision mixers via TSL UMD (Television Systems Ltd, Universal Monitor Driver) protocol.

- Tracks which Pivox channels are on program (red tally) or preview (green tally)
- Updates operator UI with tally state
- Surfaces tally in `pivox.system.channel.tally` for templates
- Can trigger automated actions (e.g., auto-play a graphic when channel goes on-air via mixer)

### Hardware Automation Gateway

Controls and monitors broadcast facility hardware. Each hardware type has a protocol adapter:

| Hardware | Protocol | Capabilities |
|---|---|---|
| Video router (Evertz, Grass Valley, Blackmagic) | SW-P-08, Ember+, Blackmagic Videohub API | Route sources to destinations, read crosspoint state |
| Vision mixer (Grass Valley, Sony, Ross) | Ember+, Ross OpenGear, proprietary | Read crosspoint, trigger transitions, macro execution |
| Audio mixer (Calrec, Lawo, Studer) | Ember+, NMOS | Level automation, channel routing, fader control |
| Multiviewer (Evertz, TAG, Densitron) | SNMP, proprietary | UMD labels ("PIVOX CH1 — ON AIR"), layout control |
| Master control switcher | Automation protocol (varies) | Playout automation, scheduled transitions |
| Record/ingest servers | VDCP, REST | Trigger recording on external devices |

**Architecture:** Each protocol adapter is a Go service (or goroutine) that:
1. Maintains a persistent connection to the hardware
2. Exposes a normalized control API to the playout controller
3. Publishes state changes to the internal message bus
4. Handles reconnection and error recovery

Hardware configuration (IP addresses, port mappings, protocol versions) is managed via the control plane's configuration system.

### Data Feed Connectors

See [Data Plane Service](#data-plane-service) above and `docs/data-plane.md`. Each external data source has a connector that:
- Connects via the source's protocol (REST, WebSocket, TCP)
- Normalizes data to Pivox's field schema
- Handles reconnection, retry, and failover
- Reports connection health to monitoring service

## Operator UI

### Design Principles

- **Browser-first** — full functionality in any modern browser (Chrome, Firefox, Safari, Edge)
- **Electron for desktop** — wraps the same web UI with native OS integration (notifications, global shortcuts, file system access)
- **Multi-monitor** — operators typically have 2-4 monitors. The UI supports detaching panels into separate windows.
- **Multi-user** — multiple operators can work simultaneously on different parts of the system (one on rundown, one on data feeds, one monitoring channels)
- **Role-based** — different views for different roles (producer, graphics operator, technical director, engineer)

### Key Screens

**Rundown Editor:**
```
┌─ Show: Evening News ─── Rundown: 2026-03-20 ────────────────────┐
│                                                                    │
│  #   Item                    Template          CH  Status    Dur   │
│  ──────────────────────────────────────────────────────────────── │
│  1   Opening titles          show-open          1  ▶ ON AIR  :15  │
│  2   Headlines               headline-stack     1  ● CUED    :30  │
│  3   Story: Election         election-board     2  ○ READY   ---  │
│  4   Phone: Sen. Smith       audio-interview    1  ◌ LOADING ---  │
│  5   Weather                 weather-map        1  ◌ QUEUED  :45  │
│  ...                                                               │
│                                                                    │
│  [▶ Play Next]  [⏸ Hold]  [⏭ Skip]  [Auto: ON ● 10s]            │
└────────────────────────────────────────────────────────────────────┘
```

**Channel Monitor:**
```
┌─ Channel 1 ──────────────────┐  ┌─ Channel 2 ──────────────────┐
│  ┌────────────────────────┐  │  │  ┌────────────────────────┐  │
│  │                        │  │  │  │                        │  │
│  │   MJPEG Preview        │  │  │  │   MJPEG Preview        │  │
│  │                        │  │  │  │                        │  │
│  └────────────────────────┘  │  │  └────────────────────────┘  │
│  Mode: ON-AIR  🔴            │  │  Mode: PREVIEW               │
│  TC: 01:23:45:12             │  │  TC: 01:23:45:12             │
│  Layers:                     │  │  Layers:                     │
│   L0: clip.mxf [PLAYING]     │  │   L0: (empty)                │
│   L1: lower-third [PLAYING]  │  │   L1: election-board [LOADED]│
│   L2: bug [PLAYING]          │  │   L2: (empty)                │
│  CC: ✓ CEA-708               │  │  CC: —                       │
│  Audio: ████████░░ -6dB      │  │  Audio: ░░░░░░░░░░ silent    │
└──────────────────────────────┘  └──────────────────────────────┘
```

**Live Data Monitor:**
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

**Transition Selector:**
```
┌─ Transition ─────────────────────────────────┐
│                                               │
│  Type: [Mix/Dissolve ▼]                      │
│  Duration: [20 frames ▼]  (0.67 sec)         │
│  Direction: [— ▼]                            │
│                                               │
│  Library:                                     │
│  ○ Cut                                        │
│  ● Mix / Dissolve                             │
│  ○ Push Left                                  │
│  ○ Push Right                                 │
│  ○ Wipe Edge                                  │
│  ○ Wipe Box                                   │
│  ○ Custom: "brand_swipe"                      │
│  ○ Custom: "glitch_transition"                │
│                                               │
│  [Preview]  [Apply to Item]  [Set as Default] │
└───────────────────────────────────────────────┘
```

**Hardware Status Dashboard:**
```
┌─ Facility Hardware ──────────────────────────────────────┐
│                                                           │
│  Engine A (Primary)    ✓ Healthy   4/4 channels          │
│  Engine B (Standby)    ✓ Healthy   4/4 channels          │
│  Redundancy            ✓ In sync   0ms lag               │
│                                                           │
│  AJA Corvid 88 (A)     ✓ Genlock locked  8/8 SDI out    │
│  AJA Corvid 88 (B)     ✓ Genlock locked  8/8 SDI out    │
│                                                           │
│  Video Router          ✓ Connected  Evertz EQX           │
│  Vision Mixer          ✓ Connected  GV Kayenne           │
│    Tally: CH1=PGM CH2=PVW CH3=— CH4=—                   │
│  Audio Mixer           ✓ Connected  Calrec Brio          │
│  Multiviewer           ✓ Connected  Evertz VIP-X         │
│                                                           │
│  Data Feeds:                                              │
│    AP Elections         ✓ Connected  last: 2s ago        │
│    Opta Sports          ✓ Connected  last: 1s ago        │
│    Weather Service      ⚠ Reconnecting (3 attempts)      │
│                                                           │
│  Recording:                                               │
│    CH1: ● Recording  01:23:45  disk: 234GB free          │
│    CH2: ● Recording  01:23:45  disk: 234GB free          │
│    CH3: ○ Idle                                            │
│    CH4: ○ Idle                                            │
└───────────────────────────────────────────────────────────┘
```

## User Roles and Permissions

| Role | Access |
|---|---|
| **Producer** | Full rundown control, data approval, show timing |
| **Graphics Operator** | Play/stop/update graphics, template data entry, transition selection |
| **Technical Director** | Channel modes, hardware monitoring, redundancy control |
| **Engineer** | Full system configuration, hardware setup, template management, user management |
| **Designer** | Template upload, template editor, preview (no on-air control) |
| **Viewer** | Read-only monitoring, channel preview |

Role-based access control (RBAC) with configurable permissions per role.

## Configuration Management

All system configuration is stored in the database and exposed via the API:

| Config Area | Examples |
|---|---|
| Channels | Channel count, output format (1080p59.94, etc.), output mapping |
| Engine | Engine machine addresses, gRPC endpoints |
| Hardware | Router/mixer/multiviewer IP addresses, protocols, port mappings |
| GPI mapping | Which AJA GPI pin → which command |
| Data feeds | Feed URLs, credentials, polling intervals |
| Templates | Default transitions, template search paths |
| Recording | Auto-record on air, segment duration, retention policies, storage paths |
| Redundancy | Primary/standby engine assignment, changeover switch config |
| Timers | Default auto-advance intervals, countdown behavior |
| Cache | Disk budget, look-ahead window size, eviction policy |

Configuration changes take effect immediately for most settings (no restart required). Engine-level changes (channel count, output format) require a channel restart.

## Deployment

See `docs/architecture.md` for the full deployment architecture including deployment tiers (Cloud, Hybrid, On-Prem), storage configuration, offline operation, and the local control plane design.

### Control Plane Modes

The control plane is a **single Go codebase** that runs in two modes:

**Cloud mode:**
- Source of truth for configuration, users, orgs
- Serves the web UI
- Manages cloud storage (S3/GCS)
- Database: cloud PostgreSQL (managed)
- Does NOT connect to engines directly — local control plane instances do

**Local mode (on-prem):**
- Runs on-prem alongside the engine
- Syncs bidirectionally with cloud instance
- Manages local engine(s) via gRPC over UDS
- Manages local storage (rustfs — S3-compatible)
- Runs data feed relays (connects to feeds directly, no cloud hop)
- Handles recording upload to configured storage
- **Operates independently during internet outages** — cached rundown, local state, local API fallback
- Database: embedded SQLite or local PostgreSQL

Same binary, different configuration. The local instance is not a dumb proxy — it's a full control plane instance that happens to sync state with the cloud.

### Installation (On-Prem)

Customer downloads and runs an install script — same model as GitHub Actions self-hosted runners:

1. Customer runs install script on the on-prem machine
2. Script installs the Pivox local control plane + engine binaries
3. Registers with the cloud backend (configures auth, org association, mTLS certificates)
4. Local control plane establishes outbound connection to cloud (firewall-friendly — no inbound ports needed)
5. Cloud pushes initial configuration, rundowns, and asset metadata
6. Local control plane pulls assets from configured storage to local cache
7. System is operational

### Database

| Mode | Database | Notes |
|---|---|---|
| Cloud | PostgreSQL (managed — RDS, Cloud SQL, etc.) | Source of truth, replicated for HA |
| Local (small) | Embedded SQLite | Single engine, simple installations |
| Local (large) | Local PostgreSQL | Multiple engines, higher reliability needs |

### Scaling Considerations

| Dimension | Scaling Approach |
|---|---|
| More channels | Add engine machines, local control plane manages all of them |
| More operators | Web UI scales horizontally, WebSocket connections via Redis pub/sub |
| More data feeds | Add feed connector instances on local control plane |
| More hardware | Add protocol adapter instances |
| More storage | Scale object storage (rustfs cluster on-prem, or cloud S3) |
| More sites | Each site runs its own local control plane, all sync to same cloud instance |

The control plane is stateless (state lives in PostgreSQL/SQLite + Redis) and can be horizontally scaled behind a load balancer for the REST/WebSocket endpoints. The local control plane can also be run as active/passive or horizontally scaled for high-availability on-prem installations.

## Integration Protocol (Pivox Protocol — Future)

**Strategic goal:** Replace MOS with a modern integration protocol for NRCS and automation systems.

**Design principles:**
- gRPC/protobuf (not XML/TCP)
- Bidirectional streaming (not request/response polling)
- Real-time (sub-second, not seconds-to-minutes like MOS)
- Type-safe schemas (not freeform XML)
- Event-driven (push state changes, don't poll)
- Authentication and encryption (TLS, API keys)
- Versioned (backward-compatible schema evolution)

**Scope:**
- Rundown sync (create, update, reorder items)
- Item control (play, stop, update data, cue, clear)
- Status subscription (channel state, on-air items, tally)
- Asset management (upload, reference, query)
- Template management (list, query capabilities, field schemas)
- Data binding (push data updates, subscribe to feed state)

**This protocol would be published as an open spec** — allowing NRCS vendors (AP ENPS, Avid iNEWS, Octopus, others) and automation vendors to integrate directly without MOS as an intermediary. It would also serve as the API for custom integrations, third-party control surfaces, and Pivox's own mobile apps.

To be designed separately — requires input from potential NRCS integration partners and broadcast automation vendors.

## Integration Packs (Future)

**Strategic goal:** Pre-built, installable integration packs for common broadcast data domains — elections, sports, financial, weather. Each pack bundles a data provider connector, a Pivox-defined normalized feed schema, and ready-to-use templates.

**Key concept:** The pack defines a **normalized schema** per domain (e.g., `pivox.elections.v1`). Multiple data providers for the same domain (AP, Reuters, Edison) map to the same schema via different connectors. Templates bind to the schema, not the provider — customer can switch providers without changing templates.

**A pack contains:**
- Data provider connector(s) (customer provides their own API key)
- Normalized feed schema definition
- Pre-bound templates (ready to use out of the box)
- Sample rundown

**Revenue model:** Packs sold alongside Pivox licenses (included, à la carte, or seasonal). Customer pays their data provider separately for the data itself. Pivox sells the integration + structure + templates.

**Future ecosystem:** Published schema format enables third-party packs — data companies, template agencies, and broadcast facilities can build and distribute their own packs.

To be designed separately.

## Project Structure (Go)

```
pivox/
├── cmd/
│   ├── pivox-server/              # Main control plane server
│   ├── pivox-mos-gateway/         # MOS protocol bridge (separate process)
│   └── pivox-monitor/             # Monitoring/alerting service
│
├── internal/
│   ├── api/                       # gRPC + REST handlers
│   ├── playout/                   # Playout controller, state machine
│   ├── nrcs/                      # Rundown manager, show/segment/item model
│   ├── templates/                 # Template registry, versioning, approval
│   ├── assets/                    # Asset manager, storage tiers, metadata
│   ├── cache/                     # Asset cache manager, look-ahead, eviction
│   ├── dataplane/                 # Data Plane — feed routing, gating, throttling, shared memory
│   ├── timers/                    # Timer service, frame-accurate auto-advance
│   ├── redundancy/                # Dual-write coordinator, failover logic
│   ├── recording/                 # Recording manager, ingest, indexing
│   ├── monitoring/                # Health checks, metrics, alerting
│   ├── hardware/                  # Hardware automation gateway
│   │   ├── router/                # Video router adapters (SW-P-08, Ember+, etc.)
│   │   ├── mixer/                 # Vision mixer adapters
│   │   ├── audio/                 # Audio mixer adapters
│   │   ├── multiviewer/           # Multiviewer adapters
│   │   └── tally/                 # TSL UMD tally receiver
│   ├── mos/                       # MOS protocol implementation
│   ├── vdcp/                      # VDCP protocol implementation
│   ├── auth/                      # Authentication, RBAC, sessions
│   └── config/                    # Configuration management
│
├── pkg/
│   └── sdk/                       # Go client SDK for external integrations
│
├── proto/                         # Protobuf definitions (shared with engine)
│   ├── playout.proto
│   ├── channel.proto
│   ├── input.proto
│   ├── preview.proto
│   └── plugin.proto               # Plugin protocol definitions
│
├── web/
│   ├── operator/                  # Operator UI (React + TypeScript)
│   └── electron/                  # Electron shell
│
├── migrations/                    # PostgreSQL migrations
│
├── deployments/
│   ├── docker/                    # Dockerfiles
│   └── k8s/                       # Helm charts
│
└── configs/
    └── examples/                  # Example configuration files
```

## Development Phases (Control Plane)

Control plane development runs in parallel with engine phases from `docs/engine.md`.

### Phase 1 — Core Playout Control

- Playout controller with state machine
- Basic REST/gRPC API (play, stop, update, load, clear per channel/layer)
- Basic operator web UI (channel monitor, manual play/stop)
- Template registry (upload, list, load)
- Asset manager (upload, list, basic metadata)
- MJPEG preview proxy (authenticate + forward engine preview streams)
- Single-user, no auth

### Phase 2 — Rundown and Automation

- Rundown manager (create, edit, reorder items)
- Rundown-driven playout (play from rundown, advance items)
- Timer service (auto-advance)
- Asset cache manager (look-ahead preloading)
- Transition selector in UI
- Data Plane (manual + auto modes, shared memory feeds)
- Basic monitoring (channel health, frame drops)
- Multi-user with basic roles

### Phase 3 — Full Editorial Workflow

- Full NRCS with show/segment/item model
- Template approval workflow (draft → review → publish)
- Data binding with gated mode and operator controls
- Media browser and clip management
- WYSIWYG template editor (basic)
- Recording manager with compliance recording
- User authentication and RBAC

### Phase 4 — Integration and Automation

- MOS gateway
- VDCP gateway
- TSL UMD tally
- Hardware automation gateway (routers, mixers, multiviewers)
- Data feed connectors (AP, Opta, custom)
- Redundancy coordinator
- Recording ingest with real-time indexing

### Phase 5 — Production Hardening

- Full monitoring and alerting (Prometheus, Grafana, PagerDuty)
- Audit trail (who did what when)
- Configuration backup/restore
- Multi-site support (multiple facilities)
- Performance testing at scale
- Disaster recovery procedures
- WYSIWYG template editor (full)
- Mobile operator app (view-only monitoring)
