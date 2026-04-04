# Discussion: Electron to Native App

**Date:** 2026-04-04
**Decision:** Replace Electron with native SwiftUI (macOS) + WinUI 3 (Windows) app with shared C++ core.
**Status:** Decided — POC validated on both platforms.

## Context

The Pivox operator UI was originally built as an Electron app (React + TypeScript). We evaluated replacing it with a native desktop application using platform-specific UI frameworks and a shared C++ core.

## Why Move Away from Electron

**Audience expectations.** Broadcast operators use native tools (Vizrt Trio, Ross XPression, Grass Valley). Electron apps feel foreign in this environment — non-native menus, non-standard keyboard shortcuts, unfamiliar window management.

**Performance.** The operator UI displays real-time video preview, audio meters, and live data feeds across multiple monitors. Electron's Chromium + Node.js overhead (300-500MB baseline) and IPC serialization between renderer and main processes adds unnecessary latency and memory pressure.

**gRPC integration.** Electron requires a Node.js IPC bridge for gRPC communication. A native app with a shared C++ core uses gRPC directly — the same protocol automation systems use, with no translation layer.

**Multi-monitor.** Broadcast operators work across 2-4 monitors with detachable panels. Native multi-window support is significantly better than Electron's `BrowserWindow` API.

## POC Results

### macOS — SwiftUI + CEF SetAsChild

Fully working. SwiftUI app with CEF browser embedded as a child NSView. Obj-C++ bridge wraps CEF for Swift consumption. External message pump at 60fps. JavaScript V8 bindings operational. Local HTML loading works.

### Windows — WinUI 3 + CEF OSR

Fully working. Discovered that **child HWNDs do not render inside WinUI 3** (DirectComposition paints over them). Solved with Off-Screen Rendering — CEF renders to a pixel buffer, copied to the WinUI Image control. Performance validated: WebGL runs at full speed through the software path. D3D11 shared texture GPU path also working.

Solved additional platform challenges: keyboard input forwarding (ToUnicode workaround), cursor changes (custom CefHostGrid subclass), context menus (native MenuFlyout from CefMenuModel).

## Architecture Decision: Simple UI Native, Complex UI Shared

The key insight: **complex UI components don't benefit from being native**. A timeline editor, waveform display, or node graph doesn't exist in SwiftUI or WinUI 3's widget libraries — you'd custom-draw them on either platform. Write them once as shared components.

- **Native (per-platform):** menus, toolbars, property inspectors, forms, panel layout, settings
- **Shared (written once):** timeline editor, waveform display, audio meters, video preview renderer, transition preview
- **CEF (HTML/JS):** template design canvas, template preview

Components can be composed: a channel monitor tile uses native layout for the grid and labels, with a shared component for the live preview content inside each tile.

## Authentication: Browser + Custom Token

The Firebase C++ SDK does not support OIDC/SAML sign-in (no `OAuthProvider` equivalent). However, the existing Electron auth flow already solves this:

1. Open system browser to hosted auth page (Firebase web SDK)
2. Web SDK handles all providers (Google, GitHub, OIDC, SAML)
3. Go server verifies, mints custom token
4. Redirect to `pivox://` URL scheme
5. Native app calls `SignInWithCustomToken` (which the C++ SDK does support)

The server-side infrastructure (hosted auth pages, Go callback, custom token minting) already exists from the Electron implementation. No new server work needed.

## Unified Application

The native app serves all client-side roles in a single application with mode switching:

- **Operator** — rundown, channel monitors, live data, transitions
- **Library** — asset management, template registry
- **Designer** — template editor, design canvas
- **Engineering** — hardware status, diagnostics
- **Admin** — user management, system settings

Users with multiple roles switch modes without restarting. Shared C++ core (gRPC, auth, state) is the same across all modes.

## Trade-offs Accepted

**Two native shells to maintain.** SwiftUI and WinUI 3 code is separate. Mitigated by keeping the native layer thin (standard controls, layout, forms) and moving complex UI to shared components.

**Longer initial development.** Building two native shells takes more time than one Electron codebase. Offset by: no IPC overhead, no Node.js bridge, direct gRPC, and the shared component strategy minimizing duplication.

**CEF still required.** The app embeds CEF for template preview viewports. However, CEF is loaded only for those viewports — not for the entire UI. Memory footprint is significantly smaller than Electron (one CEF instance for the preview viewport vs. Chromium powering every pixel on screen).

## References

- POC code: `~/Projects/dashkan/test/`
- POC documentation: `~/Projects/dashkan/test/docs/`
- Auth architecture: `docs/authn.md`
- Native app architecture: `docs/native-app.md`
- Cross-platform workflow: `docs/dev/cross-platform-workflow.md`
