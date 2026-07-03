# UI Testing Strategy

> **Status (2026): legacy / reference.** This covers UI testing for the
> Native App (macOS), whose auth is Firebase-based (the `USE_AUTH_EMULATOR`
> / `SKIP_AUTH` flags below drive the Firebase Auth emulator). The Native
> App is now a legacy/reference target and Firebase is no longer the Pivox
> auth system — the cloud is Keycloak-only (`internal/oidc`). These flags
> and flows remain accurate for building/testing the legacy native app, but
> its Firebase tokens don't authenticate against the current Cloud
> Controller. See `AGENTS.md` for the current auth model.

## Launch Environment Flags

| Flag | Purpose | Requires Emulator | Requires `DebugUITest` config |
|------|---------|:-:|:-:|
| `UI_TESTING=1` | Resets all state (auth tokens, prefs, sticky tab) | No | No |
| `USE_AUTH_EMULATOR=1` | Points Firebase Auth at local emulator (127.0.0.1:9099) | Yes | No |
| `SKIP_AUTH=1` | Bypasses Firebase entirely, fakes signed-in state | No | Yes |
| `TEST_IMAGE_PATH=<path>` | Auto-loads a test image, bypasses NSOpenPanel | No | No |
| `MOCK_DATA=1` | (Future) Load canned responses instead of hitting API | No | Yes |

### When to use which

- **Auth tests** (`AuthUITests`): `UI_TESTING=1` + `USE_AUTH_EMULATOR=1`, built with **Debug** config. Tests real login/register flows.
- **Feature tests** (`ImageEditorUITests`, etc.): `UI_TESTING=1` + `SKIP_AUTH=1`, built with **DebugUITest** config. No auth, no emulator.
- **Integration tests** (future): `UI_TESTING=1` + `USE_AUTH_EMULATOR=1` + `MOCK_DATA=1`, built with **DebugUITest** config.

## Build Configuration: DebugUITest

The `SKIP_AUTH` bypass code is guarded by `#if UITEST`, a Swift compilation condition that **only** exists in the `DebugUITest` build configuration. Normal Debug and Release builds have zero bypass code — it's stripped at compile time.

| Config | Swift Conditions | Has SKIP_AUTH code | Use case |
|--------|-----------------|:--:|---------|
| Debug | `DEBUG` | No | Daily development |
| DebugUITest | `DEBUG UITEST` | Yes | UI test runs |
| Release | (none) | No | Production |

To add a `ReleaseUITest` config in the future (pre-ship validation with optimizations), duplicate the Release flags in CMakeLists.txt and add `UITEST` to its Swift conditions.

### CMake setup

In `native/CMakeLists.txt`, `DebugUITest` is registered via `CMAKE_CONFIGURATION_TYPES` and inherits all Debug flags. The `UITEST` condition is set via:

```cmake
# macOS (Xcode generator)
set(CMAKE_XCODE_ATTRIBUTE_SWIFT_ACTIVE_COMPILATION_CONDITIONS[variant=DebugUITest] "DEBUG UITEST")

# Windows (Visual Studio generator) — same pattern when adding WinUI UI tests:
# set(CMAKE_CXX_FLAGS_DEBUGUITEST "${CMAKE_CXX_FLAGS_DEBUG} /DUITEST")
```

### Regenerating after CMakeLists changes

```bash
cd native && cmake -G Xcode -B build-xcode
```

## Test Structure

### Image Editor Tests (2 tests, ~34s total, DebugUITest config)

| Test | What it covers |
|------|---------------|
| `testEditModeAndSliders` | All UI elements exist, initial disabled states, canvas AX role. Exercises all 3 sliders (straighten, perspective V, perspective H): adjust to 0.75 → verify value → undo → verify center → redo → verify restored → reset → verify center. |
| `testNavigationFlow` | Back→library, Done→library with crop result, sidebar restored after Done. |

### Auth Tests (16 tests, ~160s total, Debug config + emulator)

Tests real Firebase Auth flows: register, sign in, sign out, duplicate email, password mismatch, remember me, etc.

## Running Tests

```bash
# Image editor tests only (DebugUITest, no emulator)
xcodebuild test -project native/build-xcode/Pivox.xcodeproj \
  -scheme PivoxUITests -configuration DebugUITest \
  -destination 'platform=macOS' \
  -only-testing:PivoxUITests/ImageEditorUITests

# Auth tests only (Debug + emulator)
firebase emulators:start --only auth --project pivox-cloud &
xcodebuild test -project native/build-xcode/Pivox.xcodeproj \
  -scheme PivoxUITests -configuration Debug \
  -destination 'platform=macOS' \
  -only-testing:PivoxUITests/AuthUITests
pkill -f "firebase.*emulators"

# All UI tests (make target runs both in sequence)
make test-native-ui

# C++ core tests (no emulator, any config)
cd native && cmake --build build-xcode --config Debug --target pivox_core_tests
./build-xcode/Debug/pivox_core_tests
```

## Future: Mock Data for Backend-Dependent UI

When UI tests need data that normally comes from the backend:

1. Set `MOCK_DATA=1` in launch environment (requires `DebugUITest` config)
2. Guard the check with `#if UITEST` — same as `SKIP_AUTH`
3. The network/data layer loads bundled JSON fixtures instead of making API calls
4. Fixtures live in `native/tests/fixtures/` and are included only in DebugUITest builds
5. No auth needed, no network, deterministic, fast

This follows the same pattern as `TEST_IMAGE_PATH` — launch flags swap real dependencies for test doubles. The `#if UITEST` guard ensures the bypass code never exists in Debug or Release builds.
