.PHONY: build run test tidy lint lint-fix fmt \
       lint-proto proto-format proto-breaking proto-generate proto-generate-native \
       proto-generate-native-swift proto-generate-native-cpp proto-generate-native-facade \
       build-proto-plugins test-proto-plugins update-proto-plugin-goldens proto-test-fixtures api-lint \
       db-up db-down db-migrate db-force db-seed db-clear db-drop db-create \
       docker-up docker-down firebase-emu firebase-deploy \
       proxy-nginx proxy-nginx-stop proxy-ngrok \
       test-native-ui

DATABASE_URL ?= postgresql://localhost:5432/pivox?sslmode=disable
DATABASE_NAME ?= pivox

TOOL = go tool -modfile=./tools/go.mod

# Build

build:
	go build -o bin/pivox-cloud ./cmd/pivox-cloud
	go build -o bin/pivox-agent ./cmd/pivox-agent

build-dev:
	go build -tags dev -o bin/pivox-cloud ./cmd/pivox-cloud
	go build -tags dev -o bin/pivox-agent ./cmd/pivox-agent

run-server:
	go run -tags dev ./cmd/pivox-cloud serve

run-agent:
	go run -tags dev ./cmd/pivox-agent storage --token dev-token-local

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

proto-generate: proto-generate-go proto-generate-native

# Go codegen (BE + internal gRPC types).
proto-generate-go:
	$(TOOL) buf generate

# Native codegen (Swift + C++ types for macOS app and shared C++ chat client).
# Split into separate templates so each side can have its own input scope:
# - Swift needs the full app surface (ai, api, iam, assets, storage, ...)
# - C++ only needs the shared-logic surface (ai/v1 + its transitive imports)
#   to avoid dragging in deps (google/longrunning, etc.) that the native
#   chat client doesn't need.
proto-generate-native: proto-generate-native-swift proto-generate-native-cpp proto-generate-native-facade

proto-generate-native-swift: build-proto-plugins
	$(TOOL) buf generate --template buf.gen.native.swift.yaml

proto-generate-native-cpp: build-proto-plugins
	$(TOOL) buf generate --template buf.gen.native.cpp.yaml

# Swift facade — narrow-scoped to services that have a matching C++
# bridge (kept in sync with buf.gen.native.cpp.yaml's path list).
proto-generate-native-facade: build-proto-plugins
	$(TOOL) buf generate --template buf.gen.native.facade.yaml

# Build our three local buf plugins. Each emits a distinct slice of the
# Swift↔C++ interop bridge and can be retired independently as the
# underlying interop constraints relax upstream. Sources under
# tools/cmd/; shared helpers in tools/internal/pivoxgen.
build-proto-plugins:
	@mkdir -p bin
	cd tools && go build -o $(PWD)/bin/protoc-gen-pivox-swift-protobridge ./cmd/protoc-gen-pivox-swift-protobridge
	cd tools && go build -o $(PWD)/bin/protoc-gen-pivox-cpp-bridge ./cmd/protoc-gen-pivox-cpp-bridge
	cd tools && go build -o $(PWD)/bin/protoc-gen-pivox-swift-facade ./cmd/protoc-gen-pivox-swift-facade

# Run the plugin test suites. Each plugin has a golden-file test driven
# by tools/testdata/ai_chat.descriptors.binpb.
test-proto-plugins:
	cd tools && go test ./cmd/...

# Regenerate golden files in-place (do this after an intentional
# template change). Inspect the diff before committing.
update-proto-plugin-goldens:
	cd tools && go test ./cmd/... -update

# Rebuild the descriptor fixture tests load (rare — only when we change
# the test fixture .proto or buf's managed settings).
proto-test-fixtures:
	$(TOOL) buf build --as-file-descriptor-set \
		-o tools/testdata/ai_chat.descriptors.binpb \
		--path api/proto/pivox/ai/v1/ai_chat.proto

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
