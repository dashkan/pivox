---
name: proto-gen
description: Run the proto pipeline - lint, format, generate code, and tidy modules. Use when proto files change.
---

# Proto Generate

Run the full proto code generation pipeline after making changes to `.proto` files.

## Steps

1. Lint proto files: `make lint-proto`
2. Lint AIP compliance: `make api-lint`
3. Format proto files: `make proto-format`
4. Generate Go code: `make proto-generate`
5. Tidy modules: `make tidy`

## Usage

```bash
make lint-proto && make api-lint && make proto-format && make proto-generate && make tidy
```

## Notes

- If lint or api-lint fails, fix the issues before generating code.
- After generation, verify the generated code compiles: `make build`
