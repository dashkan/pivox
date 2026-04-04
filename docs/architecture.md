# Pivox System Architecture

## Overview

Pivox is a broadcast graphics and media playout platform deployed as a hybrid cloud/on-prem system. The architecture is designed around a single codebase that runs in multiple deployment modes — cloud, hybrid, and fully on-prem — with the same binary and different configuration.

**Architecture documents:**
- `docs/architecture.md` — this document. System-level architecture, deployment tiers, data flow, security.
- `docs/engine.md` — playout engine (Rust + C++). Rendering, compositing, video playback, SDI/NDI output.
- `docs/control-plane.md` — Cloud Controller + Playout Agent. NRCS, asset management, operator UI, hardware automation.

## Deployment Tiers

| Tier | Engine | Management | Storage | Target Customer |
|---|---|---|---|---|
| **Pivox Cloud** | Pivox-hosted (GPU cloud instances) | Cloud Controller | Pivox cloud (S3) | Small orgs, no on-prem hardware. Output delivered via NDI/SRT over private networking. |
| **Pivox Hybrid** | Customer on-prem | Cloud Controller + Playout Agent | Cloud and/or on-prem (configurable per org) | Mid-large facilities with internet connectivity |
| **Pivox On-Prem** | Customer on-prem | Playout Agent only (standalone) | On-prem only | Enterprises, air-gapped facilities, government |

All three tiers run the **same software**. The difference is configuration and where the data lives.

## Hybrid Architecture (Primary Deployment Model)

The hybrid model is the primary deployment target. Data lives in the cloud. On-prem, the Playout Agent and engine run on **separate machines** — the engine machine is dedicated to rendering with minimal overhead.

```
┌──────────────────────────────────────────────────────────────┐
│  PIVOX CLOUD                                                  │
│                                                               │
│  ┌────────────────────────────────────────────────────────┐  │
│  │  Cloud Controller (api.pivox.app)                       │  │
│  │                                                          │  │
│  │  - Source of truth: config, users, orgs                 │  │
│  │  - Web UI hosting                                       │  │
│  │  - User authentication (primary)                        │  │
│  │  - Org management, billing                              │  │
│  │  - Template registry (master)                           │  │
│  │  - Asset metadata (master)                              │  │
│  │  - Rundown storage (master)                             │  │
│  │  - Monitoring aggregation (all sites)                   │  │
│  └────────────────────────────────┬───────────────────────┘  │
│                                    │                          │
│  Cloud Database (PostgreSQL)       │                          │
│  Cloud Storage (S3 / GCS)          │                          │
│                                    │                          │
└────────────────────────────────────┼──────────────────────────┘
                                     │
                          Outbound connection (mTLS)
                          Bidirectional sync
                                     │
┌────────────────────────────────────┼──────────────────────────┐
│  CUSTOMER ON-PREM (per site)       │                          │
│                                    │                          │
│  ┌─────────────────────────────────┴──────────────────────┐  │
│  │  PLAYOUT AGENT (separate machine from engine)           │  │
│  │                                                          │  │
│  │  Cloud Controller Sync (bidi, queues during outages)    │  │
│  │  Core Services:                                          │  │
│  │    - Playout controller                                 │  │
│  │    - Rundown manager (synced copy)                      │  │
│  │    - Asset cache manager (look-ahead preload)           │  │
│  │    - Data Plane (feed connectors, routing, gating)      │  │
│  │    - Timer service                                      │  │
│  │    - Redundancy coordinator                             │  │
│  │    - Recording manager                                  │  │
│  │    - Hardware automation gateway                        │  │
│  │    - Monitoring (local + reports to cloud)              │  │
│  │  Offline Mode (local state cache, serves UI locally)    │  │
│  │  Database: SQLite (small) or local PostgreSQL (large)   │  │
│  └──────────────────────┬───────────────────────────────┘  │
│                          │ gRPC over TCP (facility LAN)     │
│                          │ (commands + feed data stream)    │
│  ┌──────────────────────┴───────────────────────────────┐  │
│  │  ENGINE MACHINE (dedicated broadcast hardware)        │  │
│  │                                                        │  │
│  │  Supervisor (Rust, ~20MB):                              │  │
│  │    - Manages channel/plugin processes                  │  │
│  │    - Shared memory writer (receives feed stream          │  │
│  │      from agent, writes to /dev/shm/ for templates)    │  │
│  │    - gRPC endpoint for agent commands                  │  │
│  │                                                        │  │
│  │  Channel Processes (CEF + FFmpeg + Rive plugins)       │  │
│  │  AJA card → SDI/ST2110 + NDI + MJPEG output            │  │
│  │  GPU (dedicated to rendering)                          │  │
│  └────────────────────────────────────────────────────────┘  │
│                                                               │
│  ┌────────────────────────────────────────────────────────┐  │
│  │  Local Storage — rustfs (S3-compatible)                 │  │
│  │  Assets, templates, compliance recordings               │  │
│  └────────────────────────────────────────────────────────┘  │
│                                                               │
│  ┌────────────────────────────────────────────────────────┐  │
│  │  Broadcast Hardware                                     │  │
│  │  Routers, mixers, multiviewers, audio desks             │  │
│  │  (managed by Playout Agent's hardware automation gw)    │  │
│  └────────────────────────────────────────────────────────┘  │
└───────────────────────────────────────────────────────────────┘
```

**Key design decision:** The Playout Agent runs on a separate server from the engine. The engine machine is 100% dedicated to rendering — only the supervisor (~20MB Rust process) and shared memory writer run on it. All Go services, PostgreSQL, Redis, web UI, and feed connectors run on the agent server. Communication between Playout Agent and engine is gRPC over the facility LAN.

## Cloud-Only Architecture

For small organizations without on-prem infrastructure. The engine runs on Pivox-hosted GPU cloud instances. Output is delivered to the customer's facility via private networking.

```
┌──────────────────────────────────────────────────────────────┐
│  PIVOX CLOUD                                                  │
│                                                               │
│  ┌────────────────────────────────────────────────────────┐  │
│  │  Cloud Controller                                       │  │
│  │  (same as hybrid, but manages cloud-hosted engines)     │  │
│  └─────────────────────────┬──────────────────────────────┘  │
│                             │ gRPC                            │
│  ┌─────────────────────────┴──────────────────────────────┐  │
│  │  Cloud-Hosted Engine (GPU instance — AWS g5, GCP A2)    │  │
│  │                                                          │  │
│  │  Same engine binary as on-prem                          │  │
│  │  No AJA card — NDI + SRT output only                    │  │
│  │  MJPEG preview for operator UI                          │  │
│  └──────────────────────┬─────────────────────────────────┘  │
│                          │ NDI / SRT output                   │
│  ┌──────────────────────┴─────────────────────────────────┐  │
│  │  Cloud Storage (S3 / GCS)                               │  │
│  └────────────────────────────────────────────────────────┘  │
└──────────────────────────┬───────────────────────────────────┘
                           │
              Private networking / VPN / dedicated link
              NDI (LAN-level latency) or SRT (WAN-tolerant)
                           │
┌──────────────────────────┴───────────────────────────────────┐
│  CUSTOMER FACILITY                                            │
│                                                               │
│  NDI/SRT receiver → SDI conversion → vision mixer             │
│  (Blackmagic Web Presenter, AJA Bridge, NDI receiver, etc.)  │
└───────────────────────────────────────────────────────────────┘
```

**Requirements for cloud-only:**
- Private networking between Pivox cloud and customer facility (AWS Direct Connect, GCP Interconnect, VPN, or dedicated link)
- Sufficient bandwidth: NDI full ~150 Mbps per 1080p60 stream, SRT ~10-40 Mbps per stream
- Customer provides SDI conversion hardware at their facility if needed
- No AJA card — engine outputs NDI and/or SRT only
- Latency depends on network path — typically 50-200ms (acceptable for graphics playout)

## On-Prem Architecture

For enterprises and air-gapped facilities with no cloud dependency. Everything runs locally.

```
┌───────────────────────────────────────────────────────────────┐
│  CUSTOMER ON-PREM (fully self-contained)                       │
│                                                                │
│  ┌─────────────────────────────────────────────────────────┐  │
│  │  Playout Agent (standalone mode, same binary)             │  │
│  │                                                           │  │
│  │  - All services run locally                              │  │
│  │  - No cloud sync (standalone mode)                       │  │
│  │  - Local PostgreSQL database                             │  │
│  │  - Local auth (LDAP, SAML, or built-in)                  │  │
│  │  - Serves web UI locally                                 │  │
│  │  - Self-contained — no internet required                 │  │
│  └──────────────────────┬──────────────────────────────────┘  │
│                          │ gRPC over TCP (LAN)                       │
│  ┌──────────────────────┴──────────────────────────────────┐  │
│  │  Playout Engine                                          │  │
│  └──────────────────────────────────────────────────────────┘  │
│                                                                │
│  ┌──────────────────────────────────────────────────────────┐ │
│  │  Local Storage — rustfs (S3-compatible)                   │ │
│  └──────────────────────────────────────────────────────────┘ │
│                                                                │
│  ┌──────────────────────────────────────────────────────────┐ │
│  │  Local PostgreSQL                                         │ │
│  └──────────────────────────────────────────────────────────┘ │
│                                                                │
│  ┌──────────────────────────────────────────────────────────┐ │
│  │  Broadcast Hardware                                       │ │
│  └──────────────────────────────────────────────────────────┘ │
└───────────────────────────────────────────────────────────────┘
```

Same binary as cloud and hybrid — configured for standalone mode. Updates are delivered as binary releases (manual download or internal artifact repository).

## Data Flow

### Where Data Lives

| Data | Cloud Mode | Hybrid Mode | On-Prem Mode |
|---|---|---|---|
| User accounts, orgs | Cloud DB | Cloud DB (source of truth) | Local DB |
| Configuration | Cloud DB | Cloud DB → synced to local | Local DB |
| Rundowns | Cloud DB | Cloud DB → synced to local | Local DB |
| Template registry | Cloud DB + cloud storage | Cloud → synced to local | Local DB + local storage |
| Asset metadata | Cloud DB | Cloud DB → synced to local | Local DB |
| Asset files | Cloud storage (S3) | Cloud and/or on-prem storage (configurable) | On-prem storage (rustfs) |
| Compliance recordings | Cloud storage | Local SSD → uploaded to configured storage | Local storage (rustfs) |
| Engine state | Engine (runtime only) | Engine (runtime only) | Engine (runtime only) |
| Monitoring metrics | Cloud | Local → aggregated to cloud | Local only |

### Storage Configuration

Each organization can configure one or more storage locations:

```yaml
# Example: hybrid org with on-prem primary, cloud archive
storage:
  locations:
    - name: "primary"
      type: "on-prem"
      engine: "rustfs"
      endpoint: "https://rustfs.internal:9000"
      bucket: "pivox-assets"
      role: "primary"         # new assets stored here

    - name: "archive"
      type: "cloud"
      engine: "s3"
      endpoint: "https://s3.us-east-1.amazonaws.com"
      bucket: "pivox-archive-acme"
      role: "archive"         # compliance recordings, cold assets

  cache:
    local_path: "/data/pivox/cache"
    disk_budget_gb: 500
    eviction_policy: "lru"
    pin_active_rundown: true
```

Asset metadata includes which storage location holds each file. The asset cache manager resolves the fastest path — on-prem storage is preferred (LAN speed) over cloud storage (internet speed).

### Asset Flow (Hybrid)

```
Designer uploads template
  │
  ▼
Cloud Controller → cloud storage (S3) or on-prem storage (rustfs)
  │
  ▼
Asset metadata synced to Playout Agent
  │
  ▼
Asset cache manager (local) pulls asset to local SSD cache
  (prefers on-prem storage if available — LAN speed)
  (falls back to cloud storage — internet speed)
  │
  ▼
Engine loads from local SSD cache
```

### Live Data Flow (Hybrid)

```
Cloud Controller configures: "connect to AP Elections at ws://feeds.ap.org/..."
  │
  ▼
Playout Agent connects to feed DIRECTLY (no cloud hop)
  │
  ▼
Data Plane routes to engine (shared memory + gRPC over TCP (LAN))
  │
  ▼
Engine patches template view model → on-air update
```

Live data never traverses the cloud. The Cloud Controller configures which feeds to use; the Playout Agent connects and routes locally.

### Compliance Recording Flow (Hybrid)

```
Engine records to local SSD (real-time)
  │
  ▼
Playout Agent:
  ├── Ingests into asset manager (metadata, thumbnails, indexing)
  ├── Uploads to configured storage:
  │   ├── On-prem rustfs (if configured as primary)
  │   └── Cloud S3 (if configured as archive)
  └── Queues uploads during outages, resumes on reconnect
```

## Sync and Offline Operation

### Cloud Controller ↔ Playout Agent Sync

The Playout Agent maintains a persistent outbound bidi gRPC connection to the Cloud Controller (mTLS). Sync is bidirectional:

**Cloud Controller → Playout Agent:**
- Configuration changes
- Rundown updates (create, edit, reorder)
- Template registry changes (new versions, approvals)
- Asset metadata updates
- User/permission changes

**Playout Agent → Cloud Controller:**
- Engine status and health
- Channel on-air state
- Recording metadata
- Monitoring metrics
- Audit trail (who did what when)

Sync is event-driven (pushed on change), not polled.

### Offline Operation

When the internet connection drops, the Playout Agent switches to offline mode:

**What continues working:**
- Engine rendering (unaffected — engine doesn't depend on cloud)
- All on-air graphics and video playback
- Operator commands (play, stop, update) via local API
- Live data feeds (connected directly, not via cloud)
- GPI triggers and tally
- Compliance recording
- Timer / auto-advance
- Hardware automation (all local)
- Asset cache (already on local SSD)

**What stops working:**
- New rundown creation/editing (if not cached locally — TBD: should local allow full editing?)
- New asset uploads (cloud storage unreachable — unless on-prem storage is configured)
- Template approval workflow (requires Cloud Controller)
- User authentication for new sessions (cached tokens continue working)
- Multi-site monitoring aggregation
- Cloud Controller config pushes

**What the operator sees:**
- Banner: "OFFLINE MODE — operating from local cache"
- All on-air functions work normally
- Editing functions may be limited (TBD: define exactly which features are disabled)

**On reconnect:**
- Playout Agent reconciles state with Cloud Controller
- Queued status updates and audit logs are pushed
- Queued recording uploads resume
- Any config changes made in cloud during outage are pulled
- Conflict resolution: cloud wins for config/rundowns, local wins for on-air state

### Offline Mode — TBD

The exact feature set available during offline operation needs deeper analysis:

- Which editing operations should work offline? (edit existing rundown items? create new ones?)
- Should the Playout Agent serve the web UI directly during outage?
- How long can offline mode sustain? (hours? days?)
- What happens if both engines fail during offline mode? (no cloud to coordinate recovery)
- Should there be a "emergency offline kit" — pre-cached set of essential templates + rundowns that's always available regardless of cache state?

## Security

### Trust Boundaries

```
┌─ Cloud ─────────────────────────────────────────────┐
│  Cloud Controller (trusted)                           │
│  Cloud storage (trusted)                             │
│  Cloud database (trusted)                            │
└──────────────────────┬──────────────────────────────┘
                       │ mTLS (mutual TLS)
                       │ Certificate pinning
                       │ Outbound from on-prem (firewall-friendly)
┌──────────────────────┴──────────────────────────────┐
│  Playout Agent (trusted — same binary)                │
│  └── gRPC over TCP (LAN) to engine (trusted)              │
│  └── Local storage (trusted)                         │
└─────────────────────────────────────────────────────┘
```

### Authentication

| Context | Method |
|---|---|
| Operator → Cloud UI | OAuth2 / OIDC (see docs/authn.md for auth architecture) |
| Operator → Local UI (online) | Same — proxied through cloud auth |
| Operator → Local UI (offline) | Cached auth tokens with local validation |
| Playout Agent → Cloud Controller | mTLS with org-scoped certificates |
| Playout Agent → Engine | gRPC over TCP (LAN) (process-level trust, no auth needed) |
| External integrations → API | API keys + TLS |

### On-Prem Credential Security

The Playout Agent stores:
- mTLS certificates for cloud connection
- Storage credentials (rustfs, S3)
- Data feed credentials
- Cached auth tokens

These are stored encrypted at rest. The install script provisions a machine-specific encryption key derived from hardware identity (TPM where available).

### Network Requirements

**Hybrid deployment — outbound connections only (no inbound ports needed):**

| Connection | Direction | Protocol | Ports |
|---|---|---|---|
| Playout Agent → Cloud Controller | Outbound | gRPC over TLS | 443 |
| Playout Agent → Cloud storage | Outbound | HTTPS (S3 API) | 443 |
| Playout Agent → Data feeds | Outbound | WebSocket/HTTPS | Varies |
| Operator → Cloud UI | Outbound | HTTPS | 443 |
| Playout Agent → Engine | Local | gRPC over TCP (LAN) | N/A (file socket) |
| Engine → AJA card | Local | PCIe | N/A |
| Engine → NDI | Local LAN | mDNS + TCP/UDP | 5353 + dynamic |

No inbound firewall rules needed at the customer site. All connections are outbound — same model as GitHub Actions runners, Tailscale, Cloudflare Tunnel.

## Installation

### On-Prem Installation (Hybrid or Full On-Prem)

Same model as GitHub Actions self-hosted runners:

```
1. Customer logs into Pivox cloud UI
2. Navigates to Org Settings → Sites → Add Site
3. Gets an install command with a registration token:

   curl -sSL https://install.pivox.io | bash -s -- \
     --token <registration-token> \
     --site-name "NYC Studio A"

4. Script installs:
   - Pivox Playout Agent (Go binary)
   - Pivox engine (Rust + C++ binary + CEF + FFmpeg libs)
   - rustfs (if on-prem storage selected)
   - System service definitions (systemd / Windows Service)
   - AJA NTV2 drivers (if AJA card detected)

5. Playout Agent registers with Cloud Controller:
   - Exchanges registration token for mTLS certificates
   - Associates with org and site
   - Receives initial configuration

6. Cloud Controller pushes:
   - Channel configuration
   - Rundowns and template metadata
   - Asset metadata (actual files pulled on-demand by cache manager)

7. System is operational
```

### Updates

| Deployment | Update Method |
|---|---|
| Cloud | Continuous deployment (Pivox manages) |
| Local (hybrid) | Pulled from cloud, operator approves and schedules update window |
| Local (on-prem) | Manual binary update from release artifacts, or internal artifact repository |

Engine updates require channel restart (brief outage). Playout Agent updates can be rolling (zero downtime) in HA configurations.

## Multi-Site

Large organizations may have multiple broadcast sites (NYC studio, LA studio, DC bureau, etc.). Each site runs its own Playout Agent and engines. All sites sync to the same Cloud Controller.

```
Cloud Controller (api.pivox.app)
  │
  ├── NYC Site (Playout Agent + engines)
  ├── LA Site (Playout Agent + engines)
  └── DC Site (Playout Agent + engines)
```

**Cross-site capabilities:**
- Shared template library (published templates available at all sites)
- Shared asset library (assets pulled to each site's cache on demand)
- Centralized monitoring (all sites visible in cloud dashboard)
- Per-site configuration (different channel counts, output formats, hardware)
- Per-site user permissions (NYC operators can't control LA channels)

**Cross-site playout:** A future capability — controlling engines at one site from another site's operator UI. Requires careful latency analysis and is not day-one scope.

## Disaster Recovery

### Engine Failure

Handled by hot-standby redundancy. See `docs/engine.md` — Redundancy section.

### Playout Agent Failure

- Auto-restart via system service (systemd / Windows Service)
- Engine continues rendering whatever is currently on-air
- Operator loses UI control until agent restarts (~5-10 seconds)
- For HA: active/passive or horizontally scaled Playout Agent

### Cloud Controller Failure

- All local sites continue operating independently (offline mode)
- No new rundown editing, template publishing, or user management
- On-air operations unaffected
- Resume normal operation on cloud recovery

### Site Failure (Total)

- If redundant site exists: failover at the facility level (router/mixer switches to backup feed)
- If no redundant site: this is a facility-level disaster, not a software problem
- Compliance recordings on local SSD may need recovery from backup storage

### Data Recovery

| Data | Recovery Source |
|---|---|
| Configuration | Cloud database (source of truth) |
| Rundowns | Cloud database |
| Templates | Cloud storage + cloud database |
| Assets | Cloud storage or on-prem rustfs (depending on config) |
| Compliance recordings | Cloud/nearline storage (uploaded async from local) |
| Engine state | Reconstructed from rundown + commands (engine is stateless between restarts) |
