# Pivox Repository Structure

## Overview

Pivox is split across multiple repositories, each with a single concern and independent release cadence. All repos are private under the `dashkan` GitHub organization.

## Repositories

| Repo | Language | Purpose |
|---|---|---|
| `dashkan/pivox-docs` | Markdown | Architecture & design documentation |
| `dashkan/pivox-engine` | Rust + C/C++ | Core engine: supervisor, compositor, frame pipeline, output adapters (AJA, NDI, MJPEG, recording) |
| `dashkan/pivox-plugin-sdk` | Rust + C/C++ | Plugin interface: PivoxPlugin trait, C headers, C++ wrapper |
| `dashkan/pivox-plugin-cef` | C++ + Rust | CEF rendering plugin (HTML/JS graphics) |
| `dashkan/pivox-plugin-ffmpeg` | Rust + C | FFmpeg rendering plugin (video, audio, stills) |
| `dashkan/pivox-plugin-rive` | C++ + Rust | Rive rendering plugin (2D motion graphics) |
| `dashkan/pivox-proto` | Protobuf | Engine protocol definitions (playout commands, channel status, input, plugin protocol) |
| `dashkan/pivox-server` | Go | Control plane: NRCS, asset management, Data Plane, hardware automation, monitoring |
| `dashkan/pivox-web` | React + TypeScript | Operator UI (browser + Electron) |
| `dashkan/pivox-sdk-js` | TypeScript | JavaScript PivoxSDK + browser mock (npm package) |

## Dependency Graph

```
pivox-proto (protobuf definitions — engine protocol only)
  │
  ├──► pivox-plugin-sdk (plugin interface types)
  │     │
  │     ├──► pivox-engine (loads plugins via SDK interface)
  │     │     └── output adapters: AJA, NDI, MJPEG, recording (built-in)
  │     │
  │     ├──► pivox-plugin-cef (implements PivoxPlugin)
  │     ├──► pivox-plugin-ffmpeg (implements PivoxPlugin)
  │     └──► pivox-plugin-rive (implements PivoxPlugin)
  │
  ├──► pivox-server (Go — generates Go code from proto)
  │
  └──► pivox-sdk-js (JS SDK uses proto message shapes)

pivox-web ──► pivox-sdk-js (npm dependency)

pivox-docs (standalone, no code dependencies)
```

## Dependency Management

**No git submodules.** Use native dependency managers:

- **Rust plugins → engine:** Cargo git dependencies with version tags
  ```toml
  [dependencies]
  pivox-plugin-sdk = { git = "ssh://git@github.com/dashkan/pivox-plugin-sdk.git", tag = "v0.1.0" }
  ```

- **C/C++ deps:** CMake `FetchContent` for plugin repos, vcpkg for third-party C/C++ libraries

- **Proto → Rust/Go:** Cargo/Go git dependency, or build script that clones proto repo at specific tag

- **JS SDK → Web:** npm package (`@pivox/sdk-js`)

## What Each Repo Contains

### pivox-docs

All cross-cutting architecture and design documentation. Not code-specific build docs — those live in each code repo.

```
pivox-docs/
  ├── architecture.md
  ├── engine.md
  ├── control-plane.md
  ├── data-plane.md
  ├── sdk.md
  ├── protocols.md
  ├── hardware.md
  ├── templates.md
  ├── tooling.md
  ├── licensing.md
  ├── glossary.md
  └── plugins/
      ├── plugin-sdk.md
      ├── plugin-cef.md
      ├── plugin-ffmpeg.md
      ├── plugin-rive.md
      └── plugin-unreal.md
```

### pivox-engine

Core engine runtime. Output adapters (AJA, NDI, MJPEG, recording) are built-in — they consume composited frames, not a plugin concern.

```
pivox-engine/
  ├── Cargo.toml
  ├── CMakeLists.txt
  ├── vcpkg.json
  ├── src/
  │   ├── supervisor/
  │   ├── compositor/
  │   ├── frame_pipeline/
  │   ├── audio_mixer/
  │   ├── plugin_host/          # Loads plugins via C ABI
  │   ├── shared_memory/        # Data Plane shared memory writer
  │   ├── output/
  │   │   ├── aja/              # AJA NTV2 (C++ via FFI)
  │   │   ├── ndi/              # NDI (C++ via FFI)
  │   │   ├── mjpeg/            # MJPEG HTTP preview
  │   │   └── recording/        # Compliance recording (NVENC)
  │   ├── captions/
  │   └── gpi/
  ├── docs/dev/
  │   └── engine-build.md
  └── README.md
```

### pivox-plugin-sdk

The shared interface that all plugins implement. Published as a Rust crate + C/C++ headers.

```
pivox-plugin-sdk/
  ├── rust/
  │   ├── Cargo.toml
  │   └── src/lib.rs            # PivoxPlugin trait, PluginCapabilities
  ├── c/
  │   ├── pivox-plugin.h        # C ABI function pointer table
  │   └── pivox-plugin-types.h
  ├── cpp/
  │   ├── pivox-plugin.hpp      # C++ abstract class wrapper
  │   └── pivox-plugin-macros.h
  └── README.md
```

### pivox-plugin-cef

```
pivox-plugin-cef/
  ├── Cargo.toml
  ├── CMakeLists.txt
  ├── cmake/FindCEF.cmake
  ├── src/
  │   ├── cef_app.cpp
  │   ├── cef_client.cpp
  │   ├── cef_v8_handler.cpp
  │   ├── cef_audio.cpp
  │   ├── plugin_interface.c
  │   └── lib.rs
  ├── scripts/download-cef.sh
  └── README.md
```

### pivox-plugin-ffmpeg

```
pivox-plugin-ffmpeg/
  ├── Cargo.toml
  ├── vcpkg.json                # FFmpeg via vcpkg
  ├── src/
  │   ├── decoder.rs
  │   ├── playback.rs
  │   ├── audio.rs
  │   ├── captions.rs
  │   └── lib.rs
  └── README.md
```

### pivox-plugin-rive

```
pivox-plugin-rive/
  ├── Cargo.toml
  ├── CMakeLists.txt
  ├── cmake/FindRive.cmake
  ├── src/
  │   ├── rive_renderer.cpp
  │   ├── rive_commands.cpp
  │   ├── plugin_interface.c
  │   └── lib.rs
  └── README.md
```

### pivox-proto

```
pivox-proto/
  ├── playout.proto
  ├── channel.proto
  ├── input.proto
  ├── plugin.proto
  └── README.md
```

Engine protocol only. Server REST/gRPC API definitions live in `pivox-server`.

### pivox-server

```
pivox-server/
  ├── go.mod
  ├── cmd/
  │   ├── pivox-server/
  │   └── pivox-mos-gateway/
  ├── internal/
  │   ├── api/
  │   ├── playout/
  │   ├── nrcs/
  │   ├── assets/
  │   ├── dataplane/
  │   ├── timers/
  │   ├── redundancy/
  │   ├── recording/
  │   ├── monitoring/
  │   ├── hardware/
  │   ├── mos/
  │   ├── vdcp/
  │   ├── auth/
  │   └── config/
  ├── api/proto/              # Server-specific gRPC (if any)
  └── README.md
```

### pivox-web

```
pivox-web/
  ├── package.json
  ├── src/                    # Shared React + TypeScript
  ├── electron/               # Electron shell
  ├── scripts/
  │   ├── build-web.sh
  │   └── build-electron.sh
  └── README.md
```

### pivox-sdk-js

```
pivox-sdk-js/
  ├── package.json            # @pivox/sdk
  ├── src/
  │   ├── model.ts            # pivox.model (view model bindings)
  │   ├── feeds.ts            # pivox.feeds (shared memory subscriptions)
  │   ├── native.ts           # pivox.native (V8 binding interface)
  │   ├── system.ts           # pivox.system (time, timecode, channel)
  │   ├── assets.ts           # pivox.assets (resolve, preload)
  │   ├── timing.ts           # pivox.timing (genlock-synced rAF)
  │   └── index.ts            # Main export
  ├── mock/
  │   ├── pivox-sdk-mock.ts   # Browser mock for development
  │   └── index.ts
  └── README.md
```

## Versioning Strategy

### pivox-plugin-sdk — Compatibility Anchor

The Plugin SDK version defines compatibility between engine and plugins.

| Change | SDK Version Bump | Impact |
|---|---|---|
| New optional field in PluginCapabilities | Patch | No forced plugin updates |
| New optional callback in PivoxPlugin | Minor | Old plugins still work (default no-op) |
| Breaking change to PivoxPlugin trait | Major | All plugins must update |

Engine declares which SDK versions it supports (range). Plugins declare which SDK version they built against. Engine refuses to load incompatible plugins.

### pivox-proto — Coordinated Releases

Breaking proto changes require coordinated releases of `pivox-engine` and `pivox-server`. Use proto best practices:
- Never renumber or remove fields — deprecate them
- Add new fields as optional
- New RPCs are additive

### Everything Else — Independent

Each repo versions independently. A CEF plugin update doesn't require an engine update (as long as SDK version is compatible). A server update doesn't require an engine update (as long as proto version is compatible).

## Release Artifacts

| Repo | Artifact | Distribution |
|---|---|---|
| `pivox-engine` | Binary + bundled plugin shared libs | GitHub releases |
| `pivox-plugin-cef` | `libpivox_cef.so/dylib/dll` | Bundled with engine release |
| `pivox-plugin-ffmpeg` | `libpivox_ffmpeg.so/dylib/dll` | Bundled with engine release |
| `pivox-plugin-rive` | `libpivox_rive.so/dylib/dll` | Bundled with engine release |
| `pivox-plugin-sdk` | Rust crate + C/C++ headers | crates.io (private) + GitHub releases |
| `pivox-server` | Go binary | GitHub releases, Docker image |
| `pivox-web` | Static assets + Electron app | Bundled with server, GitHub releases |
| `pivox-sdk-js` | npm package `@pivox/sdk` | npm (private registry or GitHub packages) |
| `pivox-proto` | `.proto` files | GitHub releases (tagged) |
| `pivox-docs` | Markdown | GitHub Pages or docs site |

The **engine installer** bundles: engine binary + all plugin shared libraries + FFmpeg libs + CEF distribution + Rive runtime. One download.

## Development Order

### Phase 1a — Foundation (macOS, NDI output)

Build order — each step produces a testable artifact:

**Step 1: pivox-proto + pivox-plugin-sdk**
- Define the protobuf schemas (playout commands, channel status)
- Define the PivoxPlugin trait (C ABI + Rust trait)
- This is the contract everything else builds against
- Testable: compile, type-check

**Step 2: pivox-engine (core — no plugins yet)**
- Supervisor: spawn/manage channel processes
- Plugin host: load plugins via C ABI, call render_frame()
- Compositor: alpha-blend N RGBA buffers
- Audio mixer: mix N PCM streams
- Frame pipeline: colorspace conversion (CPU SIMD), fill+key split
- NDI output: send composited frames via NDI
- MJPEG preview: HTTP server for browser preview
- Shared memory writer: receive Data Plane feed stream, write to /dev/shm/
- gRPC endpoint: accept commands from control plane
- Testable: engine starts, accepts gRPC commands, outputs black frames via NDI and MJPEG

**Step 3: pivox-plugin-ffmpeg**
- FFmpeg decode pipeline (libavformat, libavcodec)
- Implement PivoxPlugin trait
- Video playback: load, play, pause, seek, variable speed
- Audio decode + delivery
- Still image support
- Testable: load a video clip, play it, see output on NDI

**Step 4: pivox-plugin-cef**
- CEF OSR initialization
- Implement PivoxPlugin trait
- SDK injection (pivox.model, pivox.timing, pivox.ready/done)
- Native V8 bindings (pivox.native.getTimecode, getAudioLevels, etc.)
- Template loading (LoadCommand → CEF loads HTML)
- Testable: load an HTML template, play it, see graphics on NDI composited over video

**Step 5: pivox-plugin-rive**
- Rive C/C++ runtime initialization
- Implement PivoxPlugin trait
- Command → state machine mapping
- Testable: load a .riv file, play animation, see on NDI

**Step 6: pivox-sdk-js**
- PivoxSDK source (injected by CEF plugin)
- Browser mock (pivox-sdk-mock.js)
- Testable: template dev workflow — mock in browser, real SDK in engine

**Step 7: Integration + Data Plane**
- Shared memory feeds (pivox.feeds subscriptions)
- Foreground/background slots
- Transitions (cut, mix/dissolve)
- Remote input interaction
- Audio pipeline (per-layer volume, AFV, delay comp)
- Testable: full Phase 1a feature set working on macOS via NDI

### Phase 1b — AJA SDI Output (Linux)

- AJA NTV2 output adapter in pivox-engine
- Genlock synchronization
- GPI via AJA card
- Caption pass-through (FFmpeg → AJA VANC)
- Compliance recording (NVENC)
- Testable: SDI fill+key output on Linux with AJA card

### Later Phases

- pivox-server (Phase 1 CP, then iterative)
- pivox-web (parallel with server)
- pivox-docs already exists (this repo)

## Repo Creation Order

1. `pivox-docs` — move docs from current repo immediately
2. `pivox-proto` — define schemas before writing code
3. `pivox-plugin-sdk` — define the interface before implementing it
4. `pivox-engine` — core engine
5. `pivox-plugin-ffmpeg` — first plugin to validate the SDK works
6. `pivox-plugin-cef` — second plugin
7. `pivox-plugin-rive` — third plugin
8. `pivox-sdk-js` — JavaScript SDK
9. `pivox-server` — control plane (can start in parallel with engine work)
10. `pivox-web` — operator UI (can start once server API is defined)
