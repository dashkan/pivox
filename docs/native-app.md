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
|  - gRPC client (engine + Cloud Controller)     |
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
- **Cloud Controller / Playout Agent** — rundown management, asset queries, data feeds, template registry

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

### Two Auth Paths

Authentication uses two paths depending on the provider — **native screens** for email/password (the Firebase C++ SDK supports this directly) and **external browser** for everything else (social, OIDC, SAML).

### Path 1: Email/Password — Native UI + Firebase C++ SDK

Email/password auth is handled entirely in native screens. No browser involved.

**Sign in, sign up, forgot password, password reset** — all native SwiftUI / WinUI 3 screens calling the Firebase C++ SDK directly:

```cpp
// Sign in
auth->SignInWithEmailAndPassword(email, password);

// Register
auth->CreateUserWithEmailAndPassword(email, password);

// Forgot password
auth->SendPasswordResetEmail(email);
```

**User profile management** — native screens for:

- Display name and photo (SwiftUI form / WinUI 3 form)
- Email change with verification
- Password change
- Connected accounts (link/unlink social providers)
- MFA enrollment (TOTP)
- Account deletion

These screens replace the current HTML-based `LoginCard`, `RegistrationCard`, `UserProfileCard`, `ForgotPasswordCard`, and `ResetPasswordCard` compound components (documented in `docs/authn.md`) with native equivalents. The Firebase C++ SDK provides all the necessary methods:

```cpp
// Profile management
user->UpdateUserProfile(profile);   // display name, photo
user->UpdatePassword(new_password);
user->SendEmailVerification();
user->Delete();                     // account deletion

// Token for gRPC
user->GetToken(/*force_refresh=*/false, &token_result);
```

### Path 2: Social / OIDC / SAML — External Browser + Custom Token

Social providers (Google, GitHub, Apple) and enterprise SSO (OIDC, SAML) require browser-based OAuth/SAML flows. The Firebase C++ SDK does not support these directly (no `OAuthProvider` equivalent). The external browser handles it:

```
Native app
  │
  │  1. User taps "Sign in with Google" or "Enterprise SSO"
  │     Opens system browser to hosted auth page
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
Authenticated gRPC calls to Cloud Controller + engine
```

### Why Two Paths

| Provider Type | Auth Path | Why |
|---|---|---|
| Email/password | Native UI + C++ SDK | C++ SDK supports it directly. Native forms feel right for email/password. No browser needed. |
| Google, GitHub, Apple | External browser + custom token | OAuth requires browser redirects. C++ SDK has `GoogleAuthProvider` but the browser path is simpler and consistent. |
| OIDC (enterprise SSO) | External browser + custom token | C++ SDK has no `OAuthProvider`. Must use web SDK. |
| SAML (enterprise SSO) | External browser + custom token | SAML is browser-based by design (POST bindings, XML redirects). |

Social providers *could* use the C++ SDK's native methods (e.g., `GoogleAuthProvider::GetCredential`), but routing them through the browser alongside OIDC/SAML simplifies the auth architecture — one browser flow handles all non-password providers. The native login screen shows email/password fields and social/SSO buttons; the buttons open the browser.

### Native Auth Screens (To Build)

These replace the existing React compound components with native equivalents:

| Screen | Replaces | Firebase C++ SDK Methods |
|---|---|---|
| **Login** | `LoginCard` | `SignInWithEmailAndPassword`, social buttons → browser path |
| **Register** | `RegistrationCard` | `CreateUserWithEmailAndPassword`, social buttons → browser path |
| **Forgot Password** | `ForgotPasswordCard` | `SendPasswordResetEmail` |
| **User Profile** | `UserProfileCard.AccountPage` | `UpdateUserProfile`, `UpdatePassword`, `SendEmailVerification` |
| **Security Settings** | `UserProfileCard.SecurityPage` | `UpdatePassword`, TOTP enrollment |
| **Link Account** | `LinkAccountCard` | `LinkWithCredential` |

The existing web-based auth components (`@pivox/ui` auth cards, `@pivox/features` auth hooks) remain for the browser-only web UI. The native screens are additive — they don't replace the web implementation, they're a parallel native implementation calling the same Firebase backend.

### Token Storage

- macOS: Keychain Services (via Security framework)
- Windows: Credential Manager (via `CredWrite`/`CredRead`)

Firebase C++ SDK manages its own token persistence. The native app stores the refresh token securely using platform APIs.

### Shared Auth Logic

All auth logic lives in the **shared C++ core**, not in platform-specific code. The Firebase C++ SDK is already cross-platform — wrapping it in Rust would add indirection over a library that already works identically on macOS and Windows.

```
SwiftUI auth screens ──→ Shared C++ AuthManager ──→ Firebase C++ SDK
WinUI 3 auth screens ──→ Shared C++ AuthManager ──→ Firebase C++ SDK
```

The `AuthManager` in the shared C++ core handles:

- Form validation and error mapping
- Auth state machine (logged out → authenticating → authenticated → token expired)
- Token management for gRPC metadata
- All Firebase C++ SDK calls

The native screens are thin views — they render form fields and bind to `AuthManager` callbacks. Written per-platform, but minimal code since all logic is shared.

### Existing Infrastructure

The external browser auth flow is already built and running in the Electron app (see `docs/authn.md`). The hosted auth pages, Go server callback endpoint, custom token minting, and `pivox://` URL scheme handling all exist. The native app reuses the same server-side infrastructure — only the client-side code changes (Electron's protocol handler → native URL scheme handler, React auth screens → native auth screens).

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

## Embedded Engine

The native app architecture enables embedding the Pivox playout engine directly into the application. Since the engine is Rust + C/C++ and the app already has a C++ shared core, the engine can be loaded in-process:

```
Native App (with embedded engine)
  ├── Native shell (SwiftUI / WinUI 3)
  ├── Shared C++ core
  ├── Embedded Pivox engine (Rust, loaded as library)
  │   ├── Compositor (GPU)
  │   ├── CEF plugin (template rendering)
  │   ├── Rive plugin
  │   ├── FFmpeg plugin
  │   ├── Hardware encode/decode (NVENC / VideoToolbox)
  │   └── Software clock + direct framebuffer output
  └── Design canvas viewport (showing engine output)
```

### For Template Designers

A template designer gets the full playout engine running locally inside the app — real GPU compositor, real plugin rendering, real SDK bindings, real transitions, real hardware encode/decode. No separate engine process to manage, no network connection to a remote engine. The software clock drives timing, a direct framebuffer provides preview output to the design canvas viewport.

This is the complete development suite in a single application: edit a template, see it rendered by the real engine, test data bindings, preview transitions — all without deploying to a staging server or connecting to external hardware.

### For Engine Developers

The embedded engine is also the **engine development workbench**. Change compositor code, change a plugin, change the frame pipeline — rebuild and see the result immediately in the design canvas. No separate engine process to launch, no gRPC connection to establish, no NDI stream to set up. The engine and its visual output are in the same process.

This collapses the engine development feedback loop to: edit → build → see. The native app becomes the primary tool for developing the engine itself, not just for using it.

### Connection Modes

For **Operator** mode and other production workflows, the app connects to remote engines via gRPC as normal. The embedded engine is used for design and engine development:

```
Operator mode:     App ──gRPC──→ Remote engine(s) on production hardware
Designer mode:     App ──gRPC──→ Embedded engine (in-process)
Engine dev:        App ──gRPC──→ Embedded engine (in-process, rebuild on change)
```

### In-Process gRPC

The embedded engine exposes the **exact same gRPC service interface** as a standalone engine. The only difference is transport — in-process gRPC instead of Unix domain socket or TCP. The app's shared C++ core connects to the embedded engine the same way it connects to a remote engine. No second API surface, no second test matrix.

```
Operator mode:   App ──gRPC──→ Remote engine (UDS/TCP)
Designer mode:   App ──gRPC──→ Embedded engine (in-process)
```

The shared C++ gRPC client code is identical in both modes. A connection target config determines whether to connect to a remote address or the in-process engine.

### Embedded Engine Build Configuration

The embedded engine is the full engine **minus broadcast I/O hardware** (AJA cards, GPI, SDI, ST 2110). Everything that runs on standard desktop hardware with a GPU stays in:

| Component | Standalone (Production) | Embedded (Designer) |
|---|---|---|
| Compositor (GPU) | Yes | Yes |
| CEF plugin | Yes | Yes |
| Rive plugin | Yes | Yes |
| FFmpeg plugin | Yes | Yes |
| Hardware encode (NVENC / VideoToolbox) | Yes | Yes |
| Hardware decode (NVDEC / VideoToolbox) | Yes | Yes |
| Software clock | Yes | Yes (primary clock source) |
| NDI output | Yes | Yes |
| AJA NTV2 (SDI output) | Yes | No — broadcast hardware |
| AJA genlock clock | Yes | No — broadcast hardware |
| GPI handler | Yes | No — broadcast hardware |
| ST 2110 | Yes | No — broadcast hardware |
| Caption/VANC | Yes | No — broadcast hardware |

The distinction is clean: **broadcast I/O requires specialized hardware** and is excluded. Everything else runs on any machine meeting minimum GPU requirements and is included. This isn't "engine-lite" — it's the engine without AJA.

A GPU is a minimum system requirement for the designer app. Features like GPU compositing, hardware encode/decode (NVENC on Windows, VideoToolbox on macOS), and CEF GPU-accelerated rendering require it and will not function without it.

The embedded build configuration emerges naturally from the engine's hardware abstraction. Output adapters that require AJA SDK linkage are behind compile-time feature flags — the embedded build simply disables those flags.

### Scope Constraints

The embedded engine is deliberately limited to prevent scope creep:

- **One channel only.** Designers work on one template at a time. Multi-channel preview is an operator concern.
- **Software clock only.** No genlock, no external timing reference.
- **No data plane.** Shared memory feeds are not available in embedded mode. Template data comes from the designer's mock data in the UI. Enough to test bindings, not to simulate live production.
- **No automation integration.** No MOS, VDCP, or external automation triggers.
- **Direct framebuffer output.** The compositor writes to a buffer the design canvas reads directly. NDI is optional for external monitoring but not the primary path.

The designer app is a design tool, not a production simulator. For full multi-channel testing with live data, the designer deploys to a staging engine (Tier 3 workflow).

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

**macOS (SwiftUI):** Functional app shell with sidebar navigation (Operator, Library, Designer, Engineering, Admin), profile. Image editor (crop/rotate/straighten/flip/zoom) in Library section with Photos-style UI. Firebase Auth (email/password + Google Sign-In via ASWebAuthenticationSession). 109 C++ core tests, 27 Swift bridge tests, 28+ XCUITest UI tests (auth + sidebar + image editor).

**Windows (WinUI 3):** App shell with sidebar, login, register pages. Firebase C++ SDK for email/password auth. Google Sign-In via OAuth2Manager. Image editor not yet started (Phase 3 — Win2D renderer, see `docs/discussions/image-editor-next-steps-native.md`).

**Shared C++ Core:** AppState (preferences, Keychain/CredentialManager), auth validation, image editor engine (crop math, state machine, undo/redo). 109 gtest tests passing on both platforms.

**Authentication:** Firebase Auth on both platforms. Email/password via native screens. Google Sign-In via ASWebAuthenticationSession (macOS) / OAuth2Manager (Windows). Firebase Auth Emulator integration for UI testing. Shared error constants across platforms (`core/auth_constants.h`).

**Build System:** CMake generating Xcode (macOS) and Visual Studio (Windows). Warnings-as-errors enabled (`-Wall -Wextra -Werror` / `/W4 /WX`). `SWIFT_TREAT_WARNINGS_AS_ERRORS` for Swift. `make test-native-ui` orchestrates Firebase emulator + XCUITest lifecycle.
