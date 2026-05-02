.PHONY: build run test tidy lint lint-fix fmt generate \
       air dev-air \
       lint-proto proto-format proto-breaking proto-generate \
       proto-generate-go proto-generate-native build-grpc-swift-2-plugin api-lint \
       db-up db-down db-migrate db-force db-seed db-clear db-drop db-create \
       docker-up docker-down firebase-emu firebase-deploy clean-fn-revisions \
       proxy-nginx proxy-nginx-stop proxy-ngrok \
       test-native-ui

DATABASE_URL ?= postgresql://localhost:5432/pivox?sslmode=disable
DATABASE_NAME ?= pivox

TOOL = go tool -modfile=./tools/go.mod

# Build

dev-build:
	go build -tags dev -o bin/pivox-cloud ./cmd/pivox-cloud
	go build -tags dev -o bin/pivox-agent ./cmd/pivox-agent
	go build -tags dev -o bin/pivox-worker ./cmd/pivox-worker

dev-server:
	go run -tags dev ./cmd/pivox-cloud serve

dev-agent:
	go run -tags dev ./cmd/pivox-agent storage --token dev-token-local

dev-worker:
	go run -tags dev ./cmd/pivox-worker

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
#
# Two targets so the build-tag mode is explicit at the command line —
# configs/air.toml builds without `-tags dev`, configs/air.dev.toml
# builds with it. Both write their binary into ./tmp/ under
# different names so the two modes can run side-by-side without
# stomping each other.

air:
	air -c configs/air.toml

dev-air:
	air -c configs/air.dev.toml

air-worker:
	air -c configs/air.worker.toml

dev-air-worker:
	air -c configs/air.worker.dev.toml

test:
	go test ./...

tidy:
	go mod tidy && cd tools && go mod tidy

lint:
	$(TOOL) golangci-lint run ./...

lint-fix:
	$(TOOL) golangci-lint run --fix ./...

fmt:
	gofmt -w .

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

firebase-emu:
	firebase emulators:start --import=.firebase-data --export-on-exit=.firebase-data --inspect-functions

firebase-deploy:
	pnpm --dir ./deployments/firebase/functions run deploy

# Clean up Firebase Functions deployments in Cloud Run:
#   - delete services orphaned by source-side renames or removals
#   - prune non-active revisions of surviving services
# Dry-run by default; set FORCE=1 to actually delete.
clean-fn-revisions:
	@scripts/clean-fn-revisions.sh

# Native UI Tests (macOS)

test-native-ui:
	@echo "=== Image editor tests (DebugUITest, no emulator) ==="
	@xcodebuild test \
		-project native/build-xcode/Pivox.xcodeproj \
		-scheme PivoxUITests \
		-configuration DebugUITest \
		-destination 'platform=macOS' \
		-only-testing:PivoxUITests/ImageEditorUITests \
		2>&1 | grep -E "Test Case|passed|failed|skipped|Suite" || true
	@echo "=== Auth tests (Debug + emulator) ==="
	@firebase emulators:start --only auth --project pivox-cloud &
	@sleep 3
	@xcodebuild test \
		-project native/build-xcode/Pivox.xcodeproj \
		-scheme PivoxUITests \
		-configuration Debug \
		-destination 'platform=macOS' \
		-only-testing:PivoxUITests/AuthUITests \
		2>&1 | grep -E "Test Case|passed|failed|skipped|Suite" || true
	@-pkill -f "firebase.*emulators" 2>/dev/null
	@echo "Done."

# Proxy

proxy-nginx:
	nginx -c $(PWD)/configs/nginx.conf -e stderr

proxy-nginx-stop:
	nginx -c $(PWD)/configs/nginx.conf -s stop

proxy-ngrok:
	ngrok start --config configs/ngrok.yml --all
