# Native App Architecture

## Overview

The Pivox operator application is a **native desktop app** built with platform-specific UI frameworks — **SwiftUI on macOS** and **WinUI 3 on Windows**. It replaces the previous Electron-based approach with a native outer shell while retaining CEF for HTML viewport rendering (template preview, design canvas).

The architecture follows a principle: **simple UI is native, complex UI is shared**. Standard controls (menus, toolbars, property panels, forms) use each platform's native toolkit for authentic look and feel. Custom-drawn components (timeline editor, waveform display, channel preview tiles) are written once in shared C++ (or Rust) and hosted as native views on both platforms.

Broadcast operators expect native-feeling tools. Vizrt Trio, Ross XPression, and Grass Valley's tooling are all native apps. Electron would feel wrong to this audience.

## Architecture Layers

```
+-----------------------------------------------+
| Native Shell (SwiftUI / WinUI 3)              |
|  - Window management, menus, panel layout     |
|  - Property inspectors, forms, settings       |
|  - Role-based workspace switching             |
|  - Native drag-and-drop, multi-monitor        |
+-----------------------------------------------+
| Shared Custom UI (C++ or Rust, GPU-drawn)     |
|  - Timeline / rundown editor                  |
|  - Waveform / audio meters                    |
|  - Transition preview                         |
|  - Any custom-drawn widget                    |
|  Hosted as native views (NSView / HWND)       |
+-----------------------------------------------+
| CEF Viewports (HTML/JS)                       |
|  - Template preview / design canvas           |
|  - Embedded via SetAsChild (mac) / OSR (win)  |
+-----------------------------------------------+
| Shared C++ Core                               |
|  - gRPC client (engine + control plane)       |
|  - Document model, undo/redo                  |
|  - NDI receive for preview                    |
|  - Auth manager                               |
|  - State management                           |
+-----------------------------------------------+
```

## Native vs Shared Decision Framework

The guiding heuristic: **if the component doesn't exist in the platform's widget library, it's shared.**

### Native (per-platform, built twice)

| Component | Why Native |
|---|---|
| Window chrome, menus, toolbars | Must feel like the OS |
| Property inspector panels | Standard form controls (text fields, dropdowns, sliders, checkboxes) |
| Settings / preferences | Platform conventions differ significantly |
| File dialogs, clipboard, notifications | OS APIs |
| Panel layout, tab bars, split views | Platform layout primitives |
| Grid / tile layouts (e.g., channel monitor grid) | Standard collection views with native drag-to-reorder |

### Shared (written once, hosted on both platforms)

| Component | Why Shared |
|---|---|
| Timeline / rundown editor | Totally custom — no native equivalent on either platform |
| Channel preview tile content | Live video frame rendering (NDI receive or MJPEG) |
| Waveform / audio level meters | Custom GPU-drawn visualization |
| Transition preview | Custom rendering of transition effects |
| Node graph editor (future) | Custom interactive canvas |
| Data feed grid with real-time updates | Custom rendering for high-frequency data |

### CEF Viewports (HTML/JS, write once)

| Component | Why CEF |
|---|---|
| Template preview / design canvas | Renders the same HTML templates the engine renders |
| Template editor WYSIWYG (future) | Full browser environment for DOM manipulation |

### Composition Pattern

Many components are a mix. For example, the **channel monitor**:

- **Grid layout** of monitor tiles — native (SwiftUI `LazyVGrid` / WinUI `GridView`)
- **Each tile's chrome** (channel label, status indicators, layer list) — native
- **Each tile's live preview content** (the actual video frame) — shared custom-drawn component
- **Audio meters** alongside the preview — shared custom-drawn component

The native layer composes shared components as subviews. The shared component receives a native view handle (`NSView*` on macOS, `HWND` or `SwapChainPanel` on Windows) and renders into it.

## Shared Custom UI — Implementation Options

For GPU-drawn shared components, three options were evaluated:

**C++ with platform graphics abstraction (Metal + D3D11)**
- Maximum control, lowest overhead
- Proven in the POC — D3D11 `SwapChainPanel` integration working on Windows, `NSView` on macOS
- What broadcast tools traditionally use
- Con: maintaining two graphics backends (Metal, D3D11)

**Rust + wgpu**
- Cross-platform GPU abstraction — compiles to Metal on macOS, D3D11/D3D12 on Windows
- Write rendering code once
- If the engine is in Rust, sharing rendering code between engine and UI tools is a real advantage
- FFI to Swift and C++ is well-established
- Con: Rust learning curve

**Skia (via C++)**
- Google's 2D graphics library (powers Chrome, Flutter, Android)
- Excellent for UI widgets — text layout, anti-aliased vector drawing, GPU-accelerated
- Well-suited for timeline editors, data grids, waveforms
- Con: large dependency

Decision deferred until more shared components are needed. The POC has proven feasibility with direct Metal/D3D11 — any of these options can be adopted later without changing the overall architecture.

## CEF Integration

### macOS — SetAsChild (Windowed)

CEF runs in windowed mode. The browser view is embedded as a child `NSView` inside the SwiftUI view hierarchy via `NSViewRepresentable`. An Obj-C++ bridge (`CEFBridge.mm`) wraps CEF for Swift consumption.

- External message pump driven by `Timer` at 60fps (`CefDoMessageLoopWork`)
- JavaScript bindings via `CefV8Handler` in the renderer subprocess
- 5 helper subprocess bundles (renderer, GPU, plugin, alerts, network)
- Local HTML loaded from app bundle
- Ad-hoc code signing to prevent keychain prompts

### Windows — Off-Screen Rendering (OSR)

**Critical finding: child HWNDs do not render inside WinUI 3.** DirectComposition paints over them. This is not CEF-specific — even plain Win32 `STATIC` controls are invisible inside WinUI 3 windows.

CEF runs in OSR mode. It renders to a pixel buffer, which is copied to a `WriteableBitmap` on an `Image` control (software path) or rendered via D3D11 shared textures to a `SwapChainPanel` (GPU path).

- Software path: BGRA buffer from `OnPaint()` → `WriteableBitmap`. Proven performant — WebGL runs at full speed
- GPU path: D3D11 shared texture from `OnAcceleratedPaint()` → `SwapChainPanel`. Working with initial rendering issues resolved
- `DispatcherQueue` timer at 16ms for external message pump
- Input forwarding: mouse via `PointerPressed/Released/Moved`, keyboard via `KeyDown` + `ToUnicode()` (WinUI `CharacterReceived` is unreliable)
- Cursor: custom `CefHostGrid` subclass exposes `ProtectedCursor()` (WinUI overrides Win32 `SetCursor`)
- Context menus: intercept CEF `RunContextMenu`, build native `MenuFlyout` from `CefMenuModel`

### JavaScript Bindings

Both platforms use the same renderer subprocess (`renderer_app.cc`) with a `NativeV8Handler` that injects `window.native.*` functions via `OnContextCreated`. These bindings connect the HTML content to the shared C++ core — calling into the gRPC client, document model, or platform APIs.

## Shared C++ Core

The shared C++ layer contains all business logic. Neither the SwiftUI nor WinUI 3 code contains logic — they are views that bind to the shared core.

### gRPC Client

A single gRPC client library, shared between macOS and Windows. Connects to:

- **Engine** — playout commands, channel status, plugin control
- **Control plane** — rundown management, asset queries, data feeds, template registry

Native gRPC in C++ — no IPC translation layer, no Node.js overhead. The same protocol the automation systems and external integrations use.

### Document Model and Undo/Redo

The shared core owns the document model (rundowns, show config, template assignments) and provides a command/action layer for undo/redo. The native UI dispatches actions to the shared core; the shared core notifies the native UI of state changes via callbacks.

```cpp
class DocumentModel {
    void dispatch(const Action& action);       // UI → core
    void subscribe(StateCallback callback);    // core → UI
    void undo();
    void redo();
};
```

### NDI Receive

For channel preview tiles, the shared core receives NDI streams from the engine and provides decoded frames to the native view for rendering. The NDI SDK is C/C++ — fits naturally in the shared core.

## Application Modes

The app is a **unified application** for all client-side tooling. Users with multiple roles access different modules. A top-level mode switch selects the workspace:

| Mode | Role | Primary UI |
|---|---|---|
| **Operator** | Graphics operator, TD | Rundown editor, channel monitors, transition controls, live data monitor |
| **Library** | Asset manager, producer | Asset browser, template registry, upload/approval workflow |
| **Designer** | Template designer | Template editor, CEF design canvas, preview |
| **Engineering** | Systems engineer | Hardware status, channel config, redundancy monitoring, diagnostics |
| **Admin** | System administrator | User management, roles, show config, system settings |

Each mode presents a different panel layout, toolbar set, and menu structure. The underlying shared core and gRPC connection are the same. A user with both Operator and Designer roles switches between modes without restarting the app.

## Authentication

### Architecture

The native app does **not** use the Firebase C++ SDK for sign-in flows. Authentication happens in the system browser using the Firebase web SDK, identical to the pattern already proven in the Electron app.

### Flow (All Providers — Google, GitHub, OIDC, SAML)

```
Native app
  │
  │  1. Opens system browser to hosted auth page
  │     (ASWebAuthenticationSession on macOS,
  │      WebAuthenticationBroker or ShellExecute on Windows)
  │
  ▼
Hosted auth page (Firebase web SDK)
  │
  │  2. signInWithPopup / signInWithRedirect
  │     Handles Google, GitHub, Apple, any OIDC, any SAML
  │     Firebase Auth does all the protocol work
  │
  ▼
Firebase Auth completes → user has ID token
  │
  │  3. Redirects to Go server callback
  │
  ▼
Go server
  │
  │  4. Verifies Firebase ID token (Admin SDK)
  │  5. Mints custom token: auth.CustomToken(ctx, uid)
  │  6. Redirects to pivox://auth?token=<custom_token>
  │
  ▼
Native app captures URL scheme callback
  │
  │  7. firebase::auth::Auth::SignInWithCustomToken(custom_token)
  │     Now has a fully authenticated firebase::auth::User
  │  8. user->GetToken() for gRPC auth metadata
  │
  ▼
Authenticated gRPC calls to control plane + engine
```

### Why This Works for All Providers

The Firebase web SDK handles every provider Firebase supports — current and future. Adding a new OIDC or SAML provider in Firebase Console works automatically. No native app update needed.

The Firebase C++ SDK **does** support `SignInWithCustomToken` — that's the only Firebase C++ API needed on the client. All provider-specific logic (the missing `OAuthProvider` for OIDC/SAML) runs in the browser via the web SDK.

### What the Firebase C++ SDK Handles Locally

After `SignInWithCustomToken` establishes the session:

- `User::GetToken()` — JWT for gRPC auth metadata
- Token auto-refresh — Firebase C++ SDK handles this in long-running desktop processes
- `User::UpdateUserProfile()` — display name, photo (native profile settings screen)
- `User::UpdatePassword()` — password change (native security settings screen)
- `User::SendEmailVerification()` — email verification
- `Auth::SignOut()` — sign out

### Token Storage

- macOS: Keychain Services (via Security framework)
- Windows: Credential Manager (via `CredWrite`/`CredRead`)

Firebase C++ SDK manages its own token persistence. The native app stores the refresh token securely using platform APIs.

### Existing Implementation

This auth flow is already built and running in the Electron app (see `docs/authn.md`). The hosted auth pages, Go server callback endpoint, custom token minting, and `pivox://` URL scheme handling all exist. The native app reuses the same server-side infrastructure — only the client-side auth capture code changes (Electron's protocol handler → native URL scheme handler).

## Build System

CMake is the single build system. It generates:

- **macOS:** Xcode project (or Ninja for CLI builds)
- **Windows:** Visual Studio solution

### macOS

```bash
cmake -G Ninja -DCMAKE_BUILD_TYPE=Release -DUSE_SANDBOX=OFF ..
ninja
```

Targets: main app bundle (Swift/SwiftUI) + CEF bridge (Obj-C++ static lib) + 5 helper subprocess bundles.

### Windows

```bash
cmake -G "Visual Studio 18 2026" -A x64 -DUSE_SANDBOX=OFF ..
cmake --build . --config Release
```

Targets: WinUI 3 executable + CEF bridge (C++ static lib) + renderer subprocess.

### CEF Distribution

Auto-downloaded at CMake configure time via `DownloadCEF.cmake`. Fetches the platform-specific minimal distribution. No manual download or submodule management.

### Key CMake Patterns for WinUI 3

Documented from the POC — these are hard-won lessons:

- `Directory.Build.props/targets` must be in the build directory, not project root (breaks CMake compiler detection)
- `App.idl` must be empty namespace (not a runtimeclass)
- `App.xaml.cpp` does NOT include `App.g.cpp` (other windows do)
- XAML `ApplicationDefinition` requires `VS_XAML_TYPE` property
- Custom controls: include header in `pch.h` for `XamlTypeInfo.g.cpp`
- `#undef GetCurrentTime` in `pch.h` to prevent Win32/WinRT collision

## Repository Impact

The native app replaces `pivox-web` (Electron + React). The new repo structure:

```
pivox-app/
  ├── CMakeLists.txt               # Root build system
  ├── core/                        # Shared C++ core
  │   ├── grpc/                    # gRPC client (shared)
  │   ├── auth/                    # Auth manager (shared)
  │   ├── document/                # Document model, undo/redo (shared)
  │   ├── ndi/                     # NDI receive for preview (shared)
  │   └── state/                   # State management (shared)
  ├── cef/                         # CEF integration layer
  │   ├── bridge/                  # Platform-specific CEF bridges
  │   ├── handlers/                # CefClient, render handler, OSR
  │   ├── renderer/                # Renderer subprocess (JS bindings)
  │   └── cmake/                   # CEF download and find scripts
  ├── shared-ui/                   # Shared custom-drawn components
  │   ├── timeline/                # Timeline / rundown editor
  │   ├── waveform/                # Audio waveform display
  │   ├── meters/                  # Audio level meters
  │   └── preview/                 # Video preview renderer
  ├── platform/
  │   ├── macos/                   # SwiftUI app
  │   │   ├── App/                 # SwiftUI entry point, app delegate
  │   │   ├── Views/               # SwiftUI views per workspace
  │   │   ├── Components/          # Reusable SwiftUI components
  │   │   └── Bridge/              # Obj-C++ bridge for CEF + shared core
  │   └── windows/                 # WinUI 3 app
  │       ├── App.xaml             # WinUI entry point
  │       ├── Views/               # XAML pages per workspace
  │       ├── Controls/            # Custom WinUI controls (CefHostGrid, etc.)
  │       └── pch.h
  ├── resources/                   # Shared assets (icons, HTML, etc.)
  └── docs/                        # Native app-specific docs
```

The `pivox-web` repo may still exist for the **browser-only** web UI (pure SPA, no native features) used for remote access, lightweight monitoring, or mobile. The native app is the primary operator tool; the web UI is the fallback.

## Advantages Over Electron

| Concern | Electron | Native App |
|---|---|---|
| **Platform feel** | Simulated (CSS) | Authentic (SwiftUI / WinUI 3) |
| **Memory footprint** | ~300-500MB (Chromium + Node.js) | Smaller (CEF only for viewports, not entire UI) |
| **Startup time** | Slow (Chromium init) | Fast (native app + CEF lazy-loaded for viewports) |
| **Multi-monitor** | Awkward (Electron BrowserWindow) | Native multi-window support |
| **gRPC** | Via Node.js IPC bridge | Direct C++ gRPC, no IPC |
| **JS bindings** | IPC serialization (renderer ↔ main) | Direct V8 in-process |
| **OS integration** | Electron APIs (subset) | Full platform APIs |
| **Code sharing** | 100% shared (one codebase) | Shared core + shared custom components; native shell is per-platform |

## Current Status

**macOS (SwiftUI + CEF):** Working prototype. SwiftUI app with CEF embedded, native toolbar, JS bindings operational, local HTML loading, 60fps external message pump.

**Windows (WinUI 3 + CEF OSR):** Working prototype. Software OSR path fully functional (WebGL at full speed). D3D11 GPU path working. Input forwarding, context menus, cursor handling all solved.

**Shared C++ Core:** Stub. gRPC client, document model, NDI receive not yet implemented.

**Authentication:** Server-side infrastructure exists (Go callback, custom token minting, `pivox://` handling). Native client auth capture not yet implemented.
