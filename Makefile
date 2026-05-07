.PHONY: build run test test-up test-down tidy lint lint-fix fmt generate \
	air air-worker mocks dev log-pivox-app-macos run-app-macos \
	lint-proto proto-format proto-breaking proto-generate \
	proto-generate-go proto-generate-native build-grpc-swift-2-plugin api-lint \
	db-up db-down db-migrate db-force db-seed db-clear db-drop db-create \
	docker-up docker-down firebase-deploy clean-fn-revisions \
	proxy-nginx proxy-nginx-stop proxy-nginx-reload proxy-ngrok \
	test-native-ui

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
	go run ./cmd/pivox-agent storage --token dev-token-local

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
proto-generate: proto-generate-go proto-generate-native generate

# Go codegen (BE + internal gRPC types).
proto-generate-go:
	$(TOOL) buf generate

# Native codegen (Swift proto types + grpc-swift-2 client stubs for the
# macOS app). The macOS app talks to Pivox cloud directly via grpc-swift-2;
# there is no shared C++ chat client to generate bridges for.
proto-generate-native: build-grpc-swift-2-plugin
	$(TOOL) buf generate --template buf.gen.native.swift.yaml

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

# run-app-macos builds the macOS app in Debug and launches it.
# Uses a project-local derivedDataPath so the .app path is
# predictable and so this loop stays independent of the Xcode
# IDE's DerivedData cache (the IDE keeps its own). On build
# failure the last 30 lines of the log surface so the failure is
# diagnosable without searching for the file.
run-app-macos:
	@xcodebuild build \
		-project native/build-xcode/Pivox.xcodeproj \
		-scheme Pivox \
		-configuration Debug \
		-derivedDataPath native/build-xcode/derived \
		-allowProvisioningUpdates \
		> /tmp/pivox-xcodebuild.log 2>&1 \
		|| (echo "build failed; tail of /tmp/pivox-xcodebuild.log:"; tail -30 /tmp/pivox-xcodebuild.log; exit 1)
	@open native/build-xcode/derived/Build/Products/Debug/Pivox.app

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

# dev runs every component of the local loop in one terminal: the
# pivox-cloud + pivox-worker air watchers, the nginx + ngrok ingress
# proxies, and the native-app log stream. `concurrently` color-codes
# each prefix and `--kill-others` tears the rest down the moment any
# one process exits — so a crashed binary or Ctrl-C cleans up cleanly
# instead of leaving zombies.
dev:
	pnpx concurrently \
		--kill-others \
		--names "air,worker,nginx,ngrok,log" \
		--prefix-colors "yellow,green,cyan,magenta,blue" \
		"$(MAKE) air" \
		"$(MAKE) air-worker" \
		"$(MAKE) proxy-nginx" \
		"$(MAKE) proxy-ngrok" \
		"$(MAKE) log-pivox-app-macos"
