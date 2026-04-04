# Pivox Project Instructions

## Architecture

- **Cloud Controller** — SaaS management layer (Go). Source of truth.
- **Playout Agent** — On-prem agent installed alongside engines (Go). Same pattern as Storage Agent.
- **Engine** — Playout engine (Rust + C/C++). Compositor, plugins (CEF, Rive, FFmpeg), output adapters.
- **Native App** — Operator application. SwiftUI (macOS) + WinUI 3 (Windows) + shared C++ core + CEF viewports.

Do not refer to components by their tech stack ("Go control plane", "Rust engine"). Use the proper names above. Tech stack references are only appropriate in build docs and architecture decisions where the technology is relevant context.

## Testing

**All new code uses TDD.** Write tests first, then implementation. No exceptions.

| Language | Framework | Run |
|---|---|---|
| Go | `go test` — table-driven tests | `go test ./...` |
| Rust | `cargo test` — `#[cfg(test)]` modules + `tests/` dir | `cargo test` |
| C/C++ | Google Test (gtest + gmock) via CMake FetchContent | `ctest` |
| Swift / Obj-C | XCTest (unit), XCUITest (UI) | `xcodebuild test` |
| WinUI 3 / C++/WinRT | MSTest (unit), WinAppDriver (UI), Google Test (non-XAML C++) | `vstest.console.exe` |

Run tests before committing. See `docs/dev/testing.md` for full framework details, patterns, and CI integration.

## Code Quality

- Write production-quality code from the start. No "fix it later."
- Do not add features, refactoring, or improvements beyond what was asked.
- Call out overengineering and unnecessary complexity when you see it.

## Documentation

- Architecture docs are in `docs/`. Read them before making design decisions.
- Do not create README files unless explicitly asked.
- Update relevant docs when making architectural changes.
