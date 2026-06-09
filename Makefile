.PHONY: build test test-up test-down tidy lint lint-fix fmt generate \
	run-server run-worker run-agent \
	air air-worker mocks dev dev-preview \
	log-pivox-app-macos run-app-macos build-app-macos \
	configure-app-macos ollama-serve \
	lint-proto proto-format proto-breaking proto-generate \
	proto-generate-go proto-generate-native build-grpc-swift-2-plugin \
	proto-generate-openapi-v3 proto-generate-typescript api-lint \
	lint-icons \
	db-up db-down db-migrate db-force db-seed db-clear db-drop db-create \
	docker-up docker-down firebase-deploy clean-fn-revisions \
	ai-native \
	proxy-nginx proxy-nginx-stop proxy-nginx-reload proxy-ngrok \
	test-native-ui \
	web-build web-build-watch web-build-start \
	web-clean web-start web-start-preview electron-start

DATABASE_URL ?= postgresql://localhost:5432/pivox?sslmode=disable
DATABASE_NAME ?= pivox

TOOL = go tool -modfile=./tools/go.mod

# Native-build env. Both vars `?=` so a caller with direnv-loaded
# values overrides; otherwise the Makefile bakes its own. Avoids
# making subprocess shell state load-bearing for `make build-app-macos`.
MACOSX_SDK ?= $(shell xcrun --show-sdk-path --sdk macosx)
VCPKG_ROOT ?= $(HOME)/.vcpkg

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
proto-generate: proto-generate-go proto-generate-openapi-v3 proto-generate-typescript generate

# Go codegen (BE + internal gRPC types).
proto-generate-go:
	$(TOOL) buf generate

# Native codegen (Swift proto types + grpc-swift-2 client stubs for the
# macOS app). The macOS app talks to Pivox cloud directly via grpc-swift-2;
# there is no shared C++ chat client to generate bridges for.
proto-generate-native: build-grpc-swift-2-plugin
	$(TOOL) buf generate --template buf.gen.native.swift.yaml

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

# Build `protoc-gen-grpc-swift-2` from the grpc-swift-protobuf SwiftPM
# checkout that PivoxModels' `swift package resolve` populates. There is
# no public BSR plugin for grpc-swift-2 yet; this builds the codegen
# binary locally and parks it in bin/ where buf.gen.native.swift.yaml
# expects it. Idempotent — skips if the binary is already up to date.
build-grpc-swift-2-plugin:
	@mkdir -p bin
	@cd native/platform/macos/swift-packages/PivoxModels && swift package resolve >/dev/null
	cd native/platform/macos/swift-packages/PivoxModels/.build/checkouts/grpc-swift-protobuf && \
		swift build -c release --product protoc-gen-grpc-swift-2
	@cp native/platform/macos/swift-packages/PivoxModels/.build/checkouts/grpc-swift-protobuf/.build/release/protoc-gen-grpc-swift-2 bin/protoc-gen-grpc-swift-2

api-lint:
	$(TOOL) api-linter --proto-path=api/proto --config=api/proto/api-linter.yaml --set-exit-status api/proto/pivox/**/**/*.proto

# lint-icons enforces the Icon-enum ↔ platform-map contract: every
# value in api/proto/pivox/api/v1/icons.proto must have a matching
# `case .X:` in each platform's icon map (today only the macOS SF
# Symbol map at native/.../Dashboards/Icons/IconSymbol.swift; Windows
# joins when that surface lands). Catches drift before it ships as a
# silent UX gap (empty thumbnail, missing symbol, force-unwrap crash).
lint-icons:
	go run ./cmd/lint-icon-maps

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

# Firebase

firebase-deploy:
	pnpm --dir ./deployments/firebase/functions run deploy

# Clean up Firebase Functions deployments in Cloud Run:
#   - delete services orphaned by source-side renames or removals
#   - prune non-active revisions of surviving services
# Dry-run by default; set FORCE=1 to actually delete.
clean-fn-revisions:
	@scripts/clean-fn-revisions.sh

# build-app-macos builds the macOS app in Debug. No
# -derivedDataPath flag — this is a CMake-generated Xcode project
# whose default build location is `native/build-xcode/Debug/Pivox.app`
# (in-tree, not under Xcode IDE's DerivedData cache), and that's
# where Xcode IDE Run and `make run-app-macos` both launch from.
# Splitting the build location with -derivedDataPath produces a
# .app that nothing else launches from, leaving stale binaries in
# play.
# configure-app-macos regenerates the Xcode project from CMakeLists.txt.
# Required after CMakeLists.txt changes; safe to re-run idempotently.
# CMAKE_TOOLCHAIN_FILE points at vcpkg so find_package(cmark-gfm)
# resolves; CMAKE_OSX_SYSROOT is the active Xcode SDK so XCTest bundle
# generation succeeds without depending on the caller's shell having
# Xcode env loaded.
configure-app-macos:
	@cd native && cmake -G Xcode -B build-xcode -S . \
		-DCMAKE_TOOLCHAIN_FILE=$(VCPKG_ROOT)/scripts/buildsystems/vcpkg.cmake \
		-DCMAKE_OSX_SYSROOT=$(MACOSX_SDK)

build-app-macos:
	@test -f native/build-xcode/Pivox.xcodeproj/project.pbxproj \
		|| $(MAKE) configure-app-macos
	@xcodebuild build \
		-project native/build-xcode/Pivox.xcodeproj \
		-scheme Pivox \
		-configuration Debug \
		-allowProvisioningUpdates \
		> /tmp/pivox-xcodebuild.log 2>&1 \
		|| (echo "build failed; tail of /tmp/pivox-xcodebuild.log:"; tail -30 /tmp/pivox-xcodebuild.log; exit 1)

# run-app-macos opens the most recently built Debug Pivox.app.
# Assumes the app has already been built (Xcode IDE Run, `make
# build-app-macos`, or any other xcodebuild invocation that lands
# in the project's default BUILT_PRODUCTS_DIR). The .app path is
# resolved dynamically so the launch tracks whatever Xcode says
# the build target is.
run-app-macos:
	@APP_DIR=$$(xcodebuild \
		-project native/build-xcode/Pivox.xcodeproj \
		-scheme Pivox \
		-configuration Debug \
		-showBuildSettings 2>/dev/null \
		| awk '/^[[:space:]]*BUILT_PRODUCTS_DIR =/ {print $$3; exit}'); \
	test -d "$$APP_DIR/Pivox.app" \
		|| (echo "Pivox.app not found at $$APP_DIR — build it first (Xcode IDE or xcodebuild)"; exit 1); \
	open "$$APP_DIR/Pivox.app"

# ai-native builds the Pivox.Native native dependencies (markdown
# C++ via CMake, highlight Rust via Cargo) for the host RID and
# stages them into dotnet/Pivox.Native/runtimes/<rid>/native/.
# Run this before `dotnet build` after first checkout or after
# native sources change. Idempotent.
ai-native:
	@dotnet/scripts/build-ai-native.sh

# Native UI Tests (macOS) — image editor only. The auth UI tests
# previously depended on the Firebase Auth emulator; they are
# excluded from this target until they're rewritten to run against
# real Firebase Auth or a hermetic stub.
test-native-ui:
	@xcodebuild test \
		-project native/build-xcode/Pivox.xcodeproj \
		-scheme PivoxUITests \
		-configuration DebugUITest \
		-destination 'platform=macOS' \
		-only-testing:PivoxUITests/ImageEditorUITests \
		2>&1 | grep -E "Test Case|passed|failed|skipped|Suite" || true

# Proxy

proxy-nginx:
	nginx -c $(PWD)/configs/nginx.conf -e stderr

proxy-nginx-stop:
	nginx -c $(PWD)/configs/nginx.conf -s stop

# Re-read configs/nginx.conf without dropping in-flight connections.
# Use after editing locations/upstreams; the running master forks
# new workers with the fresh config and gracefully drains the old.
proxy-nginx-reload:
	nginx -c $(PWD)/configs/nginx.conf -s reload

proxy-ngrok:
	ngrok start --config configs/ngrok.yml --all

# Dev loop

# log-pivox-app-macos streams the macOS app's unified-log output
# at debug level, scoped to the native app's subsystem so we don't
# drown in unrelated system messages. `--style=compact` drops the
# verbose timestamp prefix so each line is short enough to read in
# a multiplexed terminal.
log-pivox-app-macos:
	log stream --predicate 'subsystem == "app.pivox.native"' --level=debug --style=compact

# ollama-serve runs the Ollama daemon in the foreground. Pivox's
# AiChat handler dials it at http://localhost:11434 (overridable via
# --ollama-url / PIVOX_OLLAMA_URL); without it, StreamGenerateContent
# fails with `connection refused`. Foreground form pairs cleanly
# with `make dev` — Ctrl-C / sibling-failure tears it down with the
# rest of the loop.
ollama-serve:
	ollama serve

# Web watchers + dev server. Each is a thin wrapper around the
# corresponding pnpm script in web/package.json so callers can run
# them standalone (e.g. `make web-primitives`) or composed via `make dev`.
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
# workspace deps get rebuilt out of order. Symptom is `make dev`
# reporting `Cannot find module '@pivox/...'` errors from the
# web-ui watcher even though the symlinks + dist/ files are
# present — Nitro's cache (.nitro/, .output/) and Vite's optimized-
# deps cache (node_modules/.vite/) hold dependency-graph snapshots
# that don't always invalidate when a workspace package
# republishes. Run this when `make dev` starts complaining about
# missing modules; retry `make dev` after. Electron isn't wiped
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
# checkout. Without this prerequisite, the first `make dev` or
# `make electron-start` after a clone races the watchers and fails
# with "Cannot find module '@pivox/storage'" before any of the
# `--watch` jobs in `dev` have produced their initial output.
# `pnpm run build` filters to `./packages/**` (see web/package.json),
# so this builds libraries only — apps stay fresh for the watchers.
web-start: web-build
	pnpm run --dir web web:start

web-start-preview: web-build
	pnpm run --dir web web:start:preview

electron-start: web-build
	pnpm run --dir web electron:start

# dev runs every component of the local loop in one terminal: the
# pivox-cloud + pivox-worker air watchers, the nginx + ngrok ingress
# proxies, the native-app log stream, and the web watchers + dev
# server. `concurrently` color-codes each prefix and `--kill-others`
# tears the rest down the moment any one process exits — so a crashed
# binary or Ctrl-C cleans up cleanly instead of leaving zombies.
#
# `web-build` prerequisite — see the comment on `web-start` above for
# why this is non-optional. Watchers handle incremental rebuilds, but
# the FIRST emit needs to land before the dev server boots.
dev: web-build
	pnpx concurrently \
		--kill-others \
		--names "cloud,worker,nginx,ngrok,packages,start" \
		--prefix-colors "yellow,green,cyan,magenta,blue,gray,white" \
		"$(MAKE) air" \
		"$(MAKE) air-worker" \
		"$(MAKE) proxy-nginx" \
		"$(MAKE) proxy-ngrok" \
		"$(MAKE) web-build-watch" \
		"$(MAKE) web-start"

dev-preview: web-build-start
	pnpx concurrently \
		--kill-others \
		--names "cloud,worker,nginx,ngrok,build,start" \
		--prefix-colors "yellow,green,cyan,magenta,white" \
		"$(MAKE) air" \
		"$(MAKE) air-worker" \
		"$(MAKE) proxy-nginx" \
		"$(MAKE) proxy-ngrok" \
		"$(MAKE) web-start-preview"
