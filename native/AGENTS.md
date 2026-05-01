# Native App — agent conventions

Scope: `native/` — Pivox operator application. Stack:

- macOS: SwiftUI + AppKit bridges where SwiftUI is too weak (image
  editor, terminal, complex transcripts). Swift 6 language mode +
  Swift-C++ interop for the shared core.
- Windows: WinUI 3 + C++/WinRT.
- Shared core: C++ (Swift-C++ interop on macOS, C++/WinRT on Windows).
- Generated proto: SwiftProtobuf + grpc-swift-2 (see
  `native/platform/macos/swift-packages/PivoxModels/`).

This doc is a stub. Full conventions are still being written — until
they are, defer to the Go backend's `AGENTS.md` for cross-cutting
rules (component naming, code-quality bar, etc.) and to the patterns
established in the existing Swift code in `platform/macos/swift/`.

## Build

Read `docs/build.md` first. The native build is **not** `cmake
--build` — that builds all targets including broken UITests. Use:

```sh
cd native
xcodebuild build -project build-xcode/Pivox.xcodeproj -scheme Pivox \
  -configuration Debug -allowProvisioningUpdates
```

Regenerate the Xcode project after CMake changes:

```sh
cd native
cmake -G Xcode -B build-xcode -S .
```

Run tests with `xcodebuild test -scheme PivoxTests` — `ctest` is
latently broken.

## Logging

- Use `PivoxLog` (`platform/macos/swift/Logging/PivoxLog.swift`) —
  category-scoped `os.Logger` instances. `PivoxLog.chat`, `.auth`,
  `.sso`, `.transcript`, etc.
- `debugSensitive(_:)` for payloads that may contain tokens or PII.
  Compiles to a no-op in release builds.
- Never silently swallow errors in catch blocks. Even when the UX
  recovery is "set isLoading = false," log the error class +
  description before returning. Silent catches make production stalls
  invisible.

## SwiftUI vs AppKit

- HIG-compliant components first: `List(selection:)`, `NavigationSplitView`,
  `Form`. Don't roll a custom layout when a HIG primitive exists.
- Prepend-scroll preservation: `scrollTo` inside `onChange(of: count)`
  same-transaction. Diverging from this breaks scroll preservation
  on prepend.
- AppKit bridge when data is unbounded, heights are unknown, or
  SwiftUI's rendering taxes performance (transcripts, image editor).
  Treat SwiftUI like JSX — fast for declarative composition, weak
  under heavy real-time updates.

## Generated proto

Every backend proto change requires `make proto-generate-native` from
the repo root. Don't edit `swift-packages/PivoxModels/Sources/.../Generated/`
files by hand.

## Naming

Don't prefix internal Swift types with `Pivox` — `ChatClient`, not
`PivoxChatClient`. We *are* the Pivox app.
