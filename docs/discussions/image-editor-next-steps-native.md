# Native Image Editor — Status & Next Steps

Last updated: 2026-04-06

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

### 2. UI tests — 10 of 12 failing

**Passing**: `testEditorOpensInViewMode`, `testCanvasIsDiscoverable`

**Failing**: All tests that click `edit-enter` (Edit button) and then look for edit-mode UI elements (Done button, crop tools, undo/redo, etc.). The Edit button click appears to work (no error on the click itself), but edit-mode toolbar items don't appear.

**Root cause not yet diagnosed**. Possible causes:
- The `withAnimation` block in the Edit button handler may delay state changes
- The `.id(isImageEditing)` was removed — but toolbar content is conditional on `model.isEditing`, not `isImageEditing`. The toolbar swap might not be triggering
- `NSWindow.appearance` change might interfere with XCUITest's element discovery
- The toolbar rebuilds completely between view/edit mode — XCUITest may need a wait

**Next step**: Add `print(app.debugDescription)` after clicking Edit to dump the accessibility hierarchy and see what's actually there. Same diagnostic approach that found the NSView accessibility issue.

### 3. NSView accessibility

`accessibilityIdentifier` set via SwiftUI modifiers does NOT propagate through `NSViewRepresentable`. The NSView is invisible in the accessibility tree unless you call `setAccessibilityElement(true)`, `setAccessibilityRole(.image)`, and `setAccessibilityIdentifier(...)` directly on the NSView in `makeNSView`. This is now done. Test `testCanvasIsDiscoverable` passes, confirming it works. Use `app.images["image-edit-view"]` (not `app.otherElements`) since the role is `.image`.

### 4. Window widens during edit

**Fixed**: Tool panel changed from `HStack` (adds width) to `.overlay(alignment: .trailing)` (overlaps canvas). Window no longer grows.

## Test Infrastructure

### Running Tests

```bash
# C++ core tests (no emulator needed)
cmake --build build-xcode --config Debug --target pivox_core_tests
./build-xcode/Debug/pivox_core_tests

# Swift unit tests (bridge tests, no emulator needed)
xcodebuild test -project build-xcode/Pivox.xcodeproj -scheme PivoxTests -configuration Debug

# UI tests (requires Firebase Auth emulator)
make test-native-ui   # starts emulator, runs ALL UI tests, stops emulator

# Single UI test class
firebase emulators:start --only auth --project pivox-cloud &
xcodebuild test -project build-xcode/Pivox.xcodeproj -scheme PivoxUITests -configuration Debug \
  -only-testing:PivoxUITests/ImageEditorUITests
pkill -f "firebase.*emulators"
```

### UI Test Setup

Image editor UI tests use:
- `UI_TESTING=1` — single flag that resets auth tokens, preferences, and sticky tab selection
- `USE_AUTH_EMULATOR=1` — points Firebase Auth at local emulator (127.0.0.1:9099)
- `TEST_IMAGE_PATH=<path>` — auto-loads a test image in LibraryPlaceholderView, bypassing NSOpenPanel

Each test registers a fresh user through the UI (emulator starts with zero users), navigates to Library, and the test image auto-loads.

### Regenerating Xcode Project

After changing CMakeLists.txt:
```bash
cd native
cmake -G Xcode -B build-xcode
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

**NuGet dependencies**:
- `Microsoft.Graphics.Win2D` — 2D rendering (replaces Core Graphics). First-class WinUI 3 citizen.
- `FluentIcons.WinUI` — [Fluent UI System Icons](https://github.com/microsoft/fluentui-system-icons) (~4000 icons). Covers SF Symbols equivalents that Segoe Fluent Icons lacks (straighten, flip, aspect ratio, etc.). Use `FluentIcons.` prefix in XAML.

**SF Symbols → Fluent Icons mapping** (image editor):
| SF Symbol (macOS) | Fluent Icon (Windows) | Usage |
|---|---|---|
| `circle.and.line.horizontal.fill` | `CircleLine16Filled` | Straighten tool |
| `arrow.left.and.right.righttriangle.left.righttriangle.right` | `FlipHorizontal16Regular` | Flip horizontal |
| `arrow.up.and.down.righttriangle.up.righttriangle.down` | `FlipVertical16Regular` | Flip vertical |
| `aspectratio` | `RatioOneToOne16Regular` | Aspect ratio |
| `crop` | `Crop16Regular` | Crop tool tab |
| `arrow.uturn.backward` | `ArrowUndo16Regular` | Undo |
| `arrow.uturn.forward` | `ArrowRedo16Regular` | Redo |
| `minus` / `plus` | `Subtract16Regular` / `Add16Regular` | Zoom controls |

## What Needs to Happen Next

### Immediate

1. **Fix UI test failures** — 10 of 12 image editor UI tests fail. Diagnose by dumping accessibility hierarchy (`print(app.debugDescription)`) after clicking Edit to see what XCUITest sees. All tests that click `edit-enter` and look for edit-mode elements fail.
2. **Manual test dark mode transition** — verify NSWindow.appearance approach works for: edit → done, edit → back, switching to another app and back
3. **Run full test suite** — `make test-native-ui` must pass (auth + sidebar + image editor tests)

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
