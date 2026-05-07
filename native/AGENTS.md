# Native App — agent conventions

Scope: `native/` — Pivox operator application. Stack:

- macOS: SwiftUI + AppKit bridges where SwiftUI is too weak (image
  editor, terminal, complex transcripts). Swift 6 language mode +
  Swift-C++ interop for the shared core.
- Windows: WinUI 3 + C++/WinRT.
- Shared core: C++ (Swift-C++ interop on macOS, C++/WinRT on Windows).
- Generated proto: SwiftProtobuf + grpc-swift-2 (see
  `native/platform/macos/swift-packages/PivoxModels/`).

Defer to the Go backend's `AGENTS.md` for cross-cutting rules
(component naming, code-quality bar, etc.) that aren't restated here.

## Source layout

Top-level folders under `platform/macos/swift/` are layers, not
arbitrary categories. Each layer has a defined dependency surface,
enforced on review (one Xcode target, no compiler enforcement).

```
App/                 composition root — depends on anything
Core/                app-wide layer — depends on nothing in the app
  Foundation/        primitives: Theme, Buttons, Tooltip, Layout,
                     Avatar, Inputs, Effects, Logging
AIElements/          Vercel AI Elements 1:1 layer — depends on Core
  Foundation/        AI-Elements-specific primitives:
                     Markdown, Highlight, Tooltip
  Components/        the elements themselves: Message/, future
                     Reasoning/, Tool/, etc.
AIChat/              feature — depends on Core, AIElements
  AIChatService.swift, AIChatState.swift   (feature-root: app-lifetime
                                            singletons + observable state)
  Client/              ChatClient.swift    (gRPC client wrapper)
  Window/              window/panel chrome (AIChatWindow, InlineAIChatPanel,
                       AIChatContainerView, ChatResizeHandle)
  Transcript/          conversation rendering + composer
  History/             conversation list + popover
Auth/                feature — depends on Core
ImageEditor/         feature — depends on Core
Settings/            feature — depends on Core
```

### Dependency-direction rule

Code may depend **downward**, never **upward**, never **sideways**.

- `App/` may depend on anything (it composes the app).
- `AIChat/` may depend on `Core/` and `AIElements/`.
- `AIElements/` may depend on `Core/` only.
- Other features (`Auth/`, `ImageEditor/`, `Settings/`) may depend on
  `Core/` only.
- `Core/` depends on nothing in the project.
- **No feature depends on another feature.** AIChat doesn't import
  from Auth, Auth doesn't import from Settings, etc.

Cross-cutting types that two features need to share belong in `Core/`,
not duplicated and not stuffed into one feature folder for the other
to reach into. If `Core/` looks like the wrong home for the shared
type, the answer is usually that the shared type should be a value
passed by `App/` rather than imported globally.

### Multi-foundation pattern

Both `Core/Foundation/` and `AIElements/Foundation/` exist. The split
is intentional, not a duplication:

- **`Core/Foundation/`** — primitives that any feature might need.
  Theme, generic buttons, tooltips, logging, layout containers, input
  fields. App-wide.
- **`AIElements/Foundation/`** — primitives that exclusively serve
  the AI Elements layer. Markdown rendering, syntax highlighting, the
  rich hover tooltip used by chat. If a non-chat consumer needs one
  of these later, promote it to `Core/Foundation/` at that time
  (`git mv` plus a few imports — cheap).

The pattern generalizes: any layer in the dependency graph may have
its own internal `Foundation/` for primitives scoped to that layer.
This is logical modularization without paying SwiftPM's cost.

### When a file's home isn't obvious

Decision tree:

1. Is it a primitive used by ≥2 features across feature boundaries?
   → `Core/Foundation/<category>/`
2. Does it exclusively serve AIElements?
   → `AIElements/Foundation/<category>/` or `AIElements/Components/<element>/`
3. Is it specific to a single feature?
   → That feature's folder.
4. Is it composing the app shell (window mgmt, top-level lifecycle)?
   → `App/`

If the file naturally pulls in dependencies from multiple features,
that's a sign you're conflating things — split the file.

### SwiftPM modules

We do **not** split into SwiftPM modules at current scale (~14k LOC,
single Xcode target). The folder boundaries above ARE the
modularization. Promoting a layer to a SwiftPM package costs CMake
plumbing, modulemap fragility, and the loss of `internal` access; the
benefits (faster preview rebuild, hard team boundaries, public-API
discipline) don't apply at this size.

If preview rebuilds become slow enough to break flow, OR an
extraction target appears (e.g., open-sourcing AIElements as a
package), revisit. Until then: folders.

### Cross-platform intent

The macOS app is the MVP. iOS/iPadOS port is planned (selective —
not every feature). Windows port is planned via WinUI 3 + .NET, with
shared C++ logic exposed as WinRT components consumed via C#/WinRT
projection (P/Invoke only for stateless C-ABI helpers).

For Swift code today, this means:

- **Theme tokens are stored as SwiftUI `Color`** (universal type).
  AppKit consumers convert at their boundary: `NSColor(theme.foo)`.
  iOS port flips that to `UIColor(theme.foo)` and the rest of the
  Swift stack doesn't move.
- **Avoid incidental `Color(nsColor: .somethingColor)` in pure SwiftUI
  views.** Use a theme token instead. NSColor is appropriate at
  AppKit-component boundaries (NSTableView, CGContext drawing,
  dynamic-color providers), not in SwiftUI views that have no AppKit
  hunger.
- **Files in `Foundation/` aspire to universal SwiftUI APIs.** When
  AppKit bridging is needed (e.g., `NSViewRepresentable` wrapping an
  AppKit primitive that has a UIKit analog), the public API stays
  SwiftUI-facing; the bridge implementation can be conditional-
  compiled (`#if canImport(AppKit)` / `#if canImport(UIKit)`) within
  the same file for short bridges, or split into platform-suffixed
  pairs (`Foo+macOS.swift`, `Foo+iOS.swift`) when implementations
  diverge enough that `#if` blocks become unreadable.
- **Files with no clean iOS analog** (e.g., things that depend on
  `NSWindow`'s detached-panel semantics, `NSCursor`, `NSEvent`'s
  AppKit-specific tracking modes) belong in feature folders, not in
  `Foundation/`. iOS port for those is a redesign, not a port.
- **Don't write iOS branches today.** Code that's never been
  compiled rots. Mark the slot with intent; fill at port time.

`Theme.swift` is the iOS port's first surgery — it sources values
from AppKit semantic colors (`NSColor.labelColor`, etc.) that lack
universal SwiftUI cross-platform names. At port time, conditionalize
the constructor with `#if canImport(AppKit) / canImport(UIKit)`.

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

### SwiftUI Previews

CMake-generated Xcode projects do not populate `SWIFT_OPTIMIZATION_LEVEL`
or `SWIFT_COMPILATION_MODE` by default. SwiftUI Previews read those
build settings directly (not `OTHER_SWIFT_FLAGS`), and treat unset ==
`-O` — Previews then refuse with *"needs an unoptimized build … current
setting is '-O'"* even when `OTHER_SWIFT_FLAGS = -Onone` is in effect.
`CMakeLists.txt` pins both per variant:

```cmake
set(CMAKE_XCODE_ATTRIBUTE_SWIFT_OPTIMIZATION_LEVEL[variant=Debug]   "-Onone")
set(CMAKE_XCODE_ATTRIBUTE_SWIFT_OPTIMIZATION_LEVEL[variant=Release] "-O")
set(CMAKE_XCODE_ATTRIBUTE_SWIFT_COMPILATION_MODE[variant=Debug]     "singlefile")
set(CMAKE_XCODE_ATTRIBUTE_SWIFT_COMPILATION_MODE[variant=Release]   "wholemodule")
```

Don't try to fix Previews via `OTHER_SWIFT_FLAGS`; the Preview engine
doesn't look there. `wholemodule` (`-wmo`) in Debug also breaks
Previews independent of optimization level — keep Debug on `singlefile`.

The Preview engine also rejects the build if any embedded dylib in the
app bundle was compiled with `-O`. Every dylib reachable from a
previewed view must be the debug variant. PivoxModels is built in both
configs (`debug/` + `release/`) at configure time and the active Xcode
configuration picks one via `$<CONFIG>` generator expressions — see
the `PIVOX_MODELS_BUILD_DIR` block in `CMakeLists.txt`.

Stale `xcodebuild test` runs can leave Apple's XCTest support
frameworks in `Pivox.app/Contents/Frameworks/` (they're precompiled
with `-O`). If Previews fail and the diagnosis is unclear, check that
folder — non-CMake-managed dylibs there are cruft and can be deleted;
`xcodebuild build` won't recreate them.

## Logging

- Use `PivoxLog` (`Core/Foundation/Logging/PivoxLog.swift`) —
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
