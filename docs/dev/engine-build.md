# Engine Build System

## Overview

The engine and its plugins are built using **Cargo (Rust) + CMake (C/C++) + vcpkg (C/C++ dependencies)**. Cargo is the top-level build driver — it invokes CMake via `build.rs`, which in turn uses vcpkg for C/C++ dependency management.

One command builds everything: `cargo build`.

**Related docs:**
- `docs/engine.md` — engine architecture
- `docs/plugins/` — individual plugin documentation
- `docs/licensing.md` — FFmpeg LGPL build constraints

## Prerequisites

### macOS (Dev)

```bash
brew install cmake rust
# vcpkg bootstraps automatically during setup
# Xcode Command Line Tools required (for C/C++ compiler)
```

### Windows (Dev)

```powershell
# Visual Studio 2022 with C++ workload
# Or: Build Tools for Visual Studio 2022
choco install cmake rust
# vcpkg bootstraps automatically during setup
```

### Linux (Production Build)

```bash
# Ubuntu 24.04 LTS
apt install build-essential cmake
curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh
# vcpkg bootstraps automatically during setup
# NVIDIA drivers + CUDA toolkit for NVDEC/NVENC
```

## Build Architecture

```
cargo build
  │
  ├── Rust crates (Cargo)
  │   ├── engine/supervisor
  │   ├── engine/compositor
  │   ├── engine/frame-pipeline
  │   ├── engine/mjpeg-preview
  │   ├── engine/recording
  │   └── engine/gpi
  │
  └── build.rs → invokes CMake
       │
       ├── CMake builds C/C++ targets
       │   ├── cef-host (links prebuilt CEF)
       │   ├── rive-plugin (links rive-runtime)
       │   ├── aja-output (links NTV2 SDK)
       │   └── ndi-output (links NDI SDK)
       │
       └── vcpkg provides
           ├── ffmpeg (libavformat, libavcodec, etc.)
           ├── grpc + protobuf
           └── other C/C++ deps
```

## Project Layout

```
engine/
├── Cargo.toml                  # Rust workspace root
├── build.rs                    # Invokes CMake from Cargo
├── CMakeLists.txt              # Root CMake — all C/C++ targets
├── vcpkg.json                  # vcpkg manifest (C/C++ deps)
│
├── cmake/
│   ├── FindCEF.cmake           # Locate downloaded CEF binary distribution
│   ├── FindRive.cmake          # Locate Rive runtime
│   └── FindNTV2.cmake          # Locate AJA NTV2 SDK
│
├── cef-host/                   # C++ — CEF plugin
│   ├── CMakeLists.txt
│   └── src/
│       ├── cef_app.cpp         # CefApp implementation
│       ├── cef_client.cpp      # CefClient, CefRenderHandler (OSR)
│       ├── cef_v8_handler.cpp  # V8 native bindings (→ Rust via FFI)
│       ├── cef_audio.cpp       # CefAudioHandler (→ Rust via FFI)
│       └── plugin_interface.c  # C ABI function pointer table
│
├── rive-plugin/                # C++ — Rive plugin
│   ├── CMakeLists.txt
│   └── src/
│       ├── rive_renderer.cpp   # Rive runtime init, render loop
│       ├── rive_commands.cpp   # Command → state machine mapping
│       └── plugin_interface.c  # C ABI function pointer table
│
├── aja-output/                 # C++ — AJA NTV2 adapter
│   ├── CMakeLists.txt
│   └── src/
│       ├── aja_card.cpp        # CNTV2Card, AutoCirculate
│       ├── aja_gpi.cpp         # GPI input/output
│       └── aja_vanc.cpp        # VANC caption embedding
│
├── ndi-output/                 # C++ — NDI adapter
│   ├── CMakeLists.txt
│   └── src/
│       └── ndi_sender.cpp      # NDIlib_send
│
├── supervisor/                 # Rust — process manager
│   ├── Cargo.toml
│   └── src/
│
├── compositor/                 # Rust — layer compositing
│   ├── Cargo.toml
│   └── src/
│
├── frame-pipeline/             # Rust — colorspace, fill+key
│   ├── Cargo.toml
│   └── src/
│
├── video-engine/               # Rust — FFmpeg wrapper
│   ├── Cargo.toml
│   └── src/
│
├── mjpeg-preview/              # Rust — MJPEG HTTP server
│   ├── Cargo.toml
│   └── src/
│
├── recording/                  # Rust — compliance recording
│   ├── Cargo.toml
│   └── src/
│
├── captions/                   # Rust — CC extraction/embedding
│   ├── Cargo.toml
│   └── src/
│
└── sdk-inject/                 # JavaScript — PivoxSDK + browser mock
    ├── package.json
    └── src/
```

## External Dependencies

### CEF — Prebuilt Binary Distribution

CEF is **not** built from source. Prebuilt binaries are downloaded from [Spotify's CEF build service](https://cef-builds.spotifycdn.com/index.html).

```bash
# scripts/download-cef.sh
CEF_VERSION="130.0.13+g2826558+chromium-130.0.6723.70"

# Download for current platform
curl -O "https://cef-builds.spotifycdn.com/cef_binary_${CEF_VERSION}_linux64.tar.bz2"
tar xf cef_binary_*.tar.bz2 -C third_party/cef/

# Build the thin C++ wrapper (~30 seconds)
cd third_party/cef/libcef_dll_wrapper
cmake . && make
```

The `FindCEF.cmake` module locates the downloaded distribution:

```cmake
# cmake/FindCEF.cmake
set(CEF_ROOT "${CMAKE_SOURCE_DIR}/third_party/cef")
find_library(CEF_LIB cef PATHS "${CEF_ROOT}/Release")
set(CEF_INCLUDE_DIR "${CEF_ROOT}/include")
```

**Version pinning:**

```yaml
# CEF version tracked in project config
cef:
  version: "130.0.13+g2826558+chromium-130.0.6723.70"
  platforms:
    linux64: "cef_binary_..._linux64.tar.bz2"
    macos64: "cef_binary_..._macos64.tar.bz2"
    macosarm64: "cef_binary_..._macosarm64.tar.bz2"
    windows64: "cef_binary_..._windows64.tar.bz2"
```

**Update process:**
1. New Chromium stable released
2. Spotify builds service publishes prebuilt CEF binaries (~1-2 weeks later)
3. Update version in config
4. CI downloads new binaries, builds, runs test suite
5. Tests pass → merge. Tests fail → investigate, stay on current version.

### Rive — rive-runtime

```bash
# Clone rive-runtime
git clone https://github.com/rive-app/rive-runtime.git third_party/rive/
cd third_party/rive/

# Rive uses premake5 — we wrap it in CMake
# cmake/FindRive.cmake handles this
```

Alternatively, build Rive as part of our CMake tree by adding its source files directly. The runtime is MIT-licensed.

### AJA NTV2 SDK

```bash
# Clone NTV2
git clone https://github.com/aja-video/ntv2.git third_party/ntv2/
```

NTV2 has its own CMake build. `FindNTV2.cmake` locates it. Only needed on machines with AJA hardware — the build is conditional.

### NDI SDK

Downloaded from [NDI SDK downloads](https://ndi.video/download-ndi-sdk/). Binary distribution with headers. `FindNDI.cmake` locates the installed SDK.

### FFmpeg — via vcpkg

```json
// vcpkg.json
{
  "name": "pivox-engine",
  "version": "0.1.0",
  "dependencies": [
    {
      "name": "ffmpeg",
      "features": [
        "avcodec", "avformat", "avutil", "swscale", "swresample",
        "nvcodec"
      ]
    }
  ]
}
```

**LGPL compliance:** vcpkg builds FFmpeg with shared libraries by default. Do not add GPL-licensed features. See `docs/licensing.md` for the exact flags.

## How Cargo Invokes CMake

```rust
// engine/build.rs
fn main() {
    // Build all C/C++ targets via CMake
    let dst = cmake::Config::new(".")
        .define("CMAKE_TOOLCHAIN_FILE", vcpkg_toolchain_path())
        .build();

    // Link Rust against the C/C++ shared libraries
    println!("cargo:rustc-link-search={}/lib", dst.display());
    println!("cargo:rustc-link-lib=dylib=pivox_cef_plugin");
    println!("cargo:rustc-link-lib=dylib=pivox_rive_plugin");
    println!("cargo:rustc-link-lib=dylib=pivox_aja");
    println!("cargo:rustc-link-lib=dylib=pivox_ndi");

    // FFmpeg (via vcpkg)
    println!("cargo:rustc-link-lib=dylib=avformat");
    println!("cargo:rustc-link-lib=dylib=avcodec");
    println!("cargo:rustc-link-lib=dylib=avutil");
    println!("cargo:rustc-link-lib=dylib=swscale");
}

fn vcpkg_toolchain_path() -> String {
    // vcpkg provides CMAKE_TOOLCHAIN_FILE for CMake integration
    format!("{}/scripts/buildsystems/vcpkg.cmake",
            std::env::var("VCPKG_ROOT").unwrap_or_else(|_| "third_party/vcpkg".into()))
}
```

## Build Commands

```bash
# Initial setup (download CEF, bootstrap vcpkg, clone third-party deps)
make setup

# Build everything (Rust + C/C++)
cargo build

# Build release
cargo build --release

# Build engine only (no Go control plane)
cargo build -p pivox-engine

# Build specific plugin
cargo build -p pivox-cef-plugin
cargo build -p pivox-rive-plugin

# Clean
cargo clean
```

### Makefile Targets

```makefile
# Root Makefile
setup:
	./scripts/download-cef.sh
	./scripts/bootstrap-vcpkg.sh
	git submodule update --init  # NTV2, Rive

build-engine:
	cargo build --release

build-server:
	cd cmd/pivox-server && go build

build-all: build-engine build-server

dev:
	# Start engine + control plane + Electron for development
	./scripts/dev.sh

test:
	cargo test
	cd cmd/pivox-server && go test ./...
```

## CI Build Matrix

| Platform | Engine | Control Plane | Notes |
|---|---|---|---|
| macOS arm64 | cargo build | go build | Dev build, no AJA |
| macOS x64 | cargo build | go build | Dev build, no AJA |
| Linux x64 | cargo build --release | go build | Production build, with AJA + NVDEC/NVENC |
| Windows x64 | cargo build | go build | Dev build, future prod |

CI downloads CEF binaries per platform, bootstraps vcpkg, and builds. Test suite runs on all platforms. AJA-specific tests only run on Linux CI runners with AJA hardware (or mocked).

## Conditional Compilation

Not every platform needs every component:

```cmake
# CMakeLists.txt
option(PIVOX_BUILD_AJA "Build AJA NTV2 output adapter" OFF)
option(PIVOX_BUILD_NDI "Build NDI output adapter" ON)
option(PIVOX_BUILD_RIVE "Build Rive plugin" ON)

if(PIVOX_BUILD_AJA)
    add_subdirectory(aja-output)
endif()
```

```rust
// Rust feature flags
// Cargo.toml
[features]
default = ["cef", "ffmpeg", "rive", "ndi", "mjpeg"]
aja = ["dep:pivox-aja"]
recording = ["dep:pivox-recording"]
```

- **Dev build (macOS):** CEF + FFmpeg + Rive + NDI + MJPEG. No AJA.
- **Production build (Linux):** Everything including AJA + recording.
- **Minimal build (testing):** FFmpeg + MJPEG only.

## Third-Party Directory

```
third_party/
├── cef/                # Downloaded CEF binary distribution
│   ├── include/
│   ├── Release/
│   ├── Resources/
│   └── libcef_dll_wrapper/
├── rive/               # Cloned rive-runtime
├── ntv2/               # Cloned AJA NTV2 SDK
├── ndi/                # Downloaded NDI SDK
└── vcpkg/              # Bootstrapped vcpkg (gitignored, recreated by setup)
```

All third-party dependencies are in `third_party/`. None are committed to the repo (except as git submodules for rive and ntv2). CEF, NDI, and vcpkg are downloaded by `make setup`.
