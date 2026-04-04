# Product Licensing

## Overview

Pivox uses **signed JWT tokens** as the license format. The Cloud Controller is the license authority — it issues, validates, and distributes license entitlements. No third-party licensing system (CodeMeter, FlexLM, hardware dongles) is required.

Licenses gate features at three enforcement points: the engine, the Playout Agent, and the native app. The engine never hard-stops on-air operations due to licensing — licensing only gates *starting* things, not *running* things.

For dependency and third-party licensing (FFmpeg, CEF, NDI, AJA SDK, codec patents), see `docs/licensing.md`.

## License Format

A license is a signed JWT containing claims that define what the customer paid for:

```json
{
  "iss": "pivox.app",
  "sub": "org-uuid",
  "iat": 1712188800,
  "exp": 1743724800,
  "org": "BBC Studios",
  "license": {
    "tier": "broadcast",
    "channels": 4,
    "max_resolution": "2160p",
    "outputs": ["sdi", "ndi", "st2110"],
    "ingest": false,
    "redundancy": true,
    "max_engines": 2,
    "max_operators": 10,
    "app_modes": ["operator", "designer", "library", "engineering", "admin"]
  }
}
```

Signed with ES256 (ECDSA on P-256). The Cloud Controller holds the private key in a cloud HSM (AWS KMS, Google Cloud HSM). The public key is embedded in the Playout Agent and native app binaries for offline validation.

## License Dimensions

| Dimension | What It Gates | Enforced By |
|---|---|---|
| **Channels per engine** | Max concurrent channels per engine process | Engine |
| **Max output resolution** | 1080p, 2160p, 4320p | Engine |
| **Output types** | SDI, NDI, ST 2110 | Engine |
| **Ingest capability** | SDI ingest + record-to-storage (future) | Engine |
| **Redundancy** | Hot standby engine pairs | Playout Agent |
| **Engine count** | Max engines managed by one Playout Agent | Playout Agent |
| **Concurrent operators** | Max simultaneous authenticated users | Cloud Controller |
| **App modes** | Which workspaces are visible (Operator, Designer, Library, Engineering, Admin) | Native app |

## License Tiers

| Tier | Channels | Resolution | Outputs | Ingest | Redundancy | Engines | Operators | App Modes |
|---|---|---|---|---|---|---|---|---|
| **Designer** | 1 (embedded) | 1080p | NDI | No | No | 0 | 1 | Designer |
| **Studio** | 2 | 1080p | SDI + NDI | No | No | 1 | 5 | Operator, Designer, Library |
| **Broadcast** | 4 | 2160p | SDI + NDI + ST 2110 | Add-on | Yes | 2 | 10 | All |
| **Enterprise** | 8+ | 4320p | All | Yes | Yes | 4+ | 25+ | All + multi-site |

Tiers are a sales construct — the license JWT contains the actual entitlements, not the tier name. A customer can have a custom license that doesn't map to any named tier.

## Entitlement Distribution

### Cloud Customers (No On-Prem)

The Cloud Controller enforces entitlements directly. No license file. Entitlements are tied to the customer's subscription in the billing system. The native app receives entitlements from the Cloud Controller API at login.

### On-Prem Customers (Playout Agent)

The Playout Agent receives entitlements from the Cloud Controller via the bidi gRPC stream — the same channel used for config delivery, rundown sync, and cert management. No license file to manage on-prem. The admin never touches a config file for licensing.

```
Cloud Controller
  │
  │  bidi gRPC (agent-initiated, outbound)
  │  Pushes entitlements as part of config stream
  │
  ▼
Playout Agent (on-prem)
  │
  │  Caches entitlements locally (30-day lease)
  │  Phones home daily for fresh entitlements
  │
  ├──→ Engine A: "4 channels, 2160p, SDI+NDI"
  ├──→ Engine B: "4 channels, 2160p, SDI+NDI"
  └──→ Native App: "all modes, 10 operators"
```

The Playout Agent distributes entitlements to engines at startup via the existing gRPC connection. Engines cache their entitlements locally.

### Native App

The native app receives entitlements either:
- **From the Cloud Controller** (cloud-only customers) — at login, via the API
- **From the Playout Agent** (on-prem customers) — via the existing gRPC connection

The app uses entitlements to show/hide workspace modes and features.

## Entitlement Caching and Offline Operation

### Daily Phone-Home

The Playout Agent contacts the Cloud Controller daily to refresh entitlements. This keeps licenses current and allows immediate effect for upgrades, downgrades, or added features.

### 30-Day Cache

When the Playout Agent can't reach the Cloud Controller (internet outage, cloud maintenance, network issues), it continues operating on cached entitlements for up to **30 days**. This is full, normal operation — not degraded mode. Everything works exactly as it did when the entitlements were last refreshed.

30 days covers:
- Extended internet outages
- Air-gapped facilities with infrequent connectivity windows
- Cloud maintenance periods
- Network infrastructure changes

### After 30 Days Without Contact

If the Playout Agent hasn't reached the Cloud Controller for 30+ days:

- **Everything on-air keeps running.** The engine never stops a live broadcast for licensing.
- **New shows can't be started.** Can't load new rundowns, can't start new channels that weren't already running.
- **Warnings displayed.** Monitoring dashboard, native app, admin email alerts.

The on-air path is never touched by licensing. Licensing only gates *starting* things, not *running* things.

### License Expiry (Business Event)

License expiry is distinct from connectivity loss. A license expires when the `exp` claim in the JWT passes, or when the Cloud Controller explicitly revokes it (customer stopped paying).

On expiry, the same rules apply: on-air keeps running, new shows can't start, warnings everywhere. The sales team has the contract grace period to resolve billing before the customer is affected.

## Enforcement Points

### Engine

The engine receives entitlements from the Playout Agent at startup and caches them locally.

**What the engine enforces:**
- Channel count — refuses to start channels beyond the limit
- Resolution — refuses to configure output resolution above the limit
- Output adapters — refuses to initialize SDI/ST 2110 if not licensed
- Ingest — refuses to configure SDI input if not licensed

**What the engine never does:**
- Stop a running channel due to license
- Degrade quality of a running output
- Interrupt any on-air operation

### Playout Agent

**What it enforces:**
- Engine count — refuses to accept connections from engines beyond the limit
- Redundancy — refuses to configure hot standby pairs if not licensed
- Feature gates on playout commands — returns errors for unlicensed features before they reach the engine

### Native App

**What it enforces:**
- Workspace visibility — hides modes the license doesn't include
- Feature visibility — hides UI for features not in the license (ingest controls, ST 2110 config)
- Operator count — Cloud Controller or Playout Agent tracks concurrent sessions; app shows "max operators reached" if at limit

## License Lifecycle

### Issuance

A Pivox admin (internal) issues a license via an admin tool or dashboard:

1. Assembles the entitlement claims based on the customer's contract
2. Signs the JWT with the Pivox private key (via cloud HSM)
3. Stores the license in the Cloud Controller database
4. Cloud Controller pushes entitlements to the customer's Playout Agent on the next sync

### Upgrades / Changes

License changes (more channels, higher resolution, new features) take effect on the next daily sync. For urgent changes, an admin can trigger an immediate push from the Cloud Controller.

### Renewals

Licenses have an expiry date (`exp` claim). Renewal generates a new JWT with a new expiry. The Cloud Controller handles this automatically for subscription customers.

### Revocation

The Cloud Controller can revoke a license immediately. On the next agent phone-home, the Playout Agent receives revoked entitlements and enters the grace behavior (on-air keeps running, new shows blocked).

## Security

### Tamper Resistance

The license JWT is signed with ES256. The public key is embedded in the Playout Agent and engine binaries. Modifying the JWT invalidates the signature. This is sufficient for B2B — broadcast customers don't pirate software. They have audits, procurement processes, and support contracts.

### No Hardware Dongles

Hardware dongles (CodeMeter, HASP/Sentinel) add cost, physical management burden, and points of failure. Broadcast engineers managing racks of equipment don't want to track USB tokens. JWT-based licensing with cached leases provides equivalent functionality without physical dependencies.

### Private Key Security

The license signing private key is stored in a cloud HSM. It never exists on disk in plaintext. License signing happens server-side only.

## Offline Mode

Licensing is one part of the broader offline mode design. When connectivity is lost, multiple systems are affected: authentication (Firebase), license validation, rundown sync, asset access, and data feeds. The offline mode design must address all of these together.

See `docs/offline-mode.md` (to be written) for the comprehensive offline operation design covering auth, licensing, caching, and degradation across all connectivity scenarios.

## Comparison to Broadcast Industry

| Capability | Vizrt (CodeMeter) | Ross (Sentinel) | Pivox (JWT + Cloud Controller) |
|---|---|---|---|
| License validation | CodeMeter runtime on each machine | USB dongle per machine | JWT signature verification (built-in) |
| Feature gating | License container | Dongle features | JWT claims |
| Offline support | CodeMeter cached lease | Dongle is always offline | 30-day cached entitlements |
| License server | Dedicated CodeMeter server | No (dongle-based) | Cloud Controller (already exists) |
| Seat management | CodeMeter tracks | Manual dongle distribution | Cloud Controller tracks sessions |
| Physical dependency | Optional dongle | Required dongle | None |
| Tamper resistance | Encrypted containers | Hardware dongle | Signed tokens (sufficient for B2B) |
| License distribution | CodeMeter portal | Ship dongles | Automatic via bidi gRPC stream |
| On-prem management | Install CodeMeter runtime | Plug in dongle | Zero — pushed from cloud |
