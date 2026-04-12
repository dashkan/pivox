#!/usr/bin/env pwsh
# Cross-platform build script — PowerShell equivalent of Makefile targets.
# Usage: ./make.ps1 <target> [args]
#   ./make.ps1 build
#   ./make.ps1 run-server
#   ./make.ps1 db-migrate -Name create_auth_sessions

param(
    [Parameter(Position=0)]
    [string]$Target = "help",

    [string]$Name,
    [string]$Version,
    [string]$DatabaseUrl = "postgresql://localhost:5432/pivox?sslmode=disable",
    [string]$DatabaseName = "pivox"
)

$ErrorActionPreference = "Stop"
$Tool = "go tool -modfile=./tools/go.mod"

function Invoke-Tool { param([string]$Cmd) Invoke-Expression "$Tool $Cmd" }

switch ($Target) {
    # Build
    "build" {
        go build -o bin/pivox-cloud.exe ./cmd/pivox-cloud
        go build -o bin/pivox-agent.exe ./cmd/pivox-agent
    }
    "build-dev" {
        go build -tags dev -o bin/pivox-cloud.exe ./cmd/pivox-cloud
        go build -tags dev -o bin/pivox-agent.exe ./cmd/pivox-agent
    }
    "run-server" {
        go run -tags dev ./cmd/pivox-cloud serve
    }
    "run-agent" {
        go run -tags dev ./cmd/pivox-agent storage --token dev-token-local
    }

    # Test / Lint
    "test"     { go test ./... }
    "tidy"     { go mod tidy; Push-Location tools; go mod tidy; Pop-Location }
    "lint"     { Invoke-Tool "golangci-lint run ./..." }
    "lint-fix" { Invoke-Tool "golangci-lint run --fix ./..." }
    "fmt"      { gofmt -w . }

    # Proto
    "lint-proto"      { Invoke-Tool "buf lint" }
    "proto-format"    { Invoke-Tool "buf format -w" }
    "proto-breaking"  { Invoke-Tool "buf breaking --against '.git#branch=main'" }
    "proto-generate"  { Invoke-Tool "buf generate" }
    "api-lint"        { Invoke-Tool "api-linter --proto-path=api/proto --config=api/proto/api-linter.yaml --set-exit-status api/proto/pivox/**/**/*.proto" }

    # Database
    "db-up" {
        migrate -path internal/db/migrations -database $DatabaseUrl up
    }
    "db-down" {
        migrate -path internal/db/migrations -database $DatabaseUrl down 1
    }
    "db-migrate" {
        if (-not $Name) { Write-Error "Usage: ./make.ps1 db-migrate -Name create_users"; return }
        migrate create -ext sql -dir internal/db/migrations -seq $Name
    }
    "db-force" {
        if (-not $Version) { Write-Error "Usage: ./make.ps1 db-force -Version 1"; return }
        migrate -path internal/db/migrations -database $DatabaseUrl force $Version
    }
    "db-seed" {
        psql $DatabaseUrl -f scripts/seed.sql
    }
    "db-clear" {
        $sql = "DO `$`$ DECLARE r RECORD; BEGIN FOR r IN (SELECT tablename FROM pg_tables WHERE schemaname = 'public' AND tablename != 'schema_migrations') LOOP EXECUTE 'TRUNCATE TABLE ' || quote_ident(r.tablename) || ' CASCADE'; END LOOP; END `$`$;"
        psql $DatabaseUrl -c $sql
    }
    "db-drop" {
        psql "postgres://localhost:5432?sslmode=disable" -c "DROP DATABASE IF EXISTS $DatabaseName"
    }
    "db-create" {
        psql "postgres://localhost:5432?sslmode=disable" -c "CREATE DATABASE $DatabaseName"
    }

    # Docker
    "docker-up"   { docker compose up -d }
    "docker-down" { docker compose down }

    # Firebase
    "firebase-emu" {
        firebase emulators:start --import=.firebase-data --export-on-exit=.firebase-data --inspect-functions
    }
    "firebase-deploy" {
        pnpm --dir ./deployments/firebase/functions run deploy
    }

    # Proxy
    "proxy-nginx"      { nginx -c "$PWD/configs/nginx.conf" -e stderr }
    "proxy-nginx-stop" { nginx -c "$PWD/configs/nginx.conf" -s stop }
    "proxy-ngrok"      { ngrok start --config configs/ngrok.yml --all }

    "help" {
        Write-Host @"
Usage: ./make.ps1 <target> [args]

Build:     build, build-dev, run-server, run-agent
Test:      test, tidy, lint, lint-fix, fmt
Proto:     lint-proto, proto-format, proto-breaking, proto-generate, api-lint
Database:  db-up, db-down, db-migrate, db-force, db-seed, db-clear, db-drop, db-create
Docker:    docker-up, docker-down
Firebase:  firebase-emu, firebase-deploy
Proxy:     proxy-nginx, proxy-nginx-stop, proxy-ngrok
"@
    }

    default { Write-Error "Unknown target: $Target. Run ./make.ps1 help" }
}
