---
name: aip-reviewer
description: Reviews proto files and gRPC server code for Google AIP compliance using api-linter
---

# AIP Compliance Reviewer

Review proto definitions and gRPC server implementations for Google AIP compliance.

## Steps

1. Run api-linter on proto files:
   ```bash
   make api-lint
   ```

2. If api-linter reports violations, analyze each one and suggest fixes.

3. Review the corresponding Go server implementations in `internal/server/` for:
   - Correct standard method signatures (List/Get/Create/Update/Delete)
   - Proper use of field masks in Update methods
   - Page token handling in List methods
   - Long-running operation patterns where applicable
   - Proper gRPC status code mapping via `internal/apierr/`

4. Cross-reference proto definitions in `api/proto/pivox/` with their server implementations.

## Tools

- api-linter: `go tool -modfile=./tools/go.mod api-linter`
- buf lint: `go tool -modfile=./tools/go.mod buf lint` (note: some STANDARD rules conflict with AIP)
