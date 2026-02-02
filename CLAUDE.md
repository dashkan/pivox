# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

This is a **Go project layout template** based on the golang-standards/project-layout. It provides a standard directory structure for Go applications but contains no actual Go source code - it's a starting point for new projects.

## Directory Structure

- `/cmd` - Main applications (each subdirectory name = executable name). Keep main functions small, import from `/internal` and `/pkg`.
- `/internal` - Private code enforced by Go compiler. Use `/internal/app` for app-specific code, `/internal/pkg` for shared internal libraries.
- `/pkg` - Public library code safe for external import.
- `/api` - OpenAPI/Swagger specs, JSON schemas, protocol definitions.
- `/web` - Web assets, templates, SPAs.
- `/configs` - Configuration templates (confd, consul-template).
- `/scripts` - Build, install, and analysis scripts (called from Makefile).
- `/build/package` - Container/OS package configs (Docker, deb, rpm).
- `/build/ci` - CI configurations (travis, circle, drone).
- `/deployments` - IaaS/PaaS/orchestration configs (docker-compose, k8s/helm, terraform).
- `/test` - External test apps and test data.
- `/docs` - Design and user documentation.
- `/tools` - Supporting tools that can import from `/pkg` and `/internal`.
- `/examples` - Application/library examples.
- `/third_party` - External tools and forked code.
- `/githooks` - Git hooks.

## Development Commands

Scripts are kept in `/scripts` and called from the root Makefile. No build commands are configured yet in this template.

Standard Go commands apply:
```bash
go build ./...
go test ./...
go mod tidy
```

## Linting

Use `gofmt` and `staticcheck` (golint is deprecated):
```bash
gofmt -w .
staticcheck ./...
```

## Key Principles

- Never create a `/src` directory (Java pattern, not Go).
- Use Go Modules (`go.mod`) for dependency management.
- The `vendor` directory is optional with Go Modules and proxy.golang.org.
