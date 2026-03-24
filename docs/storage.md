# Pivox Storage Architecture

## Overview

This document defines how Pivox stores, accesses, and distributes media assets (templates, images, video clips, fonts, compliance recordings) across cloud and on-prem deployments. The storage layer supports two backend types: S3-compatible object stores (AWS S3, rustfs, etc.) and local/network-mounted filesystems (NFS, CIFS). Both are managed through Storage Gateways — on-prem agents that proxy, cache, and serve assets to browsers and Electron on the local network.

**Key design decisions:**

1. **Cookie-based auth for reads, presigned URLs for uploads.** Asset reads go through Storage Gateways with session cookie auth — stable URLs enable CDN-style caching. Uploads use S3 presigned URLs for direct-to-storage writes. No component except the control plane holds long-lived storage credentials.

2. **Project-scoped storage.** Every asset belongs to a project (org + workspace). Storage paths are partitioned by project. No cross-project access without explicit sharing.

3. **Storage Gateways for on-prem.** A lightweight agent binary that acts as an S3 reverse proxy + cache. Installed by running a single script, scaled by running the same script on additional servers. Managed entirely from the Pivox Cloud UI — no manual YAML config, no credential distribution. Browsers and Electron hit the gateway directly on the LAN for fast, local asset access.

4. **Cloud is source of truth.** Asset metadata lives in Pivox Cloud (PostgreSQL). Gateways cache assets locally and continue serving during cloud outages. All data syncs back when connectivity restores.

**Related documents:**
- `docs/architecture.md` — system-level architecture, deployment tiers
- `docs/engine.md` — engine asset preloading, `LoadCommand` with local paths
- `docs/control-plane.md` — asset manager, asset cache manager
- `docs/sdk.md` — `pivox.assets.resolve()`, `pivox.assets.preload()`

---

## Storage Gateway

### What It Is

A Storage Gateway is an on-prem agent cluster that serves assets directly to browsers and Electron on the local network. It is:

- **A storage proxy** — proxies requests to S3-compatible backends or mounted filesystems on the local network
- **A caching layer** — caches assets on local disk for fast, repeated access
- **HTTPS with public TLS** — browsers trust it natively (Let's Encrypt certs, no self-signed CA)
- **Managed from the cloud** — configuration, credentials, certs, and upgrades are all delivered from Pivox Cloud via a persistent bidi gRPC connection
- **Offline-resilient** — keeps serving from cached config + local endpoints when cloud is unreachable

### Resource Hierarchy

```
Organization
└── StorageGateway                    (on-prem agent cluster)
    ├── Agent                         (individual server in the pool)
    └── Endpoint                      (S3-compatible bucket)
```

- **StorageGateway** — the logical cluster. Has a hostname (`{name}.storage.pivox.app`), registration token, cache config, TLS cert status.
- **Agent** — a single server running the agent binary. Created automatically when the server connects. Multiple agents behind DNS round-robin for load balancing.
- **Endpoint** — a storage backend accessible through the gateway. Either an S3-compatible bucket or a mounted filesystem path. Configuration (including credentials for S3) stored server-side, never exposed via API, delivered to agents over the bidi stream.

### Architecture

```
┌─ PIVOX CLOUD ──────────────────────────────────────────────────────┐
│                                                                      │
│  Control Plane (api.pivox.app)                                       │
│    - Asset metadata (PostgreSQL)                                    │
│    - StorageGateway / Agent / Endpoint resource management          │
│    - DNS zone management (*.storage.pivox.app)                      │
│    - Let's Encrypt ACME (DNS-01 challenge)                          │
│    - Upgrade orchestration                                          │
│    - Bidi gRPC endpoint for agent connections                       │
│                                                                      │
└──────────────────────┬─────────────────────────────────────────────┘
                       │ bidi gRPC (agent-initiated, outbound)
                       │
┌──────────────────────┴─────────────────────────────────────────────┐
│  CUSTOMER ON-PREM                                                    │
│                                                                      │
│  ┌──────────────────────────────────────────────────────────────┐  │
│  │  Storage Gateway: west-coast.storage.pivox.app               │  │
│  │                                                                │  │
│  │  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐          │  │
│  │  │  Agent 1     │  │  Agent 2     │  │  Agent 3     │          │  │
│  │  │  10.0.1.10   │  │  10.0.1.11   │  │  10.0.1.12   │          │  │
│  │  │  ┌─────────┐ │  │  ┌─────────┐ │  │  ┌─────────┐ │          │  │
│  │  │  │  Cache   │ │  │  │  Cache   │ │  │  │  Cache   │ │          │  │
│  │  │  │  500GB   │ │  │  │  500GB   │ │  │  │  500GB   │ │          │  │
│  │  │  └─────────┘ │  │  └─────────┘ │  │  └─────────┘ │          │  │
│  │  └──────┬──────┘  └──────┬──────┘  └──────┬──────┘          │  │
│  │         │                │                │                   │  │
│  │         └────────────────┼────────────────┘                   │  │
│  │                          │                                     │  │
│  │                 ┌────────┴────────┐                            │  │
│  │                 │  S3 Endpoints    │                            │  │
│  │                 │                  │                            │  │
│  │                 │  ├── MinIO       │                            │  │
│  │                 │  │   :9000       │                            │  │
│  │                 │  │               │                            │  │
│  │                 │  └── rustfs      │                            │  │
│  │                 │      :9001       │                            │  │
│  │                 └─────────────────┘                            │  │
│  └──────────────────────────────────────────────────────────────┘  │
│                                                                      │
│  ┌──────────────────────────────────────────────────────────────┐  │
│  │  Consumers (on the same LAN)                                   │  │
│  │                                                                │  │
│  │  - Operator browsers → https://west-coast.storage.pivox.app   │  │
│  │  - Electron app → https://west-coast.storage.pivox.app        │  │
│  │  - Engine cache manager → same                                │  │
│  │  - Templates (pivox.assets.resolve()) → same                  │  │
│  └──────────────────────────────────────────────────────────────┘  │
│                                                                      │
└──────────────────────────────────────────────────────────────────────┘
```

---

## Deployment

### Creating a Storage Gateway

```
1. Admin creates StorageGateway in Pivox Cloud UI
   → Provides: display name, IP addresses of servers
   → Control plane creates DNS record: west-coast.storage.pivox.app
     (round-robin A records pointing to the provided IPs)
   → Returns: registration_token (shown once)
   → UI shows install command with configurable parameters

2. Admin configures install parameters in the UI:
   - Cache directory (default: /var/lib/pivox/cache)
   - Cache size in GB (default: auto-detect, 80% of available disk)
   - HTTPS port (default: 443)
   - Bind address (default: 0.0.0.0)
   - HTTP/HTTPS proxy (for reaching Pivox Cloud through corporate proxy)
   - No-proxy list (bypass proxy for local S3 endpoints)
   - Telemetry (default: enabled)

3. UI updates the install command live. Admin copies and runs:

   curl -sSL https://get.pivox.app/agent | bash -s -- \
     --token <registration_token> \
     --cache-dir /mnt/ssd/pivox/cache \
     --cache-size 500
```

### What the Install Script Does

The install script runs on a Linux server (v1 — Linux only):

1. Checks for root/sudo
2. Creates `pivox` user and group (no login shell, no home directory)
3. Downloads the agent binary to `/usr/local/bin/pivox-agent`
4. Verifies binary checksum
5. Writes configuration to `/etc/pivox/agent.yaml` (permissions: `0600 pivox:pivox`)
6. Creates cache directory at the specified path (permissions: `0750 pivox:pivox`)
7. Installs a hardened systemd unit file (`pivox-agent.service`):
   - `ProtectSystem=strict`
   - `PrivateTmp=true`
   - `NoNewPrivileges=true`
   - `ReadWritePaths=` only the cache directory and config
8. Enables and starts the service
9. Agent process dials Pivox Cloud with the registration token

### Directory Layout

```
/usr/local/bin/pivox-agent          # agent binary
/etc/pivox/agent.yaml               # configuration (token, params)
/var/lib/pivox/cache/               # asset cache (or custom path)
```

### Agent Configuration

Every parameter is available as both a CLI argument and an environment variable. CLI arguments take precedence over environment variables.

| Parameter | CLI Arg | Env Var | Default |
|---|---|---|---|
| Registration token | `--token` | `PIVOX_TOKEN` | (required) |
| Cache directory | `--cache-dir` | `PIVOX_CACHE_DIR` | `/var/lib/pivox/cache` |
| Cache size (GB) | `--cache-size` | `PIVOX_CACHE_SIZE` | auto (80% avail) |
| HTTPS port | `--port` | `PIVOX_PORT` | `443` |
| Bind address | `--bind` | `PIVOX_BIND` | `0.0.0.0` |
| HTTP proxy | `--http-proxy` | `HTTP_PROXY` | (none) |
| HTTPS proxy | `--https-proxy` | `HTTPS_PROXY` | (none) |
| No-proxy list | `--no-proxy` | `NO_PROXY` | (none) |
| Telemetry | `--telemetry` | `PIVOX_TELEMETRY` | `true` |

Parameters are persisted in the systemd unit file as `Environment=` directives. To change after install, edit with `systemctl edit pivox-agent` or re-run the install script.

### Adding Servers (Load Balancing)

Run the same install script on additional servers. There is no distinction between "install" and "join" — every server that connects with the same registration token joins the gateway pool.

```
# Server 2 — same command, different machine
curl -sSL https://get.pivox.app/agent | bash -s -- --token <same_token>

# Server 3
curl -sSL https://get.pivox.app/agent | bash -s -- --token <same_token>
```

The control plane:
- Creates a new Agent resource for each server
- Updates the DNS round-robin with all server IPs
- Pushes the same endpoint config and cache settings to all agents

### Uninstalling

An uninstall script is available from the control plane. It:
1. Stops the `pivox-agent` service
2. Sends a graceful disconnect over the bidi stream
3. Disables and removes the systemd unit
4. Removes the binary, configuration, and (optionally) the cache directory
5. Removes the `pivox` user and group

The control plane removes the Agent resource and updates DNS.

### Adding S3 Endpoints

Once the gateway is active (at least one agent connected), the admin adds endpoints via the UI. Two endpoint types are supported:

**S3-compatible endpoint:**
```
Endpoint: "primary"
  Configuration:
    type: S3
    endpoint_uri: http://localhost:9000
    bucket: pivox-assets
    credentials:
      access_key_id: (set via UI, never exposed in API responses)
      secret_access_key: (set via UI, never exposed in API responses)
```

S3 endpoints require bucket versioning to be enabled. The agent verifies this during endpoint creation and fails with a clear error if versioning is not enabled.

**Filesystem endpoint:**
```
Endpoint: "nfs-media"
  Configuration:
    type: filesystem
    path: /mnt/nfs/pivox-assets
```

Filesystem endpoints use a local or network-mounted path (NFS, CIFS, etc.). The path must be mounted on all agents in the gateway pool before adding the endpoint. During creation, the agent writes a marker file (`.pivox-endpoint-id`) and all other agents verify they can read it — confirming shared access to the same filesystem.

Assets on filesystem endpoints are stored as immutable versioned files:
```
{path}/{org_id}/{project_id}/assets/{asset_id}/v1.ext
```

No credentials are needed — the agent accesses files using the `pivox` system user's permissions on the mount.

The control plane pushes endpoint configuration to all connected agents via the bidi stream. Agents begin proxying and caching requests immediately.

---

## Bidi gRPC Agent Protocol

### Connection Lifecycle

The agent initiates an outbound gRPC connection to `api.pivox.app`. This persistent bidirectional stream is the sole management channel — no inbound firewall rules needed on the customer's network.

```
Agent starts → dials api.pivox.app → sends Handshake (token, version, IP, hostname)
  ↓
Control plane validates token → creates Agent resource → sends HandshakeAck
  (includes: TLS cert, endpoint configs, cache config)
  ↓
Steady state: agent sends heartbeats, telemetry; control plane sends config updates, cert renewals
  ↓
Disconnect: agent retries with exponential backoff, serves from cached config
```

### Agent → Control Plane Messages

| Message | When Sent | Gated by Telemetry |
|---|---|---|
| **Handshake** | First message after connecting | No |
| **Heartbeat** | Periodic (every 30s) | No |
| **EndpointHealth** | On health check interval per endpoint | No |
| **SyncStatus** | When offline writes are pending sync | No |
| **UpgradeStatus** | During upgrade phases | No |
| **Telemetry** | Periodic metrics envelope | Yes |

### Control Plane → Agent Messages

| Message | Purpose |
|---|---|
| **HandshakeAck** | Confirms registration, delivers initial config + TLS cert |
| **CertDelivery** | TLS cert renewal (before expiry) |
| **DrainRequest** | Prepare for maintenance or upgrade |
| **UpgradeRequest** | Download or apply new agent version |
| **ConfigUpdate** | Endpoint or cache config changes |
| **Heartbeat** | Keep-alive |

### Telemetry

Telemetry is opt-in by default (`--telemetry=true`). When enabled, the agent sends a `Telemetry` envelope containing one of:

- **CacheStats** — cache used GB, hit/miss/eviction counts
- **RequestMetrics** — total requests, bytes served, error count, latency percentiles (p50, p99)
- **SystemMetrics** — CPU%, memory, disk I/O, network I/O

When telemetry is disabled (`--telemetry=false`), these metrics are not sent. Heartbeat, endpoint health, sync status, and upgrade status always flow regardless of the telemetry setting — they are required for operational management.

---

## TLS Certificates

### Why Public Certs

The gateway serves HTTPS to browsers and Electron on the local network. Self-signed certificates or a private CA would cause browser trust errors. Instead, each gateway gets a publicly trusted certificate via Let's Encrypt.

### Certificate Flow

```
1. Gateway created → control plane creates DNS record:
   west-coast.storage.pivox.app → 10.0.1.10, 10.0.1.11

2. Control plane performs Let's Encrypt DNS-01 challenge:
   - Adds TXT record to _acme-challenge.west-coast.storage.pivox.app
   - Let's Encrypt validates domain ownership
   - Issues a publicly trusted certificate

3. Certificate delivered to agent(s) via bidi stream (HandshakeAck or CertDelivery)

4. Agent serves HTTPS with the cert — browsers trust it natively

5. Before expiry (every ~60 days): control plane renews via DNS-01,
   pushes new cert over bidi stream. Agent hot-swaps — zero downtime.
```

The agent never needs outbound internet access to ACME servers. The control plane handles all DNS and certificate operations. The agent just receives the cert over the existing bidi connection.

---

## Rolling Upgrades

### Overview

Agent upgrades follow a k8s-style rolling update pattern. The control plane orchestrates the entire process — download first, then sequential apply with health checks gating progression.

### Upgrade Flow

```
Phase 1: Prepare (all nodes, parallel)
  ─────────────────────────────────────────────
  Admin sets target_version on the gateway (or auto-triggered by new release)
  → UpgradeGateway API call returns a long-running operation (LRO)
  → Control plane sends UpgradeRequest{phase: DOWNLOAD} to ALL agents
  → Each agent:
    a. Downloads the new binary from the specified URL
    b. Verifies SHA-256 checksum
    c. Verifies Ed25519 signature (public key baked into agent binary)
    d. Reports UpgradeStatus{phase: READY}
  → Control plane waits until all agents report READY
  → If any agent fails download or verification → abort, LRO fails

Phase 2: Roll (sequential, one at a time)
  ─────────────────────────────────────────────
  For each agent in the pool:
    → Control plane sends DrainRequest → agent
    → Agent removes itself from DNS round-robin
    → Agent finishes in-flight requests
    → Agent reports UpgradeStatus{phase: DRAINED}
    → Control plane sends UpgradeRequest{phase: APPLY}
    → Agent replaces binary, restarts via systemd
    → New binary reconnects, sends Handshake with new version
    → Control plane health check:
      ├── Pass → re-add to DNS, move to next agent
      └── Fail → agent rolls back to old binary (kept as fallback),
                  halt rollout, remaining agents stay on old version,
                  gateway state → DEGRADED, admin alerted

Phase 3: Done
  ─────────────────────────────────────────────
  All agents on new version, all healthy → LRO completes
```

### Binary Security

| Verification | Purpose |
|---|---|
| **SHA-256 checksum** | Integrity — download wasn't corrupted |
| **Ed25519 signature** | Authenticity — binary was built by Pivox |

The Pivox Ed25519 public key is compiled into the agent binary at build time. Even if a download URL is compromised, the signature verification prevents deploying a malicious binary.

---

## Storage Model

### S3-Compatible Everywhere

Pivox supports two storage backend types:

| Type | Examples | Notes |
|---|---|---|
| **S3-compatible** | AWS S3, rustfs, GCS (S3-compat mode) | Object storage with bucket versioning. Credentials managed via API. |
| **Filesystem** | NFS, CIFS, local disk | Mounted path on the agent server. No credentials — uses OS file permissions. |

| Deployment | Typical Setup | Notes |
|---|---|---|
| **Cloud** | AWS S3 | Managed, multi-region capable |
| **On-Prem** | rustfs (S3) or NFS mount | Runs on dedicated storage servers |
| **Hybrid** | Cloud S3 + on-prem rustfs/NFS | Assets replicated between tiers |

The Storage Gateway proxies to either backend type transparently. Assets are immutable — each version is a new object (S3) or file (filesystem).

### Bucket Layout — Project-Scoped

Storage is partitioned by project. A project is scoped to an org and represents an isolated workspace (a show, a facility, a department).

```
pivox-storage-{region}/
  ├── {org_id}/
  │    ├── {project_id}/
  │    │    ├── templates/
  │    │    │    ├── {template_id}/{version}/
  │    │    │    │    ├── index.html
  │    │    │    │    ├── style.css
  │    │    │    │    ├── animation.js
  │    │    │    │    └── assets/
  │    │    │    └── ...
  │    │    ├── media/
  │    │    │    ├── images/{asset_id}.png
  │    │    │    ├── video/{asset_id}.mxf
  │    │    │    ├── audio/{asset_id}.wav
  │    │    │    └── fonts/{asset_id}.woff2
  │    │    ├── recordings/
  │    │    │    ├── {channel_id}/{date}/{recording_id}.mp4
  │    │    │    └── ...
  │    │    └── exports/
  │    │         └── ...
  │    │
  │    ├── {project_id_2}/
  │    │    └── ...
  │    │
  │    └── _shared/                    # org-wide shared assets
  │         ├── brand/                 # logos, fonts, brand kits
  │         ├── templates/             # org-wide template library
  │         └── media/                 # shared media library
  │
  └── {org_id_2}/
       └── ...
```

**Isolation guarantees:**
- Presigned URLs are scoped to a specific object path — a URL for `org_a/project_1/media/image.png` cannot access `org_b/` or even `org_a/project_2/`
- The backing IAM policy (cloud) or bucket policy (on-prem) enforces org-level isolation as a second layer
- The control plane validates project membership before signing any URL

### Asset Metadata

Asset metadata lives in PostgreSQL in Pivox Cloud, not in S3. The database record maps an asset ID to its storage location:

```
asset {
  id:              uuid
  org_id:          uuid
  project_id:      uuid
  name:            "logo-cnn-hd"
  type:            "image"
  mime_type:       "image/png"
  size_bytes:      245760
  storage_path:    "org_abc/proj_123/media/images/abc123.png"
  checksum_sha256: "e3b0c44298fc..."
  created_at:      timestamp
  updated_at:      timestamp
  uploaded_by:     uuid
  tags:            ["brand", "logo"]
}
```

Storage paths are server-generated. Users never see or control physical storage paths.

---

## Access Model

### Reads — Cookie-Based Auth via Gateway

Asset reads go through Storage Gateways using session cookie auth. URLs are stable and cacheable — no per-request signatures.

**Session setup flow:**

```
1. User authenticates with Pivox Cloud (Firebase ID token)

2. Browser calls CreateStorageSession RPC on the control plane:
   POST /v1/storageSession

3. Control plane:
   a. Verifies Firebase token, identifies user
   b. Computes access patterns from user's org/project memberships:
      ["/local-corp/local/primary/news/*",
       "/local-corp/local/primary/sports/*"]
   c. Generates opaque session token (UUID)
   d. Pushes SessionGrant { token, patterns, expiry } to all
      relevant gateways via bidi gRPC
   e. Each gateway stores token → patterns in memory
   f. Mints HS256 JWT containing { token, exp }
   g. Returns Set-Cookie: pivox_session=<jwt>;
      Domain=.pivox.app; Secure; HttpOnly; SameSite=Lax

4. All subsequent requests to any gateway under *.storage.pivox.app
   include the cookie automatically.
```

**Asset read flow:**

```
Browser: <img src="https://west-coast.storage.pivox.app/org/gw/ep/proj/assets/logo.png">

Gateway HTTP server:
  1. Reads pivox_session cookie
  2. Validates JWT signature (HMAC key from HandshakeAck)
  3. Extracts opaque token from JWT
  4. Looks up token in memory → gets access patterns
  5. Glob-matches request path against patterns
  6. Match → serve from cache or proxy to origin
  7. No match → 403 Forbidden

No per-request control plane call. No presigned URL.
Cache key is the clean URL — high cache hit rate.
```

**Session lifecycle:**

| Event | Behavior |
|---|---|
| **Session created** | Control plane pushes SessionGrant to gateways via bidi |
| **Role changed** | Control plane pushes updated SessionGrant (same token, new patterns) |
| **Access revoked** | Control plane pushes SessionRevoke — immediate, next request is 403 |
| **Session expired** | Gateway flushes token from memory. Browser gets 401, calls CreateStorageSession again. |
| **Gateway offline** | Sessions in memory persist. New sessions can't be pushed until reconnect. |

### Uploads — Presigned URLs

Uploads bypass the gateway and go directly to the S3 endpoint via presigned URLs. The control plane generates short-lived, path-scoped URLs — no credentials are distributed to clients.

| Operation | HTTP Method | TTL | Use Case |
|---|---|---|---|
| **Upload** | PUT | 15 minutes | Designer uploading templates, operator uploading media |
| **Multipart upload** | POST + PUT | 24 hours | Large file uploads (video >100MB) |

Filesystem endpoints use a different upload path — the client PUTs to the gateway's HTTP server with a signed upload token (not a presigned S3 URL).

### Upload Flow

Asset uploads use presigned PUT URLs. The client uploads directly to storage.

```
1. Client: InitiateUpload RPC
   { "name": "interview-bg.png", "type": "image", "project_id": "..." }

2. Control plane:
   a. Validates permissions
   b. Creates asset record in DB (state: PENDING_UPLOAD)
   c. Generates storage path: org_abc/proj_123/media/images/{id}.png
   d. Signs presigned PUT URL
   e. Returns: { "asset_id": "...", "upload_url": "https://...", "expires_at": "..." }

3. Client: PUT {upload_url} with raw file bytes

4. Client: ConfirmUpload RPC
   { "asset_id": "...", "checksum_sha256": "e3b0c44..." }

5. Control plane:
   a. Verifies object exists in storage (HEAD request)
   b. Verifies checksum matches
   c. Extracts technical metadata (dimensions, duration, codec)
   d. Generates thumbnail
   e. Updates asset state: PENDING_UPLOAD → ACTIVE
```

### Multipart Upload (Large Files)

For files >100MB, the CP orchestrates an S3 multipart upload:

```
1. Client: InitiateMultipartUpload
   { "name": "game-replay.mxf", "size_bytes": 2147483648 }

2. CP: Returns upload_id + signed part URLs
   {
     "upload_id": "mp_abc123",
     "parts": [
       { "part_number": 1, "url": "https://s3...?partNumber=1&uploadId=..." },
       { "part_number": 2, "url": "https://s3...?partNumber=2&uploadId=..." }
     ]
   }

3. Client: PUT each part URL (can be parallel) → returns ETag per part

4. Client: CompleteMultipartUpload
   { "upload_id": "...", "parts": [{ "part_number": 1, "etag": "..." }, ...] }

5. CP: Completes multipart on S3, verifies, extracts metadata
```

---

## Caching

### Gateway Cache

Each agent in the gateway pool maintains a local disk cache. Cache configuration is set on the StorageGateway resource and shared by all agents.

| Setting | Description | Default |
|---|---|---|
| **max_size_gb** | Maximum cache disk usage | Auto (80% available) |
| **eviction_policy** | LRU or LFU | LRU |
| **ttl_hours** | Max age before re-fetch from origin | No TTL (immutable assets) |

### Cache Behavior

| Aspect | Behavior |
|---|---|
| **Cache key** | Storage path (org/project/type/file) — ignoring query parameters |
| **Eviction** | LRU/LFU with configurable disk budget |
| **Integrity** | SHA-256 checksum verified on cache write and periodic read-back |
| **Warm-up** | Asset cache manager pre-warms by pre-fetching look-ahead assets |
| **Invalidation** | Control plane pushes cache invalidation via bidi stream |
| **Offline** | During cloud outage, cache serves previously-fetched assets indefinitely |

### Cache vs. Engine SSD Cache

There are two caches in the system:

| | Gateway Cache | Engine SSD Cache |
|---|---|---|
| **Where** | Gateway server disk | Engine machine SSD |
| **What** | Raw asset files (PNG, MXF, HTML bundles) | Same files, copied to engine-local storage |
| **Who reads** | Operator browsers, engine cache manager, templates | Engine processes (CEF, FFmpeg, Rive) only |
| **Purpose** | Reduce origin fetches, offline resilience, shared cache | Guarantee instant access — no network hop during rendering |
| **Eviction** | LRU with large budget (configurable) | LRU with smaller budget, pinned by rundown |

**Flow with both caches:**

```
Origin (S3 endpoint on local network)
  │
  │ cache miss only
  ▼
Gateway Cache (agent server disk)
  │
  │ LAN fetch
  ▼
Engine SSD Cache (engine machine)
  │
  │ local disk read
  ▼
Engine process (CEF / FFmpeg / Rive)
```

---

## Template Asset Resolution

### `pivox.assets.resolve()` — End-to-End

When a template calls `pivox.assets.resolve('logo-cnn-hd')`:

```
Template JS: pivox.assets.resolve('logo-cnn-hd')
  │
  ▼
SDK (in engine): looks up asset in local manifest
  - Asset ID: logo-cnn-hd
  - Local path: /cache/assets/abc123.png (already on engine SSD)
  │
  ├── File exists on SSD? → Return pivox-asset://abc123.png
  │   CEF loads from custom scheme handler (local disk, sub-ms)
  │
  └── File NOT on SSD? → Return gateway URL
      https://west-coast.storage.pivox.app/org/proj/media/abc123.png?sig=...
      CEF loads via network (LAN, cached at gateway, still fast)
      Meanwhile: engine requests priority cache fill
```

**Normal case:** Assets are pre-cached on the engine SSD by the asset cache manager before the template loads. `pivox.assets.resolve()` returns a local `pivox-asset://` URL 99%+ of the time.

### Template-Bundled vs. External Assets

| Asset Type | Where Stored | Resolution |
|---|---|---|
| **Bundled** (in template directory) | Template's own `assets/` folder | Relative path — `./assets/background.png`. Local SSD. |
| **External** (shared across templates) | Project storage | `pivox.assets.resolve('asset-id')` → SSD or gateway URL |
| **Org-shared** (brand assets) | Org `_shared/` storage | `pivox.assets.resolve('org:brand-logo')` → SSD or gateway URL |

---

## Project-Level Storage

### What Is a Project

A project is the primary organizational unit for storage:

| Example | Project | Contents |
|---|---|---|
| Newsroom | "Evening News" | Show templates, rundowns, recorded clips |
| Sports | "NFL Sunday" | Sports templates, team logos, score feeds |
| Elections | "Election Night 2026" | Election templates, candidate photos |
| Facility-wide | "Shared Assets" | Common lower-thirds, bugs, brand packages |

Projects are created by org admins. Every asset upload targets a specific project. Users see only the projects they have access to.

### Storage Quotas

Storage quotas are enforced at the project level:

```yaml
project:
  id: "proj_123"
  name: "Evening News"
  org_id: "org_abc"
  storage:
    quota_gb: 500
    used_gb: 123.4
    recording_quota_gb: 2000
    recording_used_gb: 876.2
    alert_threshold_pct: 80
```

The control plane enforces quotas before signing upload URLs. Over-quota projects get a clear error.

### Cross-Project Asset Sharing

Assets can be shared across projects within the same org without duplication:

```
POST /api/v1/assets/{asset_id}/share
{
  "target_project_id": "proj_456",
  "permission": "read"
}
```

This creates a reference — not a copy. The asset lives in its original project's storage. If the source asset is deleted, the reference breaks (referential integrity check warns before deletion).

Org-level shared assets (in `_shared/`) are readable by all projects in the org without explicit sharing.

---

## Security

### Access Control

| Layer | Enforcement |
|---|---|
| **API** | Firebase ID token + RBAC. User must have read/write permission for the target project. |
| **Storage session** | Cookie-based JWT (HS256). Opaque token maps to path-scoped access patterns on the gateway. Revocable instantly via bidi. |
| **Gateway (reads)** | Validates session cookie, glob-matches request path against authorized patterns. |
| **Gateway (uploads)** | Validates presigned URL signature (for S3) or signed upload token (for filesystem). |
| **Storage backend** | IAM policy (cloud) or bucket policy (on-prem). Defense-in-depth. |

### Credential Management

Endpoint credentials (S3 access keys) are:
- Required at endpoint creation as part of the S3 configuration
- Stored encrypted in the control plane database (Google Cloud KMS envelope encryption in production)
- Never returned in API responses (INPUT_ONLY)
- Delivered to agents over the bidi gRPC stream (encrypted in transit)
- Stored locally on the agent in encrypted config
- Rotated via `UpdateEndpoint` API with field mask on `s3.credentials`
- Connectivity validated via `validate_only: true` on Create/Update (per AIP-163)

No human ever needs to SSH into a server to manage credentials.

### Audit Trail

Storage sessions and agent messages are audited:

- **Session creation** — logged when `CreateStorageSession` is called (user, org, access patterns granted)
- **Agent bidi messages** — handshake, config updates, drain/upgrade commands, endpoint health logged to `storage_agent_audit` table (heartbeats and telemetry included, secrets redacted)
- **Upload presigned URLs** — logged when generated (user, org, project, asset, operation, TTL)

### Encryption

| Layer | Encryption |
|---|---|
| **In transit** | TLS everywhere — gateway (Let's Encrypt), S3 endpoints, bidi gRPC |
| **At rest (cloud)** | S3 SSE-S3 or SSE-KMS (customer-managed keys) |
| **At rest (on-prem)** | Storage backend encryption or volume-level (LUKS) |
| **Credentials at rest** | Encrypted in control plane DB and in agent local config |
| **Agent binary** | Ed25519 signature verification on upgrade |

---

## Offline Behavior

When Pivox Cloud is unreachable:

| Component | Behavior |
|---|---|
| **Gateway agents** | Continue serving from local cache + local S3 endpoints. No degradation for cached content. |
| **Bidi stream** | Drops. Agent retries with exponential backoff. Cached config (endpoints, cache settings, TLS cert) keeps the agent running. |
| **Active sessions** | Sessions already in agent memory continue to work. New sessions can't be pushed until bidi reconnects. |
| **New uploads** | Route to local S3 endpoints. Metadata sync to cloud queued for when connection restores. |
| **Config changes** | Not possible until cloud reconnects. Agent serves with last-known config. |
| **Cert renewal** | If cert expires during outage, HTTPS will fail. Certs are renewed well before expiry (60-day cycle, renewed at 30 days) to provide buffer. |

When cloud connectivity restores:
- Agent reconnects via bidi stream
- Receives any pending config/cert updates
- Syncs offline write metadata to cloud
- Gateway state transitions back to ACTIVE
