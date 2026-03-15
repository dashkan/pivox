# Pivox Justfile
# Tools are managed in ./tools/go.mod and run via `go tool -modfile`

tools_mod := "./tools/go.mod"

# Database
database_url := env("DATABASE_URL", "postgres://localhost:5432/pivox?sslmode=disable")
database_name := env("DATABASE_NAME", "pivox")
migrations_dir := "internal/db/migrations"

# Proto
proto_dir := "api/proto"
api_linter_config := "api/proto/api-linter.yaml"

# List available recipes
[doc("Show this help")]
default:
    @just --list

# Development

[doc("Build the server binary")]
build:
    go build -o server ./cmd/server

[doc("Run the server")]
run:
    go run ./cmd/server

[doc("Run tests")]
test:
    go test ./...

[doc("Run go mod tidy for all modules")]
tidy:
    go mod tidy
    cd tools && go mod tidy

# Linting

[doc("Run golangci-lint")]
lint:
    go tool -modfile={{ tools_mod }} golangci-lint run ./...

[doc("Run golangci-lint with auto-fix")]
lint-fix:
    go tool -modfile={{ tools_mod }} golangci-lint run --fix ./...

[doc("Format Go code")]
fmt:
    gofmt -w .

# Proto / Code Generation

[doc("Lint proto files with buf")]
proto-lint:
    go tool -modfile={{ tools_mod }} buf lint

[doc("Format proto files with buf")]
proto-format:
    go tool -modfile={{ tools_mod }} buf format -w

[doc("Check for breaking proto changes against main")]
proto-breaking:
    go tool -modfile={{ tools_mod }} buf breaking --against '.git#branch=main'

[doc("Generate Go code from proto files")]
proto-generate:
    go tool -modfile={{ tools_mod }} buf generate

[doc("Lint proto files with Google API linter")]
api-lint:
    go tool -modfile={{ tools_mod }} api-linter \
        --proto-path={{ proto_dir }} \
        --config={{ api_linter_config }} \
        --set-exit-status \
        {{ proto_dir }}/pivox/**/**/*.proto

# Database Migrations (requires migrate in PATH; go tool lacks -tags support: golang/go#71503)

[doc("Run all pending migrations")]
migrate-up:
    migrate -path {{ migrations_dir }} -database "{{ database_url }}" up

[doc("Rollback the last migration")]
migrate-down:
    migrate -path {{ migrations_dir }} -database "{{ database_url }}" down 1

[doc("Create a new migration")]
migrate-create name:
    migrate create -ext sql -dir {{ migrations_dir }} -seq {{ name }}

[doc("Force migration version")]
migrate-force version:
    migrate -path {{ migrations_dir }} -database "{{ database_url }}" force {{ version }}

[doc("Seed the database")]
db-seed:
    psql "{{ database_url }}" -f scripts/seed.sql

[doc("Clear all data from the database (truncate all tables)")]
db-clear:
    psql "{{ database_url }}" -c "DO \$\$ DECLARE r RECORD; BEGIN FOR r IN (SELECT tablename FROM pg_tables WHERE schemaname = 'public' AND tablename != 'schema_migrations') LOOP EXECUTE 'TRUNCATE TABLE ' || quote_ident(r.tablename) || ' CASCADE'; END LOOP; END \$\$;"

[doc("Drop the database")]
db-drop:
    psql "postgres://localhost:5432?sslmode=disable" -c "DROP DATABASE IF EXISTS {{ database_name }}"

[doc("Create the database")]
db-create:
    psql "postgres://localhost:5432?sslmode=disable" -c "CREATE DATABASE {{ database_name }}"

# Firebase

[doc("Start Firebase emulators (auth + functions)")]
emulators:
    firebase emulators:start --import=.firebase-data --export-on-exit=.firebase-data

# Docker

[doc("Start docker-compose services")]
up:
    docker compose up -d

[doc("Stop docker-compose services")]
down:
    docker compose down
