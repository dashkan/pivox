# Pivox Server

Go control plane for the Pivox broadcast playout system. Manages all broadcast operations above the rendering engine — NRCS/rundown management, asset management, operator UI, data binding, hardware automation, redundancy coordination, and external integrations.

## Architecture

The control plane is a single Go codebase that runs in two modes:

- **Cloud mode** — source of truth for configuration, user management, asset storage. Serves the web UI.
- **Local mode** — runs on-prem on a separate server from the engine. Syncs with cloud, operates independently during outages.

See [pivox-docs](https://github.com/dashkan/pivox-docs) for full architecture documentation.

## Core Services

- **Playout Controller** — state machine, command routing to engine
- **NRCS / Rundown Manager** — shows, segments, items
- **Template Registry** — versioning, approval workflow
- **Asset Manager** — storage tiers (hot/nearline/cloud), cache management
- **Data Plane** — live data feed routing, gating, throttling
- **Timer Service** — frame-accurate auto-advance
- **Redundancy Coordinator** — dual-write, failover
- **Recording Manager** — compliance recording, ingest, indexing
- **Hardware Automation** — video routers, vision mixers, audio desks, multiviewers
- **Monitoring** — health, metrics, alerting

## Integration Gateways

- MOS (legacy NRCS integration)
- VDCP (automation)
- TSL UMD (tally)
- Hardware protocols (SW-P-08, Ember+, SNMP)

## Communication

- **To engine:** gRPC over TCP (facility LAN) using protobuf definitions from [pivox-proto](https://github.com/dashkan/pivox-proto)
- **To operator UI:** REST + WebSocket

## Related Repositories

| Repo | Description |
|---|---|
| [pivox-docs](https://github.com/dashkan/pivox-docs) | Architecture & design documentation |
| [pivox-web](https://github.com/dashkan/pivox-web) | React operator UI + Electron |

## Development

```bash
go build ./...
go test ./...
```
