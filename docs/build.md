# Build Guide

## Prerequisites

All platforms:
- **CMake** 4.0+
- **vcpkg** (installed, `VCPKG_ROOT` set or `vcpkg` in PATH)
- **Go** 1.26+
- **Rust** 1.94+ (stable)
- **buf** 1.60+ (proto toolchain)
- **Node.js** 20+ (web app + Aspire AppHost tooling)

macOS additionally:
- **Xcode** 26+ (Swift 6, Metal, XCTest)

Windows additionally:
- **Visual Studio** 2026+ with C++ desktop workload

## Cloud Controller (Go backend)

### Build

```sh
make build          # builds bin/pivox-cloud + bin/pivox-agent
make build-dev      # same, with dev build tag
```

### Database

Requires PostgreSQL with pgvector extension.

```sh
make db-create      # create pivox database
make db-up          # run migrations
make db-seed        # seed development data
```

Reset:

```sh
make db-drop        # drops the database (kills active connections first)
make db-create
make db-up
make db-seed
```

### Run

```sh
make run-server     # go run ./cmd/pivox-cloud serve
```

Default ports: gRPC `:50051`, REST `:8080`, debug `:9090`.

Flags:
- `--ollama-url` (default `http://localhost:11434`)
- `--ollama-model` (default `qwen3-vl`)
- `--database-url` (default `postgres://localhost:5432/pivox?sslmode=disable`)

### Proto codegen

```sh
make proto-generate         # Go + gateway + OpenAPI
buf generate --template buf.gen.swift.yaml   # Swift (PivoxModels)
```

### Linting

```sh
make lint-proto     # buf lint
make api-lint       # Google AIP compliance
```

### Tests

```sh
make test       # brings up the docker-compose Postgres + rustfs stack
                # (docker-compose.test.yml) and runs the suite. Idempotent.
make test-down  # tear the compose stack down
```

## Native App — macOS

### Tool setup

Install via Homebrew:

```sh
brew install cmake vcpkg
```

Rust (if not installed):

```sh
curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh
```

Verify:

```sh
cmake --version    # 4.0+
vcpkg --version
rustc --version    # 1.94+
xcodebuild -version  # Xcode 26+
```

### Generate the Xcode project

From the `native/` directory:

```sh
cd native
cmake -G Xcode -B build-xcode -S .
```

This will:
1. Install vcpkg dependencies (gtest, grpc, protobuf, cmark-gfm)
2. Fetch Corrosion (Rust/CMake bridge) and build the tree-sitter syntax highlighter
3. Download Firebase Apple SDK xcframeworks
4. Compile SwiftProtobuf from source as a CMake Swift static library
5. Generate `build-xcode/Pivox.xcodeproj`

First run takes a few minutes (vcpkg compiles grpc from source). Subsequent runs are fast.

### Build from command line

```sh
xcodebuild build \
  -project build-xcode/Pivox.xcodeproj \
  -scheme Pivox \
  -configuration Debug \
  -allowProvisioningUpdates
```

### Build from Xcode

Open `native/build-xcode/Pivox.xcodeproj`, select the **Pivox** scheme, and build (⌘B).

### Run

```sh
open build-xcode/Debug/Pivox.app
```

Or press ⌘R in Xcode.

The app requires:
- Cloud Controller running on `localhost:50051`
- Ollama running on `localhost:11434` with `qwen3-vl` model pulled
- Firebase project configured (for auth)

### Run tests

**Swift (XCTest):**

```sh
xcodebuild test \
  -project build-xcode/Pivox.xcodeproj \
  -scheme PivoxTests \
  -configuration Debug \
  -destination 'platform=macOS'
```

**C++ (gtest):**

```sh
xcodebuild build \
  -project build-xcode/Pivox.xcodeproj \
  -target pivox_markdown_tests \
  -configuration Debug

./build-xcode/core/ai_elements/markdown/Debug/pivox_markdown_tests
```

### Regenerate after CMake changes

If you modify `CMakeLists.txt`, `vcpkg.json`, or add/remove source files:

```sh
cmake -G Xcode -B build-xcode -S .
```

Xcode caches aggressively. If changes don't take effect:

```sh
xcodebuild clean build -project build-xcode/Pivox.xcodeproj -scheme Pivox ...
```

### Swift proto codegen (PivoxModels)

When proto files change, regenerate Swift types:

```sh
cd /path/to/pivox
buf generate --template buf.gen.swift.yaml
```

Output: `native/platform/macos/swift-packages/PivoxModels/Sources/PivoxModels/Generated/`

The PivoxModels SPM package can also be built standalone:

```sh
cd native/platform/macos/swift-packages/PivoxModels
swift build
```

### Project structure

```
native/
├── CMakeLists.txt              Root build config
├── CMakePresets.json            Windows presets
├── vcpkg.json                  C/C++ dependencies
├── core/
│   ├── ai_chat/client/         gRPC bidi client (C++ + C FFI)
│   ├── ai_elements/
│   │   ├── highlight/          Rust syntax highlighter (Corrosion)
│   │   └── markdown/           cmark-gfm parser (C++)
│   ├── image-editor/           Image editor engine (C++)
│   └── *.h                     Shared types
├── platform/
│   ├── macos/
│   │   ├── swift/              SwiftUI app code
│   │   │   ├── App/            Main app, ContentView
│   │   │   ├── Auth/           Firebase auth
│   │   │   ├── AIChat/         AI chat views + client
│   │   │   ├── AIElements/     Component library foundation
│   │   │   └── ...
│   │   ├── objcpp/             Obj-C++ bridge (C++ → Swift)
│   │   ├── swift-packages/     SPM packages (PivoxModels)
│   │   └── Bridging-Header.h
│   └── windows/                WinUI 3 (C++/WinRT)
├── tests/
│   ├── macos/                  XCTest + XCUITest
│   └── core/                   gtest for shared C++
└── third_party/                Firebase xcframeworks (auto-downloaded)
```
