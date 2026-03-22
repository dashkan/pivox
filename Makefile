.PHONY: build run test tidy lint lint-fix fmt \
       lint-proto proto-format proto-breaking proto-generate api-lint \
       db-up db-down db-migrate db-force db-seed db-clear db-drop db-create \
       docker-up docker-down

DATABASE_URL ?= postgresql://localhost:5432/pivox?sslmode=disable
DATABASE_NAME ?= pivox

TOOL = go tool -modfile=./tools/go.mod

# Build

build:
	go build -o bin/pivox-server ./cmd/pivox-server
	go build -o bin/pivox-agent ./cmd/pivox-agent

build-server:
	go build -o bin/pivox-server ./cmd/pivox-server

build-agent:
	go build -o bin/pivox-agent ./cmd/pivox-agent

run:
	go run ./cmd/pivox-server

run-agent:
	go run ./cmd/pivox-agent

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

proto-generate:
	$(TOOL) buf generate

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
