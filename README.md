# Pivox Broadcast Platform

Monorepo for the Pivox Broadcast Platform. Go control plane, storage gateway agent, React web UI, and Electron desktop app.

## Platform Features

| Feature | Status | Description |
|---|---|---|
| **Auth** | Built | Firebase Auth, custom storage JWT, Electron OAuth flow |
| **Organizations** | Services built | Multi-tenant org management. Needs UI + validation |
| **Projects** | Services built | Project-scoped workspaces. Needs UI + validation |
| **Tagging** | Services built | Tag keys, values, bindings. Needs UI + validation |
| **API Keys** | Services built | Integration authentication. Needs UI + validation |
| **Storage** | Services built | Gateways, agents, endpoints, S3/filesystem. Needs UI + validation |
| **Dashboards** | Services built | Widget-based dashboards. Needs UI + validation |
| **Asset Management** | Next | Asset lifecycle, folders, thumbnails, versioning |
| **Order Management** | Next | Asset request workflow (producer → artist → approval) |
| **Webhooks** | Planned | Event notifications for external integrations |
| **Workflow Engine** | Planned | Custom actions on asset ingress, approval workflows |
| **Notifications** | Planned | In-app + email notifications for workflow events |
| **Semantic Search** | Planned | Cross-resource search (assets, orders, templates) |
| **Scheduler / Jobs** | Planned | Scheduled playout actions, timed operations |
| **Watch / SSE** | Planned | Real-time streaming updates to UI via Server-Sent Events |
| **Audit Log** | Planned | Platform-wide activity trail for compliance |
| **AI Chatbot** | Planned | AI-powered assistant for operations |
| **Slack + Teams** | Planned | Chat integrations for notifications and commands |
| **IAM** | Services built | Roles, groups, permissions. Needs UI |
| **Form Builder** | Planned | Custom metadata capture in asset requests |
| **Image Editor** | Planned | Embedded editor for creating asset versions on the fly |
| **NRCS** | Planned | Story-centric newsroom system (iNEWS replacement) |
| **Playout Engine** | Planned | Real-time broadcast rendering (CEF, Rive, FFmpeg) |
| **WYSIWYG Animation UI** | Planned | HTML motion graphics editor for CEF engine |

## Repository Structure

```
cmd/
├── pivox-server/          # Control plane server (gRPC + REST gateway)
└── pivox-agent/           # On-prem storage gateway agent (S3 proxy + cache)

internal/
├── service/               # Domain service handlers
│   ├── organizations/     # Org management
│   ├── projects/          # Project management
│   ├── tags/              # Tag keys, values, bindings
│   ├── apikeys/           # API key management
│   ├── operations/        # Long-running operations
│   └── storage/           # Storage gateways, agents, endpoints, bidi agent service
├── agent/                 # Agent-side: bidi stream, session store, HTTP server
├── server/                # Shared: auth interceptors, validation, hooks
├── crypto/                # Encryption (GCP KMS prod, NoOp dev)
├── convert/               # DB → proto converters
├── lro/                   # LRO manager
└── db/                    # sqlc queries, migrations, seeds

api/proto/
├── pivox/api/v1/          # Core platform protos (orgs, projects, tags, dashboards)
├── pivox/iam/v1/          # Identity & access (users, groups, roles)
├── pivox/storage/v1/      # Storage (gateways, agents, endpoints)
└── pivox/agent/v1/        # Bidi agent protocol

web/
├── apps/start/            # React web UI (TanStack Router, Vite)
├── apps/electron/         # Electron desktop app
└── packages/              # Shared packages (ui, features, primitives)

docs/                      # Architecture & design documentation
```

## Quick Start

### Prerequisites

- Go 1.26+
- Node.js 22+ (see `web/.nvmrc`)
- pnpm
- PostgreSQL 18 (via Docker or Homebrew)
- [rustfs](https://github.com/rustfs/rustfs) (S3-compatible storage, via Homebrew)
- Firebase CLI (`firebase-tools`)

### Setup

```bash
# Start infrastructure
docker compose up -d              # Postgres + rustfs
# OR use Homebrew:
# brew services start postgresql
# rustfs server /tmp/rustfs-data

# Database
make db-up                        # Run migrations
make db-seed                      # Seed dev data

# Firebase emulator
make firebase-emu                 # Auth emulator on :9099

# Server (terminal 1)
make run-server                   # gRPC :50051, REST :8080

# Agent (terminal 2)
make run-agent                    # Connects to server, HTTP on :443

# Web UI (terminal 3)
cd web && pnpm install
pnpm --filter @pivox/start dev    # Vite dev server on :5173
```

### Build

```bash
make build                        # Both binaries (dev tags)
make build-release                # Both binaries (production, no dev tags)
make test                         # Go tests
cd web && pnpm test:eslint        # Frontend lint
```

### Proto Development

```bash
make lint-proto                   # buf lint
make api-lint                     # Google AIP linter
make proto-format                 # buf format
make proto-generate               # Generate Go code
make tidy                         # go mod tidy
```

### Database

```bash
make db-up                        # Apply migrations
make db-down                      # Rollback one migration
make db-seed                      # Seed dev data
make db-drop                      # Drop database
make db-create                    # Create database
make db-migrate NAME=create_foo   # Create new migration
```

## Architecture

See `docs/` for detailed documentation:

- [Architecture](docs/architecture.md) — system overview, deployment tiers
- [Storage](docs/storage.md) — storage gateway, S3 proxy, caching, session auth
- [Authentication](docs/authn.md) — Firebase Auth, Electron OAuth flow
- [Control Plane](docs/control-plane.md) — asset management, playout control
- [Engine](docs/engine.md) — rendering engine, CEF, FFmpeg, Rive
- [Protocols](docs/protocols.md) — gRPC, REST, WebSocket
