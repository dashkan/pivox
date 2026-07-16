.PHONY: build test test-up test-down tidy lint lint-fix fmt generate \
	run-server run-worker run-agent \
	air air-worker mocks \
	ollama-serve \
	lint-proto proto-format proto-breaking proto-generate \
	proto-generate-go \
	proto-generate-openapi-v2 proto-generate-openapi-v3 proto-generate-typescript api-lint \
	db-up db-down db-migrate db-force db-seed db-clear db-drop db-create \
	docker-up docker-down \
	web-build web-build-watch web-build-start \
	web-clean web-start web-start-preview electron-start

DATABASE_URL ?= postgresql://localhost:5432/pivox?sslmode=disable
DATABASE_NAME ?= pivox

TOOL = go tool -modfile=./tools/go.mod

# Build

build:
	go build -o bin/pivox-cloud ./cmd/pivox-cloud
	go build -o bin/pivox-agent ./cmd/pivox-agent
	go build -o bin/pivox-worker ./cmd/pivox-worker

run-server:
	go run ./cmd/pivox-cloud serve

run-agent:
	go run ./cmd/pivox-agent storage --token dev-token-local --port 8083

run-worker:
	go run ./cmd/pivox-worker

# Hot reload via air (https://github.com/air-verse/air). Install
# air separately (`go install github.com/air-verse/air@latest` or
# `brew install air`) — it's not bundled in tools/go.mod because
# its transitive tablewriter v1.x conflicts with api-linter's v0.x
# requirement.

air:
	air -c configs/air.toml

air-worker:
	air -c configs/air.worker.toml

# Tests run against the shared docker-compose stack (Postgres +
# rustfs, see docker-compose.test.yml). `make test` brings the stack
# up first; compose is idempotent so re-runs are no-ops if it's
# already running. Tear down with `make test-down`.
#
# We list only packages that actually have tests so `?  no test
# files` lines for generated proto packages and cmd entrypoints
# don't drown out the signal.
#
# 30s is a hang ceiling, not a runtime budget. Real suite runs in
# under 10s; if a single package starts taking longer, that's a
# regression worth catching, not accommodating.
TEST_PACKAGES = $(shell go list -f '{{if or .TestGoFiles .XTestGoFiles}}{{.ImportPath}}{{end}}' ./...)

test: test-up
	go test -timeout 30s $(TEST_PACKAGES)

# test-up brings up the docker-compose test stack and waits for
# healthchecks. Idempotent.
test-up:
	docker compose -p pivox-test -f docker-compose.test.yml up -d --wait

# test-down stops + removes the test stack. Use to free ports or
# reset state between sessions.
test-down:
	docker compose -p pivox-test -f docker-compose.test.yml down -v

tidy:
	go mod tidy && cd tools && go mod tidy

lint:
	$(TOOL) golangci-lint run ./...

lint-fix:
	$(TOOL) golangci-lint run --fix ./...

fmt:
	gofmt -w .

# Regenerate testify-style mocks for *external* boundaries listed in
# .mockery.yml. Internal interfaces (Querier, etc.) are NOT mocked
# per #71 — service-layer tests use grpcharness against a real DB.
mocks:
	$(TOOL) mockery

# Proto

lint-proto:
	$(TOOL) buf lint

proto-format:
	$(TOOL) buf format -w

proto-breaking:
	$(TOOL) buf breaking --against '.git\#branch=main'

generate:
	go generate ./...

# proto-generate chains into `generate` so editing a proto file
# (e.g. adding a `pivox.permission.v1.required_permission` option)
# also regenerates Go-side codegen artifacts that depend on the
# new descriptors — currently the permission registry, but the
# pattern extends to anything else built via `//go:generate` that
# walks proto descriptors.
proto-generate: proto-generate-go proto-generate-openapi-v2 proto-generate-openapi-v3 proto-generate-typescript generate

# Go codegen (BE + internal gRPC types). Uses the default buf.gen.yaml,
# which generates go/go-grpc/grpc-gateway over ALL protos (mcp included).
proto-generate-go:
	$(TOOL) buf generate

# OpenAPI v2 (merged swagger) codegen. Split into its own buf template so
# it can EXCLUDE pivox.mcp.v1 from the web-facing spec (mcp's message names
# collide with the canonical pivox.api.v1 ones under allow_merge). Must run
# before proto-generate-openapi-v3, which upgrades this output to v3.
proto-generate-openapi-v2:
	$(TOOL) buf generate --template buf.gen.openapi.yaml

# Upgrade the merged OpenAPI v2 spec directly to OpenAPI v3.1 using
# @scalar/cli. grpc-gateway's protoc-gen-openapiv3 is alpha/unpublished,
# so we bridge through scalar's upgrader until that lands.
#
# Version is pinned for supply-chain reasons — bump deliberately, not via
# `latest`. Invoked through `pnpx` so the package is fetched into the pnpm
# content-addressable store rather than npm's mutable cache.
SCALAR_CLI_VERSION ?= 1.9.4
proto-generate-openapi-v3:
	@mkdir -p api/openapi/v3
	pnpx @scalar/cli@$(SCALAR_CLI_VERSION) document upgrade \
	  api/openapi/v2/pivox.swagger.yaml \
	  --output api/openapi/v3/pivox.yaml

# Generate TypeScript types from the OpenAPI v3 spec into the
# @pivox/client package. Runs `pnpm gen:types` in web/packages/client,
# which invokes openapi-typescript (pinned via devDependencies) and
# writes src/generated/types.gen.ts. The `.gen.ts` suffix puts the
# file under the global `*.gen.{ts,tsx}` ignore in eslint.config.js
# and `*.gen.ts` in .prettierignore — generated code is excluded from
# lint and formatting.
#
# Precondition: `cd web && pnpm install` has run at least once, so the
# pinned openapi-typescript binary exists in @pivox/client's
# node_modules. CI runs install before make; fresh local checkouts
# should follow the same order. Same pattern as the other web targets
# (web-features, web-start, ...) — none of them guard for install.
proto-generate-typescript:
	cd web/packages/client && pnpm gen:types

api-lint:
	$(TOOL) api-linter --proto-path=api/proto --config=api/proto/api-linter.yaml --set-exit-status api/proto/pivox/**/**/*.proto

# Database

db-up:
	migrate -path internal/db/migrations -database "$(DATABASE_URL)" up

db-down:
	migrate -path internal/db/migrations -database "$(DATABASE_URL)" down 1

db-migrate:
	@test -n "$(NAME)" || (echo "Usage: make db-migrate NAME=create_users" && exit 1)
	migrate create -ext sql -dir internal/db/migrations -seq $(NAME)

db-force:
	@test -n "$(VERSION)" || (echo "Usage: make db-force VERSION=1" && exit 1)
	migrate -path internal/db/migrations -database "$(DATABASE_URL)" force $(VERSION)

db-seed:
	psql "$(DATABASE_URL)" -f scripts/seed.sql

db-clear:
	psql "$(DATABASE_URL)" -c "DO \$$\$$ DECLARE r RECORD; BEGIN FOR r IN (SELECT tablename FROM pg_tables WHERE schemaname = 'public' AND tablename != 'schema_migrations') LOOP EXECUTE 'TRUNCATE TABLE ' || quote_ident(r.tablename) || ' CASCADE'; END LOOP; END \$$\$$;"

db-drop:
	psql "postgres://localhost:5432?sslmode=disable" -c "DROP DATABASE IF EXISTS $(DATABASE_NAME)"

db-create:
	psql "postgres://localhost:5432?sslmode=disable" -c "CREATE DATABASE $(DATABASE_NAME)"

# Docker

docker-up:
	docker compose up -d

docker-down:
	docker compose down

# Dev loop

# ollama-serve runs the Ollama daemon in the foreground. Pivox's
# AiChat handler dials it at http://localhost:11434 (overridable via
# --ollama-url / PIVOX_OLLAMA_URL); without it, StreamGenerateContent
# fails with `connection refused`. Foreground form pairs cleanly
# with the Aspire loop — Ctrl-C tears it down with the rest.
ollama-serve:
	ollama serve

# Web watchers + dev server. Each is a thin wrapper around the
# corresponding pnpm script in web/package.json so callers can run
# them standalone (e.g. `make web-primitives`); Aspire runs the loop.
web-build:
	pnpm run --dir web web:build

# `--parallel` (via the web:build:watch script) is load-bearing: each
# package's `vite build --watch` never exits, so pnpm's default
# topological run with a concurrency cap of 4 fills its slots with the
# first 4 leaf packages and STARVES the rest — @pivox/ui, features, and
# storage never get a watcher, so edits to their src never reach dist/.
# `--parallel` starts every package's watcher at once. Safe because the
# `web-build` prerequisite on `dev` produces the initial topo-ordered
# dist/ before any watcher starts; watchers only do incremental rebuilds.
web-build-watch:
	pnpm run --dir web web:build:watch

# Wipes the start app's build/runtime caches that go stale when
# workspace deps get rebuilt out of order. Symptom is the dev loop
# reporting `Cannot find module '@pivox/...'` errors from the
# web-ui watcher even though the symlinks + dist/ files are
# present — Nitro's cache (.nitro/, .output/) and Vite's optimized-
# deps cache (node_modules/.vite/) hold dependency-graph snapshots
# that don't always invalidate when a workspace package
# republishes. Run this when the dev loop starts complaining about
# missing modules; retry after. Electron isn't wiped
# here because we haven't seen the same pattern there yet — add
# its paths if/when it surfaces.
web-clean:
	rm -rf web/apps/start/.output \
	       web/apps/start/node_modules/.vite \
	       web/apps/start/node_modules/.nitro

# `web-build` is a Make prerequisite on every target that launches a
# vite dev server, because the start + electron vite configs import
# from workspace library packages at config-LOAD time (e.g.,
# `electron.vite.config.ts` imports `buildBootScript` from
# `@pivox/storage`). Vite resolves those imports via each package's
# `exports` map → `dist/esm/index.js`, which doesn't exist on a fresh
# checkout. Without this prerequisite, the first `aspire start` or
# `make electron-start` after a clone races the watchers and fails
# with "Cannot find module '@pivox/storage'" before any of the
# `--watch` jobs have produced their initial output.
# `pnpm run build` filters to `./packages/**` (see web/package.json),
# so this builds libraries only — apps stay fresh for the watchers.
web-start: web-build
	pnpm run --dir web web:start

web-start-preview: web-build
	pnpm run --dir web web:start:preview

# Build the start app (apps/start) on top of the libraries. `web-build`
# (libraries-only) is a prerequisite so the app build always sees a
# current dist/. Encoding the dependency here — rather than on each
# caller — guarantees the order even under `make -j`: parallel make can
# run sibling prerequisites concurrently, but a prereq chain is always
# serialized. `vite preview` (web:start:preview) serves this output.
web-build-start: web-build
	pnpm run --dir web web:build:start

electron-start: web-build
	pnpm run --dir web electron:start

