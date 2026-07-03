# Native Image Editor — Status & Next Steps

Last updated: 2026-04-06

> **Status (2026): legacy / reference.** This tracks work in the Native
> App (macOS/Windows), which is now a legacy/reference target, not an
> active migration. Its auth references (Firebase Auth, the Firebase
> emulator in UI tests) reflect the abandoned native auth and are **no
> longer the Pivox auth system** — the cloud is Keycloak-only
> (`internal/oidc`), so native Firebase tokens don't authenticate
> against the current Cloud Controller. Non-auth details (image-editor
> engine, renderers, build) remain accurate for the legacy app.

## Current State

The image editor ("Edit" tool) is in the Library section of the Native App. It's a shared C++ core with a SwiftUI renderer on macOS. Windows renderer (Win2D) is not yet started.

### What's Done

**Phase 1: Shared C++ Core** — Complete, 109 gtest tests passing.

- `native/core/image-editor/image_editor_types.h` — types, enums, constants
- `native/core/image-editor/crop_math.h/cpp` — pure math (min scale, translation bounds, crop resize, aspect ratio, hit testing)
- `native/core/image-editor/image_editor_engine.h/cpp` — state machine, undo/redo, drag handling, rotation, zoom, modes
- `native/tests/core/crop_math_tests.cpp` — 36 tests
- `native/tests/core/image_editor_engine_tests.cpp` — 36 tests (plus 37 more from other core tests)

**Phase 2: macOS Renderer + UI** — Functional, needs polish.

- `native/platform/macos/objcpp/ImageEditorBridge.h/mm` — Obj-C++ bridge wrapping C++ engine
- `native/platform/macos/swift/ImageEditor/EditView.swift` — SwiftUI view + Core Graphics renderer (~770 lines)
- 27 XCTest unit tests for the bridge layer (`ImageEditorBridgeTests.swift`)
- 12 XCUITest UI tests (`ImageEditorUITests.swift`) — 2 passing, 10 failing (see Known Issues)

### Architecture

```
SwiftUI (EditView.swift)
  │
  │  @Observable ImageEditModel
  │  reads IEBState, calls bridge methods
  │
  ▼
Obj-C++ Bridge (ImageEditorBridge.h/mm)
  │
  │  Thin wrappers: Swift ↔ C++
  │  IEBState, IEBCropRect, IEBCropTemplate objects
  │
  ▼
C++ Engine (image_editor_engine.h/cpp)
  │
  │  State machine, undo/redo, crop math
  │  Zero platform dependencies
  │
  ▼
C++ Crop Math (crop_math.h/cpp)
    Pure functions, no state
```

The renderer is an `NSView` subclass (`ImageEditCanvasNSView`) hosted via `NSViewRepresentable`. It reads engine state and draws with Core Graphics: image transform → dimmed overlay → crop border → grid → L-bracket handles → edge handles.

### UI Design

Modeled after macOS Photos app:

- **View mode**: Image displayed with back button, zoom controls, Edit button
- **Edit mode**: Dark theme, right-side tool panel (260px overlay), crop handles on image
- **Toolbar**: Revert to Original | zoom slider | Crop tab | Done (yellow)
- **Tool panel**: Straighten (ruler slider), Flip H/V, Aspect ratio templates (Free, 16:9, 4:3, 1:1, etc.)
- **Bottom**: Undo/Redo, Reset
- **Handles**: White L-brackets at corners (vertex outside crop border, arms inward), white bars at edge midpoints
- **Grid**: 8x8, shown during drag or straighten (not always)
- **Dark mode**: `NSWindow.appearance` set to `.darkAqua` during edit, `nil` on exit

### macOS Swift File Structure

```
native/platform/macos/swift/
├── App/
│   ├── main.swift
│   ├── AppDelegate.swift
│   └── ContentView.swift
├── Auth/
│   ├── AuthService.swift
│   ├── LoginView.swift
│   ├── RegisterView.swift
│   └── ProfileView.swift
├── ImageEditor/
│   └── EditView.swift
└── Components/
    ├── GlassCard.swift
    └── GoogleIcon.swift
```

### Key Files

| File | Purpose |
|---|---|
| `native/core/image-editor/*.h/cpp` | Shared C++ core (types, crop math, engine) |
| `native/platform/macos/objcpp/ImageEditorBridge.h/mm` | Obj-C++ bridge (C++ → Swift) |
| `native/platform/macos/swift/ImageEditor/EditView.swift` | SwiftUI renderer + toolbar + tool panel (~770 lines) |
| `native/platform/macos/swift/App/ContentView.swift` | App shell, Library integration, dark mode, TEST_IMAGE_PATH hook |
| `native/platform/macos/swift/Auth/AuthService.swift` | Firebase Auth, UI_TESTING flag, emulator support |
| `native/platform/macos/CMakeLists.txt` | Bridge library (ARC for all .mm files) |
| `native/CMakeLists.txt` | App target, warnings-as-errors, PRODUCT_BUNDLE_IDENTIFIER |
| `native/tests/core/crop_math_tests.cpp` | 36 gtest for crop math |
| `native/tests/core/image_editor_engine_tests.cpp` | 36 gtest for engine |
| `native/tests/macos/ImageEditorBridgeTests.swift` | 27 XCTest unit for bridge |
| `native/tests/macos/ui/ImageEditorUITests.swift` | 12 XCUITest UI tests |

## Known Issues

### 1. Dark mode not fully reverting after Done (the original bug)

**Problem**: After exiting edit mode, the sidebar can retain dark appearance while detail area goes light.

**Current fix**: `NSWindow.appearance` set directly via `.onChange(of: isImageEditing)`. The `.preferredColorScheme` + `.id()` approach was tried but `.id()` destroys the entire NavigationSplitView state (loses loaded image, selected tab, etc.). The Gemini suggestion of `.id()` was correct for theme propagation but destructive for view state.

**Status**: `NSWindow.appearance` approach works but needs manual testing to confirm the sidebar fully reverts in all scenarios. Switching to another app and back seems to fix it (macOS redraws on activation). May need `NSApp.keyWindow?.invalidateShadow()` or similar nudge.

### 2. UI tests — FIXED (12/12 passing)

**Root causes found and fixed** (via `app.debugDescription` dump):

1. **`.accessibilityIdentifier("image-edit-view")` on `ImageEditView` body** — propagated to ALL children including `CropToolPanel`, overriding their individual identifiers (edit-straighten, edit-undo, etc.). Toolbar items were unaffected because NSToolbar is separate from the view hierarchy. **Fix**: removed the container identifier; the canvas NSView sets its own identifier directly via `setAccessibilityIdentifier` in `makeNSView`.

2. **`CropToolButton` used `onTapGesture` instead of `Button`** — the HStack+onTapGesture didn't create a proper AX element. Tests couldn't find flip/aspect buttons. **Fix**: converted to real `Button` with `.buttonStyle(.plain)`. Now appears as AXButton with proper identifier.

3. **`TEST_IMAGE_PATH` auto-load fired on every `onAppear`** — after Done/Back, the placeholder appeared briefly, then `onAppear` re-auto-loaded the image, making the placeholder disappear before the test could find `library-open-image`. **Fix**: added `didAutoLoad` guard so auto-load only fires once.

4. **Zoom controls: toolbar flattens HStack** — `.accessibilityIdentifier("edit-zoom")` on the HStack propagated to children (Slider + Images) instead of the wrapper Group. `app.otherElements["edit-zoom"]` found nothing. **Fix**: test now uses `app.sliders["edit-zoom"]` to find the zoom slider directly.

5. **Straighten ruler rewritten as NSSlider** — now appears as `app.sliders["edit-straighten"]` instead of `app.otherElements`. Test updated accordingly.

### 3. NSView accessibility

`accessibilityIdentifier` set via SwiftUI modifiers does NOT propagate through `NSViewRepresentable`. The NSView is invisible in the accessibility tree unless you call `setAccessibilityElement(true)`, `setAccessibilityRole(.image)`, and `setAccessibilityIdentifier(...)` directly on the NSView in `makeNSView`. This is now done. Test `testCanvasIsDiscoverable` passes, confirming it works. Use `app.images["image-edit-view"]` (not `app.otherElements`) since the role is `.image`.

### 4. Window widens during edit

**Fixed**: Tool panel changed from `HStack` (adds width) to `.overlay(alignment: .trailing)` (overlaps canvas). Window no longer grows.

## Test Infrastructure

See `docs/dev/ui-testing.md` for full details on build configurations, launch flags, and running tests.

Quick reference:
```bash
# C++ core tests
cd native && cmake --build build-xcode --config Debug --target pivox_core_tests
./build-xcode/Debug/pivox_core_tests

# Swift unit tests (bridge)
xcodebuild test -project build-xcode/Pivox.xcodeproj -scheme PivoxTests -configuration Debug

# UI tests (image editor uses DebugUITest, auth uses Debug + emulator)
make test-native-ui

# Regenerate Xcode project after CMakeLists changes
cd native && cmake -G Xcode -B build-xcode
```

To open in Xcode: open `build-xcode/Pivox.xcodeproj`, go to Product → Scheme → Manage Schemes, check "Show" next to Pivox.

## Phase 3: Windows Renderer

**Not started.** The workflow:

1. Finish and commit macOS work (Phases 1-2)
2. Push to remote
3. Generate a comprehensive prompt for the Windows side containing:
   - Shared C++ core API reference (already in repo via `git pull`)
   - Win2D CanvasControl renderer spec (same drawing as macOS Core Graphics)
   - XAML toolbar spec (same controls as SwiftUI toolbar)
   - FlaUI test spec (same test cases as XCUITest)
4. User runs the prompt manually on `kirby-win` (D:\pivox) — NOT via SSH
5. Windows machine pulls latest (gets shared C++ core), builds with Visual Studio

**Windows build**: `kirby-win` SSH alias, working directory `D:\pivox`. CMake generates Visual Studio solution. Firebase C++ SDK for auth. No Obj-C++ bridge needed — WinUI C++ calls `ImageEditorEngine` directly.

**Windows UI test config**: Same `DebugUITest` pattern as macOS. The CMakeLists.txt already registers the config for Visual Studio. For C++/WinRT, define `UITEST` as a preprocessor macro:
```cmake
# In the Windows section of CMakeLists.txt:
target_compile_definitions(${WIN_TARGET} PRIVATE "$<$<CONFIG:DebugUITest>:UITEST>")
```
Then in `WinAuthService.cpp`:
```cpp
#ifdef UITEST
if (getenv("SKIP_AUTH")) { /* bypass */ }
#endif
```
FlaUI tests use `DebugUITest` config. MSTest tests that don't need auth set `SKIP_AUTH=1` in the process start info. See `docs/dev/ui-testing.md` for the full pattern.

**NuGet dependencies**:
- `Microsoft.Graphics.Win2D` — 2D rendering (replaces Core Graphics). First-class WinUI 3 citizen.

**Icons**: Download individual SVGs directly from [microsoft/fluentui-system-icons](https://github.com/microsoft/fluentui-system-icons). No NuGet, no font files — only ship the ~15 icons we actually use. Use `ImageIcon` or extract path data into `PathIcon` for zero external files.

Icon search: `icons_filled.md` / `icons_regular.md` at repo root list all names. SVGs are at `assets/{Name}/SVG/ic_fluent_{name}_{size}_{style}.svg`. Use 24px variants (largest available — sizes are 12, 16, 20, 24). Most detail, scales cleanly since SVG is vector.

**Icon directory convention** — mirrors Microsoft's repo structure, lowercased with dashes. Use 24px (largest available). Pick regular or filled to match the corresponding SF Symbol:
```
native/platform/windows/Assets/Icons/
├── crop/
│   └── ic_fluent_crop_24_{regular|filled}.svg
├── flip-horizontal/
│   └── ic_fluent_flip_horizontal_24_{regular|filled}.svg
├── flip-vertical/
│   └── ic_fluent_flip_vertical_24_{regular|filled}.svg
├── arrow-undo/
│   └── ic_fluent_arrow_undo_24_{regular|filled}.svg
├── arrow-redo/
│   └── ic_fluent_arrow_redo_24_{regular|filled}.svg
├── add/
│   └── ic_fluent_add_24_{regular|filled}.svg
├── subtract/
│   └── ic_fluent_subtract_24_{regular|filled}.svg
├── circle-line/
│   └── ic_fluent_circle_line_24_{regular|filled}.svg
└── ratio-one-to-one/
    └── ic_fluent_ratio_one_to_one_24_{regular|filled}.svg
```

**SF Symbols → Fluent Icons mapping** (image editor):

Pick regular or filled per icon — whichever best matches the SF Symbol visual weight.

| SF Symbol (macOS) | Fluent Icon name | Usage |
|---|---|---|
| `circle.and.line.horizontal.fill` | `circle_line` (filled) | Straighten tool |
| `arrow.left.and.right.righttriangle.left.righttriangle.right` | `flip_horizontal` | Flip horizontal |
| `arrow.up.and.down.righttriangle.up.righttriangle.down` | `flip_vertical` | Flip vertical |
| `aspectratio` | `ratio_one_to_one` | Aspect ratio |
| `crop` | `crop` | Crop tool tab |
| `arrow.uturn.backward` | `arrow_undo` | Undo |
| `arrow.uturn.forward` | `arrow_redo` | Redo |
| `minus` / `plus` | `subtract` / `add` | Zoom controls |

SVG path: `assets/{Name}/SVG/ic_fluent_{name}_24_{regular|filled}.svg`

## What Needs to Happen Next

### Immediate

1. ~~**Rewrite straighten ruler as custom NSSlider**~~ — **DONE.** Custom `RulerNSSlider` + `RulerSliderNSCell` wrapped in `NSViewRepresentable`. Same pixel-verified tick pattern. Gains: AXSlider role, keyboard arrows, VoiceOver, XCUITest sees `app.sliders["edit-straighten"]`. Snap-to-zero on mouseUp commit.
2. ~~**Fix UI test failures**~~ — **DONE.** 12/12 passing. See Known Issues §2 for root causes.
3. **Manual test dark mode transition** — verify NSWindow.appearance approach works for: edit → done, edit → back, switching to another app and back
4. ~~**Run full test suite**~~ — **DONE.** 109 C++ core + 16 auth UI + 12 editor UI = 137 passing, 0 failures.

### Before Windows Prompt

4. **Polish crop interaction** — test drag handles, straighten slider, flip animation, aspect ratio templates thoroughly
5. **Generate Windows prompt** — comprehensive, self-contained prompt for `kirby-win`

### Future (see also `docs/discussions/image-editor-next-steps.md` for web-side roadmap)

- Alpha channel detection + checkerboard background
- Allow dead pixels + background color option
- Adjustments (brightness, contrast, saturation, etc.)
- Rename to ImageTransform when feature set stabilizes

### Lessons Learned

- **`accessibilityIdentifier` on SwiftUI containers can hide children** — setting it on a `Group` wrapping the entire app made all child elements invisible to XCUITest. Don't put identifiers on structural containers.
- **`accessibilityIdentifier` doesn't propagate through `NSViewRepresentable`** — must call `setAccessibilityElement(true)`, `setAccessibilityRole()`, `setAccessibilityIdentifier()` directly on the NSView in `makeNSView`.
- **`.preferredColorScheme` doesn't propagate through `NavigationSplitView` sidebar** — when sidebar visibility changes simultaneously, the sidebar retains the stale color scheme. Use `NSWindow.appearance` directly.
- **`.id()` on `NavigationSplitView` destroys all child state** — forces full rebuild, loses loaded image, selected tab, etc. Never use `.id()` for theme changes on stateful views.
- **Firebase Auth emulator starts with zero users** — every UI test must register through the UI first. `UI_TESTING=1` env var resets all state (auth + prefs + sticky tab).
- **Always diagnose before changing code** — dump `app.debugDescription` to see the accessibility hierarchy instead of guessing.
- **`accessibilityIdentifier` on containers propagates to ALL children** — not just hiding, but overriding individual identifiers. The `ImageEditView` body identifier `"image-edit-view"` replaced every child's identifier in `CropToolPanel`. Toolbar items were unaffected (NSToolbar is separate). Only set identifiers on leaf views or NSViews directly.
- **Use `Button` instead of `onTapGesture` for interactive elements** — `HStack` + `onTapGesture` doesn't create a proper AX element. Real `Button` with `.buttonStyle(.plain)` gives AXButton role, VoiceOver support, keyboard focus, and proper identifier scoping.
- **SwiftUI toolbar flattens HStack children** — `.accessibilityIdentifier` on an HStack inside `ToolbarItem` propagates to children (Images, Sliders) instead of creating a wrapper Group element. Use specific element queries (`app.sliders["id"]`) not `app.otherElements`.
- **`onAppear` fires on every view appearance, not just first** — auto-load hooks in UI testing must use a `@State` guard flag to prevent re-execution after Done/Back transitions.
