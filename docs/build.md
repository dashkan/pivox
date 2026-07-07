# Build Guide

## Prerequisites

All platforms:
- **CMake** 4.0+
- **vcpkg** (installed, `VCPKG_ROOT` set or `vcpkg` in PATH)
- **Go** 1.26+
- **Rust** 1.94+ (stable)
- **buf** 1.60+ (proto toolchain)
- **Node.js** 20+ (web app + Aspire AppHost tooling)

For the Engine's C/C++ toolchain (CMake + vcpkg + a platform C++
compiler), see `docs/dev/engine-build.md`.

## Cloud Controller (Go backend)

### Build

```sh
make build          # builds bin/pivox-cloud + bin/pivox-agent
make build-dev      # same, with dev build tag
```

### Database

Requires PostgreSQL with pgvector extension.

```sh
make db-create      # create pivox database
make db-up          # run migrations
make db-seed        # seed development data
```

Reset:

```sh
make db-drop        # drops the database (kills active connections first)
make db-create
make db-up
make db-seed
```

### Run

```sh
make run-server     # go run ./cmd/pivox-cloud serve
```

Default ports: gRPC `:50051`, REST `:8080`, debug `:9090`.

Flags:
- `--ollama-url` (default `http://localhost:11434`)
- `--ollama-model` (default `qwen3-vl`)
- `--database-url` (default `postgres://localhost:5432/pivox?sslmode=disable`)

### Proto codegen

```sh
make proto-generate         # Go + gateway + OpenAPI
```

### Linting

```sh
make lint-proto     # buf lint
make api-lint       # Google AIP compliance
```

### Tests

```sh
make test       # brings up the docker-compose Postgres + rustfs stack
                # (docker-compose.test.yml) and runs the suite. Idempotent.
make test-down  # tear the compose stack down
```
